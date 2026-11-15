package scheduler

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 确定性注入预算锁（ADR-0003 M1）：≤10 条 + ≤2000 字符，草稿不注入。
func TestMemoryInjectionBudget(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "inj", Model: "mock", Runner: "mock", Enabled: true})
	long := strings.Repeat("记", 300)
	for i := 0; i < 15; i++ {
		_, _ = st.CreateMemory(ctx, &model.Memory{AgentID: aid, Kind: "fact",
			Content: long + strings.Repeat("x", i), Status: "active"})
	}
	_, _ = st.CreateMemory(ctx, &model.Memory{AgentID: aid, Kind: "fact", Content: "草稿不注入", Status: "draft"})

	tid, _ := st.CreateTask(ctx, &model.Task{Title: "注入任务", Prompt: "p", AgentID: aid})
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})
	task, _ := st.GetTask(ctx, tid)

	capR := &captureRunner{}
	runner.Register("mock", capR)
	t.Cleanup(func() { runner.Register("mock", runner.NewMock()) })
	s.execute(ctx, *task)

	if capR.captured == nil {
		t.Fatal("execute 未运行")
	}
	prompt := capR.captured.Task.Prompt
	if !strings.Contains(prompt, "[已知背景（长期记忆）]") {
		t.Fatal("缺注入段")
	}
	if strings.Contains(prompt, "草稿不注入") {
		t.Fatal("草稿不应注入")
	}
	start := strings.Index(prompt, "[已知背景（长期记忆）]")
	end := strings.Index(prompt[start:], "\n\n")
	if end < 0 {
		end = len(prompt) - start
	}
	if n := len([]rune(prompt[start : start+end])); n > 2100 {
		t.Fatalf("注入段超预算: %d rune", n)
	}
}

// 记忆抽取（ADR-0003 M2）：judge 健康 → 抽取落草稿池（draft，不自动生效）；judge 不健康 → 跳过。
func TestExtractMemoriesToDraft(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "ex", Model: "mock", Runner: "mock", Enabled: true})
	// fake judge 返回抽取 JSON
	s.SetJudge(&fakeJudge{healthy: true, raw: `{"memories":[{"kind":"fact","content":"staging 库只读"},{"kind":"bogus_kind","content":"非法 kind 回退 fact"},{"kind":"preference","content":""}]}`})

	s.ExtractMemories(ctx, aid, 9, "部署任务", "部署完成，staging 库只读")

	mems, _ := st.ListMemories(ctx, aid, []string{"draft"})
	if len(mems) != 2 {
		t.Fatalf("有效抽取应 2 条草稿（空 content/非法 kind 处理后）: %d", len(mems))
	}
	for _, m := range mems {
		if m.Status != "draft" || m.SourceRunID != 9 {
			t.Fatalf("草稿状态/来源不符: %+v", m)
		}
		if m.Kind != "fact" && m.Kind != "preference" {
			t.Fatalf("kind 应回退 fact: %q", m.Kind)
		}
	}
	// 草稿不自动生效——人审 approve 后 active（M1 语义）
	if err := st.ApproveMemory(ctx, mems[0].ID); err != nil {
		t.Fatal(err)
	}
	active, _ := st.ListMemories(ctx, aid, []string{"active"})
	if len(active) != 1 {
		t.Fatalf("approve 后应 1 条 active: %d", len(active))
	}
}

func TestExtractMemoriesFailOpen(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "ex2", Model: "mock", Runner: "mock", Enabled: true})
	// judge 不健康 → 静默跳过
	s.SetJudge(&fakeJudge{healthy: false, raw: `{"memories":[{"kind":"fact","content":"x"}]}`})
	s.ExtractMemories(ctx, aid, 1, "任务", "产出")
	mems, _ := st.ListMemories(ctx, aid, nil)
	if len(mems) != 0 {
		t.Fatalf("不健康 judge 应跳过抽取: %d", len(mems))
	}
}
