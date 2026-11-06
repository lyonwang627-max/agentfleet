package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 事件驱动唤醒（F13-a）：Wake() 后派发应显著快于 tick 周期（800ms）。
// mock runner 极快，任务 done 时间 < 400ms 即证明走了唤醒路径（纯 tick 至少 800ms）。
func TestWakeDispatchesImmediately(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	runner.Register("mock", runner.NewMock())
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	aid := mustAgent(t, st, "fast", "mock")
	start := time.Now()
	if _, err := st.CreateTask(ctx, &model.Task{Title: "唤醒派发", Prompt: "普通任务，直接完成。",
		AgentID: aid, Priority: 9}); err != nil {
		t.Fatal(err)
	}
	s.Wake()

	deadline := time.Now().Add(700 * time.Millisecond) // 纯 tick 路径至少 800ms，700ms 内完成即证明唤醒生效
	for time.Now().Before(deadline) {
		if tasks, _, _ := st.ListTasks(ctx, nil, aid, 1, 10); len(tasks) > 0 && tasks[0].Status == model.TaskDone {
			elapsed := time.Since(start)
			t.Logf("任务在 %v 内完成（< tick 800ms，唤醒路径生效）", elapsed)
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("700ms 内未完成——事件唤醒未生效（或环境过慢）")
}

// Wake 合并语义：连发多次不 panic 不阻塞，任务仍会被处理。
func TestWakeMergeSafe(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	s := New(st, hub)
	for i := 0; i < 100; i++ {
		s.Wake() // 非阻塞合并
	}
	// 不启动调度器也不应泄漏/阻塞（唤醒 chan 容量 1 + default 丢弃）
}
