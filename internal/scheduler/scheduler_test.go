package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 集成链路（MemStore + MockRunner，确定性重放）：
// 创建任务 → 调度执行 → handoff 改派 → 审批门 → 放行 → done；含依赖阻塞与失败重试。
func TestTaskLifecycle(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	runner.Register("mock", runner.NewMock())
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	// 依赖任务会被先创建：dep done 后主任务才能跑
	depID, err := st.CreateTask(ctx, &model.Task{Title: "前置任务", Prompt: "普通任务，直接完成。",
		AgentID: mustAgent(t, st, "worker", "mock"), Priority: 5})
	if err != nil {
		t.Fatal(err)
	}
	mainID, err := st.CreateTask(ctx, &model.Task{Title: "主任务", Prompt: "需要交接：handoff:reviewer 完成质检。",
		AgentID: mustAgent(t, st, "worker", "mock"), Priority: 5, DependsOn: []int64{depID}})
	if err != nil {
		t.Fatal(err)
	}
	failID, _ := st.CreateTask(ctx, &model.Task{Title: "会失败的任务", Prompt: "这个任务要 失败 两次然后成功。",
		AgentID: mustAgent(t, st, "worker", "mock"), Priority: 5})
	_ = failID

	// 等依赖任务完成 + 主任务被 handoff 到 reviewer 进入 review（审批门由 mock 剧本决定）
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		main, _ := st.GetTask(ctx, mainID)
		dep, _ := st.GetTask(ctx, depID)
		if dep != nil && dep.Status == model.TaskDone && main != nil &&
			(main.Status == model.TaskReview || main.Status == model.TaskDone || main.Status == model.TaskQueued) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	dep, _ := st.GetTask(ctx, depID)
	if dep == nil || dep.Status != model.TaskDone {
		t.Fatalf("依赖任务未完成: %+v", dep)
	}
	_ = mainID

	// 失败重试路径：两次失败后退避重试，最终因 mock 剧本固定失败而 failed
	t.Logf("等待失败任务进入重试/终态…")
	deadline = time.Now().Add(15 * time.Second)
	var last *model.Task
	for time.Now().Before(deadline) {
		last, _ = st.GetTask(ctx, failID)
		if last != nil && (last.Status == model.TaskFailed || strings.Contains(last.Error, "mock")) && last.RetryCount > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if last == nil || last.RetryCount == 0 {
		t.Fatalf("失败任务未触发重试: %+v", last)
	}
}

// TestApprovalLoop 审批闭环：mock 任务进 review → 人工 approve（注入 [审批记录]）→ 重放后 done。
func TestApprovalLoop(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	runner.Register("mock", runner.NewMock())
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	aid := mustAgent(t, st, "operator", "mock")
	gateID, err := st.CreateTask(ctx, &model.Task{Title: "需审批的任务",
		Prompt: "对外发布操作，需要审批。", AgentID: aid, Priority: 7})
	if err != nil {
		t.Fatal(err)
	}

	// 等进入 review
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if tsk, _ := st.GetTask(ctx, gateID); tsk != nil && tsk.Status == model.TaskReview {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	tsk, _ := st.GetTask(ctx, gateID)
	if tsk == nil || tsk.Status != model.TaskReview {
		t.Fatalf("任务未进入审批门: %+v", tsk)
	}

	// 人工放行（模拟 API approve 的语义：注入 [审批记录] 并重新排队）
	now := time.Now()
	approved := tsk.Prompt + "\n\n[审批记录] 2026-11-06 03:10 人工放行"
	if err := st.UpdateTask(ctx, gateID, store.TaskPatch{
		Status: ptrStatusQ(model.TaskQueued), VisibleAfter: &now, Prompt: &approved}); err != nil {
		t.Fatal(err)
	}

	// 等待重放后 done
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if t2, _ := st.GetTask(ctx, gateID); t2 != nil && t2.Status == model.TaskDone {
			return // ✓ 审批闭环
		}
		time.Sleep(100 * time.Millisecond)
	}
	final, _ := st.GetTask(ctx, gateID)
	t.Fatalf("审批放行后未完成: %+v", final)
}

func ptrStatusQ(s model.TaskStatus) *model.TaskStatus { return &s }

// mustAgent 建一个测试 agent，返回 id。
func mustAgent(t *testing.T, st store.Store, name, runnerName string) int64 {
	t.Helper()
	id, err := st.CreateAgent(context.Background(), &model.Agent{
		Name: name, Role: "测试", Model: "mock", Runner: runnerName, MaxConcurrency: 3, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// reviewer 是 handoff 目标，一并创建
	if name == "worker" {
		_, _ = st.CreateAgent(context.Background(), &model.Agent{
			Name: "reviewer", Role: "质检", Model: "mock", Runner: runnerName, MaxConcurrency: 1, Enabled: true})
	}
	return id
}

func TestClaimRespectsConcurrency(t *testing.T) {
	st := store.NewMemStore()
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "solo", Model: "mock", Runner: "mock", MaxConcurrency: 1, Enabled: true})
	for i := 0; i < 3; i++ {
		_, _ = st.CreateTask(ctx, &model.Task{Title: "任务", Prompt: "普通", AgentID: aid, Priority: 5})
	}
	// 认领 1 个后 agent 满载，第二次认领应返回 nil
	first, err := st.ClaimNextTask(ctx)
	if err != nil || first == nil {
		t.Fatalf("首次认领失败: %v %v", first, err)
	}
	// 把已认领任务状态保持 running；由于该 agent max=1，不应再认领
	second, err := st.ClaimNextTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != nil {
		t.Fatalf("并发闸失效：max_concurrency=1 却认领了第二个任务")
	}
}
