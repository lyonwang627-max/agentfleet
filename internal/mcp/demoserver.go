package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// RunDemoServer 内置 MCP 演示 server（stdio）：echo + time 两个工具。
// fleetd 通过 re-exec 自身（--mcp-demo-worker）运行它——平台自带 MCP 端到端演示能力，
// 不依赖任何外部二进制；client_test 用同一实现做单测。
func RunDemoServer() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for sc.Scan() {
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue
		}
		if msg.ID == nil {
			continue // notification：无需响应
		}
		var result any
		switch msg.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "agentfleet-demo", "version": "0.1.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "回显输入（演示工具）",
					"inputSchema": map[string]any{"type": "object",
						"properties": map[string]any{"msg": map[string]any{"type": "string"}},
						"required":   []string{"msg"}}},
				{"name": "now", "description": "返回服务器当前时间",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			}}
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			text := ""
			switch p.Name {
			case "echo":
				text, _ = p.Arguments["msg"].(string)
				text = "echo: " + text
			case "now":
				text = time.Now().Format("2006-01-02 15:04:05 MST")
			default:
				text = fmt.Sprintf("unknown tool %q", p.Name)
			}
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
		default:
			result = map[string]any{}
		}
		resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
		out.Write(append(resp, '\n'))
		out.Flush()
	}
}
