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

// ListMCPServers 网关观测用：已注册 server 及其动态发现的工具列表。
func ListMCPServers() []map[string]any {
	mcpMu.RLock()
	defer mcpMu.RUnlock()
	out := make([]map[string]any, 0, len(mcpServers))
	for name, c := range mcpServers {
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
