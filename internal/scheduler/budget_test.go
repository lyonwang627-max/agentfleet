package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 预算熔断（F5）：24h 用量达预算 → agent 自动停用 + 事件；重启用后（预算内）恢复派发。
func TestBudgetCircuitBreaker(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	runner.Register("mock", runner.NewMock())
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	budget := 200
	aid, err := st.CreateAgent(ctx, &model.Agent{
		Name: "costly", Role: "测试", Model: "qwen3.5", Runner: "mock",
		MaxConcurrency: 1, TokenBudget24h: budget, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	// 直接注入超过预算的用量（跳过真实 LLM：熔断逻辑与用量来源解耦）
	if err := st.AppendUsage(ctx, &model.UsageEvent{AgentID: aid, AgentName: "costly",
		Model: "qwen3.5", PromptTokens: 120, CompletionTokens: 150, Requests: 3}); err != nil {
		t.Fatal(err)
	}

	// 等 sweep tick 生效（poll=800ms）
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		a, _ := st.GetAgent(ctx, aid)
		if a != nil && !a.Enabled {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	a, _ := st.GetAgent(ctx, aid)
	if a == nil || a.Enabled {
		t.Fatalf("预算熔断未生效: %+v", a)
	}
	// 告警事件已入流
	evs, _ := st.EventsSince(ctx, 0, 50)
	found := false
	for _, e := range evs {
		if e.Kind == "agent.budget_exceeded" {
			found = true
		}
	}
	if !found {
		t.Fatalf("未发出 agent.budget_exceeded 事件")
	}

	// 人工重新启用：恢复派发资格（用量仍超但口径由人决定）
	enabled := true
	if err := st.UpdateAgent(ctx, aid, store.AgentPatch{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	a2, _ := st.GetAgent(ctx, aid)
	if !a2.Enabled {
		t.Fatal("人工重启用失败")
	}
}
