//go:build eino

// eino 回调桥接（I15，源自 eino-callbacks 调研）：把 eino 组件生命周期的五时点事件
// 转成 fleetd 的 run_steps——eino 编排层的可观测性自动并入我们的 trace，
// 与 runner.Recorder（fleetd 侧等价物）形成双向桥。

package runner

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/lyonwang/agentfleet/internal/model"
)

// TraceHandler eino callbacks.Handler → fleetd run_steps 桥。
// sink 由调用方注入（通常 = runner.Recorder 包装）；nil sink 时为空操作。
type TraceHandler struct {
	Sink func(step *model.RunStep) error
}

var (
	_ callbacks.Handler       = (*TraceHandler)(nil)
	_ callbacks.TimingChecker = (*TraceHandler)(nil)
)

func (h *TraceHandler) Needed(_ context.Context, _ *callbacks.RunInfo, _ callbacks.CallbackTiming) bool {
	return h != nil && h.Sink != nil
}

func (h *TraceHandler) emit(kind model.StepKind, name, note string) {
	if h == nil || h.Sink == nil {
		return
	}
	_ = h.Sink(&model.RunStep{Kind: kind, Name: name, Note: note})
}

func (h *TraceHandler) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	if in := einomodel.ConvCallbackInput(input); in != nil {
		h.emit(model.StepLLM, "eino:OnStart", "组件开始：消息数="+itoa(len(in.Messages)))
	}
	return ctx
}

func (h *TraceHandler) OnEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	if out := einomodel.ConvCallbackOutput(output); out != nil && out.Message != nil {
		content := out.Message.Content
		if len(content) > 120 {
			content = content[:120] + "…"
		}
		h.emit(model.StepLLM, "eino:OnEnd", "组件完成："+content)
	}
	return ctx
}

func (h *TraceHandler) OnError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	h.emit(model.StepLLM, "eino:OnError", "组件错误："+err.Error())
	return ctx
}

func (h *TraceHandler) OnStartWithStreamInput(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
	input.Close() // 必须关闭自己的副本，否则整条管道泄漏（eino 契约）
	return ctx
}

func (h *TraceHandler) OnEndWithStreamOutput(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
	output.Close()
	return ctx
}

// withTraceCallbacks 在 ctx 上挂 InitCallbacks + RunInfo，并触发 OnStart 切面。
// EinoChatModel.Generate 调用后须自行触发 OnEnd/OnError 切面收尾。
func withTraceCallbacks(ctx context.Context, h *TraceHandler, input []*schema.Message) context.Context {
	if h == nil {
		return ctx
	}
	ctx = callbacks.InitCallbacks(ctx, &callbacks.RunInfo{
		Name:      "fleetd-eino",
		Component: "ChatModel",
	}, h)
	return callbacks.OnStart(ctx, &einomodel.CallbackInput{Messages: input})
}
