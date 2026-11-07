package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// I29 调度器侧分级策略回归锁：mock 风险关键词 → review 门带 risk 标注（gate step + 事件）。
func TestRiskLevelReviewGate(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	runner.Register("mock", runner.NewMock())
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	aid := mustAgent(t, st, "risk-worker", "mock")
	// mock 剧本：对外发布 → high；需审批 → medium
	highID, _ := st.CreateTask(ctx, &model.Task{Title: "高风险任务", Prompt: "对外发布操作，需要审批。", AgentID: aid, Priority: 7})
	medID, _ := st.CreateTask(ctx, &model.Task{Title: "中风险任务", Prompt: "常规操作，需要审批。", AgentID: aid, Priority: 6})

	waitStatus(t, st, highID, model.TaskReview, 8*time.Second)
	waitStatus(t, st, medID, model.TaskReview, 8*time.Second)

	// high 任务的 gate step 应带 risk_level=high
	assertGateRisk(t, ctx, st, highID, "high")
	// medium 任务的 gate step 应带 risk_level=medium
	assertGateRisk(t, ctx, st, medID, "medium")

	// 事件流应带 risk_level 标注（Data 经持久化是 map）
	evs, _ := st.EventsSince(ctx, 0, 200)
	highEvent := false
	for _, e := range evs {
		if e.Kind != "task.update" {
			continue
		}
		if data, ok := e.Data.(map[string]any); ok {
			if rl, _ := data["risk_level"].(string); rl == "high" {
				highEvent = true
			}
		}
	}
	if !highEvent {
		t.Fatal("事件流缺 risk_level=high 标注")
	}
}

func assertGateRisk(t *testing.T, ctx context.Context, st store.Store, taskID int64, wantRisk string) {
	t.Helper()
	runs, _ := st.ListRuns(ctx, taskID)
	if len(runs) == 0 {
		t.Fatalf("task#%d 无 run", taskID)
	}
	steps, _ := st.ListSteps(ctx, runs[0].ID)
	for _, s := range steps {
		if s.Kind == model.StepGate {
			if rl, ok := s.Input["risk"].(string); ok && rl == wantRisk {
				return
			}
		}
	}
	t.Fatalf("task#%d 的 gate step 缺 risk=%s 标注", taskID, wantRisk)
}

func waitStatus(t *testing.T, st store.Store, taskID int64, want model.TaskStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tsk, _ := st.GetTask(context.Background(), taskID); tsk != nil && tsk.Status == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	tsk, _ := st.GetTask(context.Background(), taskID)
	t.Fatalf("任务未在 %v 内到达 %s: %+v", timeout, want, tsk)
}
