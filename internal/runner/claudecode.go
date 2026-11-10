package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ClaudeCodeRunner 外部 agent 进程执行体（I34，对标 claude -p --bare headless 模式）：
// stream-json 事件流转 run_steps（assistant→llm / tool_use→tool），result 末行取最终产出。
// 会话级续跑（session_id）与 I11 步级结果重放互补分层。第 2 轮：注册/permission 映射/成本接 F10。

const ccDefaultTimeout = 10 * time.Minute // 对齐 CC 后台空闲上限

// ClaudeCodeRunner Bin 默认 "claude"（--bare 跳过自动发现，可复现）；测试注入 fake 脚本。
type ClaudeCodeRunner struct {
	Bin string
}

func NewClaudeCodeRunner() *ClaudeCodeRunner { return &ClaudeCodeRunner{Bin: "claude"} }

// ccEvent stream-json 单行事件（仅取平台关心的字段）。
type ccEvent struct {
	Type    string `json:"type"` // system | assistant | user | result
	Subtype string `json:"subtype,omitempty"`
	Message *struct {
		Content []struct {
			Type string          `json:"type"` // text | tool_use | tool_result
			Text string          `json:"text,omitempty"`
			Name string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
	} `json:"message,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	Result       string  `json:"result,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
}

func (r *ClaudeCodeRunner) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	return "claude"
}

func (r *ClaudeCodeRunner) Execute(ctx context.Context, req ExecRequest) (Result, error) {
	emit := func(kind model.StepKind, name string, in, out map[string]any, latencyMS int64, tokens int) {
		if req.Recorder == nil {
			return
		}
		_ = req.Recorder(ctx, &model.RunStep{Kind: kind, Name: name, Input: in, Output: out,
			LatencyMS: latencyMS, Tokens: tokens})
	}

	cctx, cancel := context.WithTimeout(ctx, ccDefaultTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, r.bin(), "-p", req.Task.Prompt,
		"--output-format", "stream-json", "--verbose", "--bare")
	// I34 教训对齐：SIGTERM=143 丢 turn——取消走 SIGINT（CC 文档建议的优雅中断信号）
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	if req.Workspace != nil && req.Workspace.RootPath != "" {
		cmd.Dir = req.Workspace.RootPath // 在绑定 workspace 中执行（I16 Local Runtime 落点）
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	cmd.Stderr = nil // stderr 丢弃（CC 的 stderr 是调试输出，非协议面）
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("启动 %s: %w", r.bin(), err)
	}

	start := timeNow()
	var reply strings.Builder
	var sessionID string
	var costUSD float64
	emit(model.StepGate, "cc-start", map[string]any{"bin": r.bin(), "cwd": cmd.Dir},
		map[string]any{"protocol": "stream-json"}, 0, 0)

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var ev ccEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue // 非协议行（启动噪音）跳过
		}
		switch {
		case ev.Type == "system" && ev.Subtype == "init":
			emit(model.StepGate, "cc-init", nil,
				map[string]any{"session_id": ev.SessionID, "capabilities": true}, 0, 0)
			sessionID = ev.SessionID
		case ev.Type == "assistant" && ev.Message != nil:
			for _, blk := range ev.Message.Content {
				if blk.Type == "text" && blk.Text != "" {
					emit(model.StepLLM, "cc-assistant", nil,
						map[string]any{"text_head": head(blk.Text, 200)}, 0, 0)
					reply.WriteString(blk.Text)
				} else if blk.Type == "tool_use" {
					emit(model.StepTool, blk.Name, map[string]any{"input_head": head(string(blk.Input), 200)},
						map[string]any{"transport": "subprocess", "runner": "claude-code"}, 0, 0)
				}
			}
		case ev.Type == "result":
			sessionID = ev.SessionID
			costUSD = ev.TotalCostUSD
			if ev.Result != "" {
				reply.Reset()
				reply.WriteString(ev.Result) // result 事件是权威最终产出
			}
		}
	}
	waitErr := cmd.Wait()
	latency := timeNow().Sub(start).Milliseconds()

	if ctx.Err() != nil {
		emit(model.StepLLM, "cc-canceled", nil, map[string]any{"error": "取消传播"}, latency, 0)
		return Result{}, ErrCanceled
	}
	if waitErr != nil {
		emit(model.StepGate, "cc-failed", map[string]any{"error": waitErr.Error()}, nil, latency, 0)
		return Result{}, fmt.Errorf("claude-code: %w", waitErr)
	}

	note := fmt.Sprintf("session=%s cost=$%.4f", sessionID, costUSD)
	emit(model.StepGate, "cc-done", nil,
		map[string]any{"session_id": sessionID, "cost_usd": costUSD}, latency, 0)
	return Result{Reply: strings.TrimSpace(reply.String()), Note: note}, nil
}
