package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 步级续跑结果重放（I11 第 1 轮）：重试时确定性 tool 步命中已记录观察则不重复调用 MCP。

func TestReplayToolHit(t *testing.T) {
	// 重试通道命中：同 ref+args → 返回已记录观察，不调 MCP（ghost 未注册也能「成功」）
	st := store.NewMemStore()
	ctx := context.Background()
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: 1, AgentID: 1})

	var seenUsers []string
	llm := newTestLLM([]string{
		`{"tool":"mcp:demo/echo","tool_args":{"msg":"hi"},"reply":""}`,
		`{"reply":"基于重放观察完成"}`,
	}, &seenUsers)
	res, err := NewLLM(llm).Execute(ctx, ExecRequest{
		Agent:   model.Agent{Name: "t", Model: "mock"},
		Task:    model.Task{Title: "重试任务", Prompt: "做事"},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			_, err := st.AppendStep(ctx, rid, s)
			return err
		},
		ReplayTools: []ReplayTool{{Ref: "mcp:demo/echo", ArgsJSON: `{"msg":"hi"}`, Result: "echo: 已记录观察"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "基于重放观察完成" {
		t.Fatalf("应基于重放收尾: %q", res.Reply)
	}
	// 第二轮 user 的观察是已记录结果（非「不存在/失败」）
	if len(seenUsers) < 2 || !strings.Contains(seenUsers[1], "echo: 已记录观察") {
		t.Fatalf("重放观察未回灌: %q", seenUsers[1])
	}
	// tool 步：deterministic + replay 双标
	steps, _ := st.ListSteps(ctx, rid)
	ok := false
	for _, s := range steps {
		if s.Kind == model.StepTool {
			if !s.Deterministic || !s.Replay {
				t.Fatalf("tool 步应 deterministic+replay: %+v", s)
			}
			if s.Output["replay"] != true {
				t.Fatalf("output 缺 replay 标: %v", s.Output)
			}
			ok = true
		}
	}
	if !ok {
		t.Fatal("缺 tool 步")
	}
}

func TestReplayMissFallsThrough(t *testing.T) {
	// args 不同 → 未命中 → 正常执行路径（ghost 未注册 → 引导观察）
	st := store.NewMemStore()
	ctx := context.Background()
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: 1, AgentID: 1})

	var seenUsers []string
	llm := newTestLLM([]string{
		`{"tool":"mcp:demo/echo","tool_args":{"msg":"different"},"reply":""}`,
		`{"reply":"按引导收尾"}`,
	}, &seenUsers)
	_, err := NewLLM(llm).Execute(ctx, ExecRequest{
		Agent:   model.Agent{Name: "t", Model: "mock"},
		Task:    model.Task{Title: "重试任务", Prompt: "做事"},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			_, err := st.AppendStep(ctx, rid, s)
			return err
		},
		ReplayTools: []ReplayTool{{Ref: "mcp:demo/echo", ArgsJSON: `{"msg":"hi"}`, Result: "echo: 旧观察"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 未命中走真实路径：ghost server 未注册 → 引导观察（非旧观察）
	hallu := false
	steps, _ := st.ListSteps(ctx, rid)
	for _, s := range steps {
		if s.Kind == model.StepTool && s.Output["hallucination"] == true {
			hallu = true
		}
	}
	if !hallu {
		t.Fatal("未命中应走幻觉引导路径")
	}
	if len(seenUsers) < 2 || strings.Contains(seenUsers[1], "旧观察") {
		t.Fatalf("未命中不应使用旧观察: %q", seenUsers[1])
	}
}

func TestNoReplayChannelWhenZero(t *testing.T) {
	// RetryCount=0（含 replay 端点清零）：ReplayTools 空 → 完全重跑
	st := store.NewMemStore()
	ctx := context.Background()
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: 1, AgentID: 1})

	llm := newTestLLM([]string{
		`{"tool":"mcp:demo/echo","tool_args":{"msg":"hi"},"reply":""}`,
		`{"reply":"真实执行后收尾"}`,
	}, nil)
	if _, err := NewLLM(llm).Execute(ctx, ExecRequest{
		Agent:   model.Agent{Name: "t", Model: "mock"},
		Task:    model.Task{Title: "replay 清零任务", Prompt: "做事"},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			_, err := st.AppendStep(ctx, rid, s)
			return err
		},
		// 无 ReplayTools：即便 ghost 未注册也走幻觉引导（真实路径），不出现重放
	}); err != nil {
		t.Fatal(err)
	}
	steps, _ := st.ListSteps(ctx, rid)
	for _, s := range steps {
		if s.Kind == model.StepTool && s.Replay {
			t.Fatal("无重放通道不应标 replay")
		}
	}
}
