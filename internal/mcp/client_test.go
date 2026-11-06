package mcp

import (
	"context"
	"os"
	"testing"
	"time"
)

// 用 Go 标准 re-exec 模式起一个进程内 MCP server（零外部依赖）：
// 测试框架把自身作为子进程运行，GO_WANT_MCP_SERVER=1 时走 demo server 分支。
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_MCP_SERVER") == "1" {
		RunDemoServer()
		return
	}
	os.Exit(m.Run())
}

func TestClientHandshakeListCall(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c, err := Start(ctx, exe, nil, []string{"GO_WANT_MCP_SERVER=1"})
	if err != nil {
		t.Fatalf("启动/握手: %v", err)
	}
	defer c.Close()

	tools, err := c.ListTools(ctx)
	if err != nil || len(tools) < 1 {
		t.Fatalf("tools/list: %v %+v", err, tools)
	}
	names := map[string]bool{}
	for _, t := range tools {
		names[t.Name] = true
	}
	if !names["echo"] || !names["now"] {
		t.Fatalf("demo server 应含 echo/now 工具: %+v", names)
	}

	got, err := c.CallTool(ctx, "echo", map[string]any{"msg": "agentfleet"})
	if err != nil || got != "echo: agentfleet" {
		t.Fatalf("tools/call: got %q err=%v", got, err)
	}
}

func TestClientBadCommandFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Start(ctx, "/nonexistent/mcp-server-xyz", nil, nil); err == nil {
		t.Fatal("启动不存在的 server 应报错")
	}
}
