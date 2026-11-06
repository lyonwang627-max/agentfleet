package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
)

// timeNow 可替换时钟（测试用）。
var timeNow = time.Now

// LLMRunner 本地模型执行器：JSON 协议一次决策（reply / handoff / 审批门）。
// 结构化输出走 Ollama 原生通道（think:false + format:"json"），解析失败降级为纯文本 reply。
type LLMRunner struct {
	LLM *gateway.LLM
}

func NewLLM(llm *gateway.LLM) *LLMRunner { return &LLMRunner{LLM: llm} }

type decision struct {
	Reply         string `json:"reply"`
	HandoffTo     string `json:"handoff_to"`
	NeedsApproval bool   `json:"needs_approval"`
	Note          string `json:"note"`
}

func (r *LLMRunner) Execute(ctx context.Context, req ExecRequest) (Result, error) {
	emit := func(kind model.StepKind, name string, in, out map[string]any, latencyMS int64, tokens int) {
		if req.Recorder == nil {
			return
		}
		_ = req.Recorder(ctx, &model.RunStep{Kind: kind, Name: name, Input: in, Output: out,
			LatencyMS: latencyMS, Tokens: tokens})
	}

	system := buildSystemPrompt(req.Agent)
	user := fmt.Sprintf("# 任务\n%s\n\n# 详细说明\n%s\n\n只输出一个 JSON 对象。", req.Task.Title, req.Task.Prompt)

	start := timeNow()
	raw, tokens, err := r.LLM.ChatJSON(ctx, system, user, 900)
	latency := timeNow().Sub(start).Milliseconds()
	if err != nil {
		emit(model.StepLLM, "llm-failed", map[string]any{"model": req.Agent.Model}, map[string]any{"error": err.Error()}, latency, tokens)
		return Result{}, fmt.Errorf("llm: %w", err)
	}

	d := decision{}
	if err := json.Unmarshal(raw, &d); err != nil || strings.TrimSpace(d.Reply) == "" {
		// 模型给了非预期结构：原文当 reply，不让任务失败
		log.Printf("[runner/llm] 非 JSON 输出降级为 reply: %v", err)
		d = decision{Reply: string(raw), Note: "（模型输出非 JSON，已按原文采用）"}
	}
	emit(model.StepLLM, "decide", map[string]any{"model": req.Agent.Model},
		map[string]any{"handoff_to": d.HandoffTo, "needs_approval": d.NeedsApproval, "reply_head": head(d.Reply, 200)},
		latency, tokens)

	return Result{Reply: d.Reply, HandoffTo: d.HandoffTo, NeedsApproval: d.NeedsApproval, Note: d.Note}, nil
}

func buildSystemPrompt(a model.Agent) string {
	base := a.SystemPrompt
	if base == "" {
		base = fmt.Sprintf("你是 %s（%s），一名可靠的执行者。", a.Name, a.Role)
	}
	return base + `

输出要求：只输出一个 JSON 对象，字段如下：
{"reply": "任务的最终产出或处理说明", "handoff_to": "需要交接给其他 agent 时填其名字，否则留空", "needs_approval": true/false, "note": "交接或需要审批的原因"}
规则：涉及对外发布、资金、删除操作时 needs_approval 必须为 true；任务超出你的职责时用 handoff_to 交接，不要硬做。`
}
