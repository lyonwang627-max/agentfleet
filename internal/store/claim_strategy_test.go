package store

import (
	"context"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 认领策略（I10 第 1 轮）：默认 priority 与历史行为完全一致；fairshare 公平轮转。

func mustClaimAgent(t *testing.T, st *MemStore, name string) int64 {
	t.Helper()
	aid, err := st.CreateAgent(context.Background(), &model.Agent{Name: name, Model: "mock", Runner: "mock", Enabled: true, MaxConcurrency: 10})
	if err != nil {
		t.Fatal(err)
	}
	return aid
}

func TestClaimStrategyPriorityDefault(t *testing.T) {
	// 默认策略：同优先级按 id 先到先得（历史行为锁）
	st := NewMemStore()
	ctx := context.Background()
	aid := mustClaimAgent(t, st, "p")
	for i := 0; i < 3; i++ {
		st.CreateTask(ctx, &model.Task{Title: "同优先", Prompt: "p", AgentID: aid})
		time.Sleep(2 * time.Millisecond) // id 相同优先级——默认按 id
	}
	first, _ := st.ClaimNextTask(ctx)
	if first == nil {
		t.Fatal("应认领到任务")
	}
	// 三个任务同优先级，默认策略选最小 id
	tasks, _, _ := st.ListTasks(ctx, nil, aid, 1, 10)
	minID := tasks[0].ID
	for _, tk := range tasks {
		if tk.ID < minID {
			minID = tk.ID
		}
	}
	if first.ID != minID {
		t.Fatalf("默认策略应选最小 id: got %d want %d", first.ID, minID)
	}
}

func TestClaimStrategyFairShare(t *testing.T) {
	// fairshare：同优先级按 created_at 最早优先（防同优先级内 id 大者饿死早创建任务）
	st := NewMemStoreWithStrategy("fairshare")
	ctx := context.Background()
	aid := mustClaimAgent(t, st, "f")
	// 早任务（created_at 早）
	t1id, _ := st.CreateTask(ctx, &model.Task{Title: "早任务", Prompt: "p", AgentID: aid})
	// 晚任务优先级相同；fairshare 选 created_at 最早者
	time.Sleep(5 * time.Millisecond)
	t2id, _ := st.CreateTask(ctx, &model.Task{Title: "晚任务", Prompt: "p", AgentID: aid})
	first, _ := st.ClaimNextTask(ctx)
	if first == nil {
		t.Fatal("应认领到任务")
	}
	if first.ID != t1id {
		t.Fatalf("fairshare 应选 created_at 最早: got %d want %d", first.ID, t1id)
	}
	_ = t2id
}

func TestClaimStrategyUnknownFallsBack(t *testing.T) {
	// 未知策略名回退默认（不炸）
	st := NewMemStoreWithStrategy("totally-unknown")
	ctx := context.Background()
	aid := mustClaimAgent(t, st, "u")
	st.CreateTask(ctx, &model.Task{Title: "t", Prompt: "p", AgentID: aid})
	first, _ := st.ClaimNextTask(ctx)
	if first == nil {
		t.Fatal("未知策略应回退默认仍可认领")
	}
}
