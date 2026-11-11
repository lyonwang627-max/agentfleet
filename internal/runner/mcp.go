package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lyonwang/agentfleet/internal/mcp"
)

// MCP 注册中心：fleetd 启动时把已配置的 MCP server 连接放进来，
// runner（当前 mock，后续 llm）按 agent.tools 里的 mcp:<server>/<tool> 引用执行。
var (
	mcpMu      sync.RWMutex
	mcpServers = map[string]*mcp.Client{}
)

func SetMCPServer(name string, c *mcp.Client) {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	mcpServers[name] = c
}

// CallMCPTool 执行 mcp:<server>/<tool> 引用；未注册返回错误（不静默）。
func CallMCPTool(ref string, args map[string]any) (string, error) {
	server, tool, ok := strings.Cut(strings.TrimPrefix(ref, "mcp:"), "/")
	if !ok || server == "" || tool == "" {
		return "", fmt.Errorf("非法 MCP 工具引用 %q（应为 mcp:<server>/<tool>）", ref)
	}
	mcpMu.RLock()
	c := mcpServers[server]
	mcpMu.RUnlock()
	if c == nil {
		return "", fmt.Errorf("MCP server %q 未注册", server)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.CallTool(ctx, tool, args)
}

// toolKnown 缓存：ListTools 有网络成本，60s TTL 内复用判定结果。
var (
	toolKnownMu  sync.Mutex
	toolKnownC   map[string]bool
	toolKnownAt  time.Time
	toolKnownTTL = 60 * time.Second
)

// toolKnownFn seam：测试可注入模拟「已注册工具」判定（生产恒为 ToolKnown）。
var toolKnownFn = ToolKnown

// ToolKnown 工具引用是否真实存在（I28 第 2 轮：幻觉工具名引导的基础判定）。
// server 未注册直接 false；已注册则 ListTools 校验工具名（结果进缓存）。
func ToolKnown(ref string) bool {
	server, _, ok := strings.Cut(strings.TrimPrefix(ref, "mcp:"), "/")
	if !ok || server == "" {
		return false
	}
	toolKnownMu.Lock()
	if time.Since(toolKnownAt) > toolKnownTTL {
		toolKnownC = map[string]bool{}
		toolKnownAt = time.Now()
	}
	if v, hit := toolKnownC[ref]; hit {
		toolKnownMu.Unlock()
		return v
	}
	toolKnownMu.Unlock()

	known := false
	mcpMu.RLock()
	c := mcpServers[server]
	mcpMu.RUnlock()
	if c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		tools, err := c.ListTools(ctx)
		cancel()
		if err == nil {
			for _, t := range tools {
				if t.Name == strings.SplitN(strings.TrimPrefix(ref, "mcp:"), "/", 2)[1] {
					known = true
					break
				}
			}
		}
	}
	toolKnownMu.Lock()
	toolKnownC[ref] = known
	toolKnownMu.Unlock()
	return known
}

// ToolCatalog 步骤级路由（I28）的工具能力清单：decide 提示注入「有什么工具可选」，
// LLM 按能力描述选工具（supervisor 思想下沉到 run 内部——eino ADK 调研结论）。
// 无 server 注册时返回空（decide 提示退回原有形态）。
func ToolCatalog() string {
	mcpMu.RLock()
	servers := make(map[string]*mcp.Client, len(mcpServers))
	for k, v := range mcpServers {
		servers[k] = v
	}
	mcpMu.RUnlock()
	if len(servers) == 0 {
		return ""
	}
	var b strings.Builder
	for name, c := range servers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		tools, err := c.ListTools(ctx)
		cancel()
		if err != nil {
			continue
		}
		for _, t := range tools {
			desc := t.Description
			if r := []rune(desc); len(r) > 60 {
				desc = string(r[:60]) + "…"
			}
			b.WriteString("- mcp:" + name + "/" + t.Name)
			if desc != "" {
				b.WriteString("：" + desc)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ListMCPServers 网关观测用：已注册 server 及其动态发现的工具列表。
func ListMCPServers() []map[string]any {
	mcpMu.RLock()
	defer mcpMu.RUnlock()
	out := make([]map[string]any, 0, len(mcpServers))
	for name, c := range mcpServers {
		if c == nil {
			// nil client（SetMCPServer 合法输入态）：降级为 error 项而非 panic
			out = append(out, map[string]any{"name": name, "error": "client 未初始化"})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tools, err := c.ListTools(ctx)
		cancel()
		if err != nil {
			out = append(out, map[string]any{"name": name, "error": err.Error()})
			continue
		}
		out = append(out, map[string]any{"name": name, "tools": tools})
	}
	return out
}
