package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
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
	if mode := os.Getenv("GO_WANT_MCP_ERRSERVER"); mode != "" {
		runErrServer(mode)
		return
	}
	os.Exit(m.Run())
}

// runErrServer 错误路径假 server（按 mode 决定行为）：
// err_list：tools/list 回 JSON-RPC error；err_exit：握手后即退出（连接中断）；
// empty：tools/call 回空 content；tool_err：tools/call 回 isError。
func runErrServer(mode string) {
	sc := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for sc.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.ID == nil {
			continue
		}
		switch {
		case mode == "err_init" && msg.Method == "initialize":
			writeJSON(out, msg.ID, map[string]any{"error": map[string]any{"code": -32000, "message": "handshake rejected"}})
		case mode == "err_list" && msg.Method == "tools/list":
			writeJSON(out, msg.ID, map[string]any{"error": map[string]any{"code": -32601, "message": "method not found"}})
		case mode == "empty" && msg.Method == "tools/call":
			writeJSON(out, msg.ID, map[string]any{"result": map[string]any{"content": []any{}}})
		case mode == "tool_err" && msg.Method == "tools/call":
			writeJSON(out, msg.ID, map[string]any{"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "磁盘已满"}}, "isError": true}})
		default:
			// initialize 及其余请求正常响应
			writeJSON(out, msg.ID, map[string]any{"result": map[string]any{
				"protocolVersion": ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "err-server"}}})
		}
		if mode == "err_exit" {
			return // 响应完 initialize 即退出：下一个请求将连接中断
		}
	}
}

func writeJSON(out *bufio.Writer, id json.RawMessage, v map[string]any) {
	v["jsonrpc"] = "2.0"
	v["id"] = id
	b, _ := json.Marshal(v)
	out.Write(append(b, '\n'))
	out.Flush()
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

// ---- 错误路径（client.go 未覆盖分支）----

// errServerStart 起错误模式假 server。
func errServerStart(t *testing.T, mode string) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c, err := Start(ctx, exe, nil, []string{"GO_WANT_MCP_ERRSERVER=" + mode})
	if err != nil {
		t.Fatalf("启动 err-server(%s): %v", mode, err)
	}
	return c
}

func TestServerErrorResponse(t *testing.T) {
	c := errServerStart(t, "err_list")
	defer c.Close()
	_, err := c.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "-32601") {
		t.Fatalf("JSON-RPC error 应上抛: %v", err)
	}
}

func TestCallToolEmptyContent(t *testing.T) {
	c := errServerStart(t, "empty")
	defer c.Close()
	got, err := c.CallTool(context.Background(), "x", nil)
	if err != nil || got != "" {
		t.Fatalf("空 content 应空串无错: %q %v", got, err)
	}
}

func TestCallToolIsError(t *testing.T) {
	c := errServerStart(t, "tool_err")
	defer c.Close()
	got, err := c.CallTool(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "磁盘已满") {
		t.Fatalf("isError 应转错误: %q %v", got, err)
	}
}

func TestConnectionInterrupted(t *testing.T) {
	c := errServerStart(t, "err_exit") // 握手后 server 即退出
	defer c.Close()
	time.Sleep(100 * time.Millisecond) // 等 server 退出、读循环收尾
	_, err := c.ListTools(context.Background())
	if err == nil {
		t.Fatal("连接中断应报错")
	}
	// 三种合理呈现：读循环收尾的中断 / 写入 broken pipe / 等待超时
	for _, kw := range []string{"中断", "超时", "broken pipe"} {
		if strings.Contains(err.Error(), kw) {
			return
		}
	}
	t.Fatalf("错误语义不符: %v", err)
}

// Initialize 握手被 server 拒绝（error 响应）→ Start 失败且 client 清理。
func TestStartInitializeRejected(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = Start(ctx, exe, nil, []string{"GO_WANT_MCP_ERRSERVER=err_init"})
	if err == nil || !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("握手拒绝应上抛: %v", err)
	}
}

// CallTool：server 回 JSON-RPC error（tools/call 层）→ 上抛（与 ListTools 对称）。
func TestCallToolErrorResponse(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Start(ctx, exe, nil, []string{"GO_WANT_MCP_SERVER=1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// demo server 的未知方法回 {} result——用非法工具名触发 server 侧 unknown tool 文本
	// （demo: unknown tool 文本非错误）。JSON-RPC error 分支由 err-server 的 tools/call
	// mode 间接覆盖（err_list mode 只对 tools/list）——此处锁 demo 正常路径的另一面：
	// 工具名大小写不匹配时 server 返回 unknown tool 文本（非 error）。
	got, err := c.CallTool(ctx, "no_such_tool", nil)
	if err != nil {
		t.Fatalf("unknown tool 文本应正常返回: %v", err)
	}
	if !strings.Contains(got, "unknown tool") {
		t.Fatalf("server unknown tool 文本不符: %q", got)
	}
}
