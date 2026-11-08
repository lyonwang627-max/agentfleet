package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// cooperativeRunner 长任务：循环检查 CancelCheck（调度器注入的 DB 状态检查），
// 模拟多轮工具循环在步边界的协作式取消。
type cooperativeRunner struct{ step1Done chan struct{} }

func (r *cooperativeRunner) Execute(ctx context.Context, req runner.ExecRequest) (runner.Result, error) {
	close(r.step1Done)
	for i := 0; i < 200; i++ { // 最多 ~4s，等取消信号
		if req.CancelCheck != nil && req.CancelCheck() {
			return runner.Result{}, runner.ErrCanceled
		}
		select {
		case <-ctx.Done():
			return runner.Result{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return runner.Result{Reply: "should not finish"}, nil
}

// I1 端到端（调度器级）：cancel 后 runner 协作退出，任务终态 canceled、
// 不被 failTask 重试（无 queued 回退）、run 带 canceled 标注。
func TestCancelPropagationEndToEnd(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	cr := &cooperativeRunner{step1Done: make(chan struct{})}
	runner.Register("cooperative", cr)
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	aid, err := st.CreateAgent(ctx, &model.Agent{Name: "coop", Model: "mock", Runner: "cooperative",
		MaxConcurrency: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := st.CreateTask(ctx, &model.Task{Title: "可取消长任务", Prompt: "长任务", AgentID: aid, Priority: 5})

	// 等任务真正 running（runner 已进入步边界等待区）
	<-cr.step1Done

	// cancel 端点等价操作：状态直接置 canceled
	fin := time.Now()
	_ = st.UpdateTask(ctx, taskID, store.TaskPatch{Status: statusPtr3(model.TaskCanceled), FinishedAt: &fin})

	// 等 runner 协作退出 + 调度器收尾（任务应保持 canceled，而非 done/queued 重试）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tsk, _ := st.GetTask(ctx, taskID)
		if tsk != nil && tsk.Status == model.TaskCanceled {
			// 再等一小段确认没有后续状态翻转（failTask 不会把它改 queued）
			time.Sleep(200 * time.Millisecond)
			final, _ := st.GetTask(ctx, taskID)
			if final.Status != model.TaskCanceled {
				t.Fatalf("canceled 被翻转: %+v", final.Status)
			}
			// run 带 canceled 标注
			runs, _ := st.ListRuns(ctx, taskID)
			if len(runs) == 0 || runs[0].Error == "" {
				t.Fatalf("run 缺 canceled 标注: %+v", runs)
			}
			return // ✓ 全链路通过
		}
		time.Sleep(50 * time.Millisecond)
	}
	tsk, _ := st.GetTask(ctx, taskID)
	t.Fatalf("取消未正确传播收尾: %+v", tsk)
}

func statusPtr3(s model.TaskStatus) *model.TaskStatus { return &s }
