// Package mcp 实现 MCP（Model Context Protocol）stdio 客户端的最小可用子集：
// initialize 握手 → tools/list 动态发现 → tools/call 调用（JSON-RPC 2.0，换行分帧）。
// 对齐 MCP 规范 2025-06-18 的 stdio 传输。为什么不引官方 SDK：控制面只需要三个方法，
// 换取零依赖与全透明的 trace 注入点（见 ARCHITECTURE 依赖原则）。
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

const ProtocolVersion = "2025-06-18"

type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcMsg
	err     error
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Start 启动 MCP server 子进程并完成 initialize 握手。
func Start(ctx context.Context, command string, args []string, env []string) (*Client, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 MCP server %q: %w", command, err)
	}

	c := &Client{cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout),
		pending: map[int64]chan rpcMsg{}}
	c.scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	go c.readLoop()

	if _, err := c.Initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) readLoop() {
	for c.scanner.Scan() {
		line := c.scanner.Bytes()
		var msg rpcMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // 非法行忽略（server 日志混入时容错）
		}
		if msg.ID == nil || len(msg.ID) == 0 {
			continue // notification（server→client 方向）暂不处理
		}
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.mu.Unlock()
		if ok {
			ch <- msg
		}
	}
	err := c.scanner.Err()
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	log.Printf("[mcp] server 读循环退出: %v", err)
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcMsg, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("写入 %s 请求: %w", method, err)
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("server 连接中断（%s）", method)
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("MPC error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("%s 超时", method)
	}
}

// Initialize 握手并回报 client 能力（v1：无自身能力）。
func (c *Client) Initialize(ctx context.Context) (json.RawMessage, error) {
	result, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agentfleet", "version": "0.1.0"},
	})
	if err != nil {
		return nil, err
	}
	// 规范要求：收到 result 后回 initialized notification（无 id，不期待响应）
	notif, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if _, err := c.stdin.Write(append(notif, '\n')); err != nil {
		return nil, err
	}
	return result, nil
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ListTools 动态发现 server 提供的工具。
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	result, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

type ToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// CallTool 调用 server 上的工具，返回拼接后的文本内容。
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	result, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var tr ToolResult
	if err := json.Unmarshal(result, &tr); err != nil {
		return "", err
	}
	text := ""
	for _, part := range tr.Content {
		text += part.Text
	}
	if tr.IsError {
		return text, fmt.Errorf("工具返回错误: %s", text)
	}
	return text, nil
}

func (c *Client) Close() {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}
