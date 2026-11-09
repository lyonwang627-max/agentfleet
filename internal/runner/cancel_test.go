package runner

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// slowRunner 两个工具步之间可观察取消：step1 后检查 CancelCheck，为 true 则返回 ErrCanceled。
type slowRunner struct {
	step1Done chan struct{}
}

func (r *slowRunner) Execute(ctx context.Context, req ExecRequest) (Result, error) {
	if req.Recorder != nil {
		_ = req.Recorder(ctx, &model.RunStep{Kind: model.StepTool, Name: "step1"})
	}
	close(r.step1Done)
	// 等待取消信号或超时（模拟长任务在步边界等待）
	for i := 0; i < 100; i++ {
		if req.CancelCheck != nil && req.CancelCheck() {
			return Result{}, ErrCanceled
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return Result{Reply: "跑完了"}, nil
}

// I1 取消传播（单元级）：CancelCheck 翻转后 runner 在步边界返回 ErrCanceled。
func TestCancelCheckPropagates(t *testing.T) {
	var canceled atomic.Bool // runner goroutine 并发读，须原子（-race 检出的裸 bool 竞争）
	check := func() bool { return canceled.Load() }

	r := &slowRunner{step1Done: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := r.Execute(context.Background(), ExecRequest{
			Task: model.Task{Title: "t", Prompt: "p"}, CancelCheck: check})
		done <- err
	}()

	<-r.step1Done   // 等 step1 完成（进入步边界等待区）
	canceled.Store(true) // 模拟 cancel 端点触发

	select {
	case err := <-done:
		if !errors.Is(err, ErrCanceled) {
			t.Fatalf("应返回 ErrCanceled: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消未在 2s 内传播")
	}
}

// mock runner 入口检查：CancelCheck=true 直接 ErrCanceled。
func TestMockRunnerCancelAtEntry(t *testing.T) {
	_, err := NewMock().Execute(context.Background(), ExecRequest{
		Task:        model.Task{Title: "t", Prompt: "普通任务，直接完成。"},
		CancelCheck: func() bool { return true },
	})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("mock 入口取消应 ErrCanceled: %v", err)
	}
}
