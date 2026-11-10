package runner

import (
	"context"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// trace 层级化（I17 第 1 轮）：循环观察步挂当轮 decide 步下（parent_seq），
// 普通步平铺（parent_seq=0），handoff 链 run 指向前一 run（parent_run_id，调度器测）。

func TestToolLoopStepsNested(t *testing.T) {
	// 真store recorder：AppendStep 回填 Seq，层级挂接可断言
	st := store.NewMemStore()
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "h", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "层级任务", Prompt: "查一下", AgentID: aid})
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid})

	recorder := func(_ context.Context, s *model.RunStep) error {
		_, err := st.AppendStep(ctx, rid, s)
		return err
	}

	llm := newTestLLM([]string{
		`{"tool":"mcp:ghost/echo","tool_args":{"msg":"a"},"reply":""}`,
		`{"reply":"完成"}`,
	}, nil)
	res, err := NewLLM(llm).Execute(ctx, ExecRequest{
		Agent: model.Agent{Name: "h", Model: "mock"}, Task: model.Task{Title: "层级任务", Prompt: "查一下"},
		Recorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "完成" {
		t.Fatalf("循环未收尾: %q", res.Reply)
	}
	steps, _ := st.ListSteps(ctx, rid)
	if len(steps) < 3 {
		t.Fatalf("应至少 3 步（decide-turn-1/tool/decide 汇总）: %d", len(steps))
	}
	// 第 1 步=decide-turn-1（顶层），第 2 步=tool（挂第 1 步下），后续 decide 汇总顶层
	if steps[0].Name != "decide-turn-1" || steps[0].ParentSeq != 0 {
		t.Fatalf("首步应是顶层 decide-turn-1: %+v", steps[0])
	}
	if steps[1].Kind != model.StepTool || steps[1].ParentSeq != steps[0].Seq {
		t.Fatalf("tool 步应挂 decide-turn-1 下: parent=%d want=%d", steps[1].ParentSeq, steps[0].Seq)
	}
	// 汇总 decide 步顶层
	last := steps[len(steps)-1]
	if last.Kind == model.StepLLM && last.Name == "decide" && last.ParentSeq != 0 {
		t.Fatalf("汇总 decide 应顶层: %+v", last)
	}
}

func TestPlainStepsFlat(t *testing.T) {
	// 直达收尾（无工具）：只有汇总 decide，parent_seq=0
	st := store.NewMemStore()
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "f", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "平铺任务", Prompt: "p", AgentID: aid})
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid})

	llm := newTestLLM([]string{`{"reply":"直达完成"}`}, nil)
	if _, err := NewLLM(llm).Execute(ctx, ExecRequest{
		Agent: model.Agent{Name: "f", Model: "mock"}, Task: model.Task{Title: "平铺任务", Prompt: "p"},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			_, err := st.AppendStep(ctx, rid, s)
			return err
		}}); err != nil {
		t.Fatal(err)
	}
	steps, _ := st.ListSteps(ctx, rid)
	for _, s := range steps {
		if s.ParentSeq != 0 {
			t.Fatalf("无工具循环应全顶层: %+v", s)
		}
	}
}
