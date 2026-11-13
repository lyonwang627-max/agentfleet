package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 并发闸超时（I24）。
//
// 架构认知（本测试确立）：ClaimNextTask 已在认领时强制并发检查——卡死的 runner
// 占住闸后，后续任务留在 queued（到不了 execute 的闸等待，该场景由看门狗与
// 依赖级联治理）。闸超时覆盖的是「认领与执行之间的竞态窗口」，因此测试手动
// 占闸 + 直调 execute 精确命中该路径。
func TestGateTimeoutRequeues(t *testing.T) {
	origTimeout := gateAcquireTimeout.Load() // atomic：与在途 execute goroutine 并发（-race）
	gateAcquireTimeout.Store(int64(150 * time.Millisecond))
	defer func() { gateAcquireTimeout.Store(origTimeout) }()

	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, err := st.CreateAgent(ctx, &model.Agent{Name: "stuck", Model: "mock", Runner: "mock",
		MaxConcurrency: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	// 手动占闸（模拟认领竞态窗口内 runner 卡死）
	g := s.gate(aid, 1)
	g <- struct{}{}

	taskID, err := st.CreateTask(ctx, &model.Task{Title: "竞态窗口任务", Prompt: "普通", AgentID: aid})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := st.GetTask(ctx, taskID)
	// execute 要求 status=running 形状（dispatch 正常路径会先置 running）；直调时手动置
	_ = st.UpdateTask(ctx, taskID, store.TaskPatch{Status: statusPtr2(model.TaskRunning)})

	// 直调 execute（绕过 dispatch——竞态窗口内被派发的等价形态）
	go s.execute(ctx, *task)

	// 闸超时 → 延迟重排队（queued + error 说明）而非 failed
	// 轮询窗口 4s：race 全量负载下 goroutine 调度抖动（曾 2s 偶发错过窗口）
	deadline := time.Now().Add(4 * time.Second)
	var got *model.Task
	for time.Now().Before(deadline) {
		t2, _ := st.GetTask(ctx, taskID)
		if t2 != nil && t2.Status == model.TaskQueued && strings.Contains(t2.Error, "并发闸等待超时") {
			got = t2
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got == nil {
		evs, _ := st.EventsSince(ctx, 0, 100)
		t.Fatalf("闸超时重排队未生效; events=%v", evs)
	}
	// 告警事件已发出（轮询等待——emit 在 execute goroutine 于 UpdateTask 之后，
	// 主测试看到 queued 时 emit 可能还在调度窗口内未执行）
	evDeadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(evDeadline) && !found {
		evs, _ := st.EventsSince(ctx, 0, 100)
		for _, e := range evs {
			if e.Kind == "agent.gate_timeout" {
				found = true
			}
		}
		if !found {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("未发出 agent.gate_timeout 事件")
	}
	// 释放闸让 execute goroutine 退出（不阻塞测试收尾）
	<-g
}
