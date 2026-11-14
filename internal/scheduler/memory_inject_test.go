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
