//go:build eino

// Package runner 的 Eino 适配器（F7）：把 fleetd gateway 包装成 eino 的 model.BaseChatModel。
// 方向选择：不是「用 eino 重写 runner」，而是「让 eino 生态的编排代码能直接使用我们的网关」——
// 降级/计量/预算熔断全部继承。构建隔离：go build -tags eino 才编译（日常构建零 eino 依赖）。
package runner

import (
	"context"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
)

// EinoChatModel eino BaseChatModel 适配器：桥接 fleetd gateway。
type EinoChatModel struct {
	gw *gateway.LLM
}

// NewEinoChatModel 用 fleetd 网关构造 eino ChatModel。
func NewEinoChatModel(gw *gateway.LLM) *EinoChatModel { return &EinoChatModel{gw: gw} }

// Generate eino 标准接口：多消息输入 → gateway.ChatRaw（native 通道，think:false）→ 助手消息。
// 消息角色原样透传；网关 mock 模式下同样可用（确定性输出，eino 编排可测试）。
func (e *EinoChatModel) Generate(ctx context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	msgs := make([]gateway.ChatMessage, 0, len(input))
	for _, m := range input {
		if m == nil {
			continue
		}
		msgs = append(msgs, gateway.ChatMessage{Role: string(m.Role), Content: m.Content})
	}
	text, tokens, err := e.gw.ChatRaw(ctx, msgs, 1024)
	if err != nil {
		return nil, err
	}
	out := schema.AssistantMessage(text, nil)
	_ = tokens // token 计量在 gateway 层落 usage，这里不重复计
	return out, nil
}

// EinoRunner 基于 eino 抽象的执行器：与 llm runner 同语义（一次 JSON 决策），
// 但消息流走 eino schema——演示「eino 编排 + fleetd 网关」的组合可行性。
type EinoRunner struct {
	cm *EinoChatModel
}

// NewEinoRunner 用 fleetd 网关构造 eino runner。
func NewEinoRunner(gw *gateway.LLM) Runner { return &EinoRunner{cm: NewEinoChatModel(gw)} }

func init() { // eino tag 构建时向主构建暴露工厂（无 tag 时 EinoRunnerFactory 恒为 nil）
	EinoRunnerFactory = func(gw *gateway.LLM) Runner { return NewEinoRunner(gw) }
}

func (r *EinoRunner) Execute(ctx context.Context, req ExecRequest) (Result, error) {
	emit := func(kind model.StepKind, name string, in, out map[string]any, latencyMS int64, tokens int) {
		if req.Recorder == nil {
			return
		}
		_ = req.Recorder(ctx, &model.RunStep{Kind: kind, Name: name, Input: in, Output: out,
			LatencyMS: latencyMS, Tokens: tokens})
	}
	msgs := []*schema.Message{
		schema.SystemMessage(buildSystemPrompt(req.Agent)),
		schema.UserMessage("# 任务\n" + req.Task.Title + "\n\n# 详细说明\n" + req.Task.Prompt),
	}
	out, err := r.cm.Generate(ctx, msgs)
	if err != nil {
		return Result{}, err
	}
	var d decision
	if err := strictJSON(out.Content, &d); err != nil || strings.TrimSpace(d.Reply) == "" {
		d = decision{Reply: out.Content, Note: "（eino 输出非 JSON，已按原文采用）"}
	}
	emit(model.StepLLM, "eino-decide", map[string]any{"via": "eino adapter"},
		map[string]any{"handoff_to": d.HandoffTo, "needs_approval": d.NeedsApproval}, 0, 0)
	return Result{Reply: d.Reply, HandoffTo: d.HandoffTo, NeedsApproval: d.NeedsApproval, Note: d.Note}, nil
}
