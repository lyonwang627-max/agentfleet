package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ToolProvider 工具池统一接口（I8，对标 LiteLLM 的多源路由思想）：
// mock 的内置检索与 MCP server 原是两套注册/调用路径——统一为按 scheme 分发的单一入口，
// llm runner 多轮循环可无缝换工具源（F11 决策协议的 tool 字段即引用）。
type ToolProvider interface {
	// Invoke 执行工具引用（如 "mcp:demo/echo" / "builtin:text_search"），返回文本观察。
	Invoke(ref string, args map[string]any) (string, error)
	// Catalog 可用工具清单（name+描述摘要，decide 提示注入用；空=无工具）。
	Catalog() string
}

// builtinTool 内置工具（进程内函数，无外部依赖）。
type builtinTool struct {
	desc string
	fn   func(args map[string]any) (string, error)
}

var (
	builtinMu    sync.RWMutex
	builtinTools = map[string]*builtinTool{}
	kbSearchFn   func(ctx context.Context, kbID int, query string, topK int) ([]KBHit, error) // K 系列：store 注入（避免 runner→store 依赖）
)

// KBHit 知识库检索命中（runner 侧视图）。
type KBHit struct {
	DocName string
	Seq     int
	Content string
}

// SetKBSearch 注册知识库检索回调（fleetd 启动时由 main 注入 store 实现）。
func SetKBSearch(fn func(ctx context.Context, kbID int, query string, topK int) ([]KBHit, error)) {
	kbSearchFn = fn
}

// RegisterBuiltinTool 注册内置工具（name 不含 scheme；进程内确定性实现）。
func RegisterBuiltinTool(name, desc string, fn func(args map[string]any) (string, error)) {
	builtinMu.Lock()
	defer builtinMu.Unlock()
	builtinTools[name] = &builtinTool{desc: desc, fn: fn}
}

// builtin:text_search 内置检索（mock 知识源——原 mock 剧本路径的接口化）。
func init() {
	RegisterBuiltinTool("text_search", "内置知识检索（返回 seed 知识要点）", func(args map[string]any) (string, error) {
		q, _ := args["query"].(string)
		if q == "" {
			return "", fmt.Errorf("text_search 需要 query 参数")
		}
		return "seed-knowledge: 关于「" + q + "」的要点（内置检索）", nil
	})

	// K 系列：知识库检索——分段召回 top-3，多段拼接（含来源 doc 名）
	RegisterBuiltinTool("kb_search", "知识库检索：按关键词从知识库召回相关分段（含来源文档名）", func(args map[string]any) (string, error) {
		q, _ := args["query"].(string)
		if q == "" {
			return "", fmt.Errorf("kb_search 需要 query 参数")
		}
		kbID := 0 // 0=跨全部知识库
		if v, ok := args["kb_id"].(float64); ok && v > 0 {
			kbID = int(v)
		}
		if kbSearchFn == nil {
			return "知识库未就绪（store 未注入）", nil
		}
		hits, err := kbSearchFn(context.Background(), kbID, q, 3)
		if err != nil {
			return "", err
		}
		if len(hits) == 0 {
			return "知识库中未找到与「" + q + "」相关的内容", nil
		}
		var out strings.Builder
		out.WriteString(fmt.Sprintf("知识库检索命中 %d 段：\n", len(hits)))
		for _, h := range hits {
			fmt.Fprintf(&out, "\n[doc:%s seq:%d]\n%s\n", h.DocName, h.Seq, head(h.Content, 300))
		}
		return out.String(), nil
	})
}

// mcpProvider MCP 工具源：引用形如 mcp:<server>/<tool>，委托 CallMCPTool。
type mcpProvider struct{}

func (mcpProvider) Invoke(ref string, args map[string]any) (string, error) {
	return CallMCPTool(ref, args)
}

func (mcpProvider) Catalog() string { return ToolCatalog() }

// UnifiedTools 统一调用入口：按 scheme 分发到对应 provider。
// "mcp:" 前缀 → MCP；"builtin:" 前缀 → 内置；无 scheme 兼容旧引用（按 MCP 解析——既有剧本不受影响）。
func UnifiedTools() ToolProvider { return unified{} }

type unified struct{}

func (unified) Invoke(ref string, args map[string]any) (string, error) {
	switch {
	case strings.HasPrefix(ref, "builtin:"):
		name := strings.TrimPrefix(ref, "builtin:")
		builtinMu.RLock()
		tool, ok := builtinTools[name]
		builtinMu.RUnlock()
		if !ok {
			return "", fmt.Errorf("内置工具 %q 未注册", name)
		}
		return tool.fn(args)
	default:
		return CallMCPTool(ref, args)
	}
}

func (unified) Catalog() string {
	builtinMu.RLock()
	names := make([]string, 0, len(builtinTools))
	for n := range builtinTools {
		names = append(names, n)
	}
	builtinMu.RUnlock()
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "- builtin:%s：%s\n", n, builtinTools[n].desc)
	}
	if mc := ToolCatalog(); mc != "" {
		b.WriteString(mc)
	}
	return b.String()
}
