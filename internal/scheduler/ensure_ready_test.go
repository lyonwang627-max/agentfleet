package scheduler

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// EnsureReady（I16 第 2 轮）：绑定 degraded workspace 的任务执行被拒（不烧模型算力）。
func TestEnsureReadyRejectsDegradedWorkspace(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "wsr", Model: "mock", Runner: "mock", Enabled: true})
	wid, _ := st.CreateWorkspace(ctx, &model.Workspace{Name: "坏环境", RootPath: "/nonexistent",
		RuntimeStatus: "degraded", RuntimeNote: "目录不存在", Status: "active"})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "环境任务", Prompt: "p", AgentID: aid, WorkspaceID: wid})
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})
	task, _ := st.GetTask(ctx, tid)

	s.execute(ctx, *task)

	t2, _ := st.GetTask(ctx, tid)
	// failTask 重试语义：预算内 queued（backoff 后环境可能恢复——容器重启中），耗尽 failed
	if t2.Status != model.TaskQueued && t2.Status != model.TaskFailed {
		t.Fatalf("degraded 环境任务应被拒并走重试语义: %s", t2.Status)
	}
	if !strings.Contains(t2.Error, "执行环境不可用") || !strings.Contains(t2.Error, "目录不存在") {
		t.Fatalf("错误应带探测原因: %q", t2.Error)
	}
}

// 对照：ready 环境任务正常执行（不拒绝）。
func TestEnsureReadyAllowsReadyWorkspace(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	capR := &captureRunner{}
	runner.Register("mock", capR) // 全局注册表：捕获型
	t.Cleanup(func() { runner.Register("mock", runner.NewMock()) })
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "wsg", Model: "mock", Runner: "mock", Enabled: true})
	wid, _ := st.CreateWorkspace(ctx, &model.Workspace{Name: "好环境", RootPath: t.TempDir(),
		RuntimeStatus: "ready", Status: "active"})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "好环境任务", Prompt: "p", AgentID: aid, WorkspaceID: wid})
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})
	task, _ := st.GetTask(ctx, tid)

	s.execute(ctx, *task)

	t2, _ := st.GetTask(ctx, tid)
	if t2.Status != model.TaskDone {
		t.Fatalf("ready 环境任务应正常执行: %s %q", t2.Status, t2.Error)
	}
}
