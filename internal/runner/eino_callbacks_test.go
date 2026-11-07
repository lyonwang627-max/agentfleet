//go:build eino

package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/callbacks"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
)

// 回调桥接（I15）：eino 五时点事件 → fleetd run_steps（sink 收集断言）。
func TestTraceHandlerEmitsToSink(t *testing.T) {
	var steps []model.RunStep
	h := &TraceHandler{Sink: func(s *model.RunStep) error { steps = append(steps, *s); return nil }}
	ctx := context.Background()

	info := &callbacks.RunInfo{Name: "fleetd-eino"}
	ctx = h.OnStart(ctx, info, &einomodel.CallbackInput{Messages: []*schema.Message{schema.UserMessage("hi")}})
	h.OnEnd(ctx, info, &einomodel.CallbackOutput{Message: schema.AssistantMessage("done content here", nil)})
	h.OnError(ctx, info, context.DeadlineExceeded)

	names := make([]string, 0, len(steps))
	for _, s := range steps {
		names = append(names, s.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"eino:OnStart", "eino:OnEnd", "eino:OnError"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺 %s: %v", want, names)
		}
	}
	if !strings.Contains(steps[1].Note, "done content") {
		t.Fatalf("OnEnd 未带输出摘要: %q", steps[1].Note)
	}
}

// Generate 全链路：EinoRunner 经 WithTraceSink 注入，eino 回调自动落 run_steps。
func TestEinoRunnerTraceIntegration(t *testing.T) {
	gw := gateway.New("mock")
	var steps []model.RunStep
	r := NewEinoRunner(gw)
	recorder := func(_ context.Context, s *model.RunStep) error {
		steps = append(steps, *s)
		return nil
	}
	res, err := r.Execute(context.Background(), ExecRequest{
		Agent:    model.Agent{Name: "t", Model: "mock"},
		Task:     model.Task{Title: "回调桥验证", Prompt: "普通任务，直接完成。"},
		Recorder: recorder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply == "" {
		t.Fatal("reply 为空")
	}
	found := false
	for _, s := range steps {
		if strings.HasPrefix(s.Name, "eino:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("未见 eino 回调产生的 step: %+v", steps)
	}
}
