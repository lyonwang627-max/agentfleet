package runner

import (
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
)

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
