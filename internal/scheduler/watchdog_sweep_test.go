package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 进展看门狗 sweep 直测（F14 分支补齐）：阈值内跳过 / 超阈值停滞重试 / fail-open 放行 /
// digestSteps 摘要与截断。JudgeProgress 单测见 watchdog_test.go。

func newWatchdogSched(t *testing.T) (*Scheduler, *store.MemStore, context.Context, int64) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, err := st.CreateAgent(ctx, &model.Agent{Name: "wd", Model: "mock", Runner: "mock", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return s, st, ctx, aid
}

func runningTask(t *testing.T, st *store.MemStore, ctx context.Context, aid int64, startedAgo time.Duration) int64 {
	tid, err := st.CreateTask(ctx, &model.Task{Title: "长任务", Prompt: "普通", AgentID: aid})
	if err != nil {
		t.Fatal(err)
	}
	started := timeNow().Add(-startedAgo)
	if err := st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning), StartedAt: ptrTime(started)}); err != nil {
		t.Fatal(err)
	}
	return tid
}

func TestSweepProgressStallRetries(t *testing.T) {
	s, st, ctx, aid := newWatchdogSched(t)
	// 超阈值 4 分钟（阈值 3 分钟）且无 run 记录 → digest 走「无执行记录」兜底
	tid := runningTask(t, st, ctx, aid, 4*time.Minute)
	s.SetJudge(&fakeJudge{healthy: true, raw: `{"progress": false, "reason": "无任何执行步骤"}`})

	s.sweepProgress(ctx)

	// task.stalled 事件已发
	evs, _ := st.EventsSince(ctx, 0, 100)
	stalled := false
	for _, e := range evs {
		if e.Kind == "task.stalled" {
			stalled = true
		}
	}
	if !stalled {
		t.Fatal("未发出 task.stalled 事件")
	}
	// failTask 走重试语义：queued（重试预算内）或 failed（耗尽），且 error 带停滞说明
	t2, _ := st.GetTask(ctx, tid)
	if t2.Status != model.TaskQueued && t2.Status != model.TaskFailed {
		t.Fatalf("停滞任务应被重试/判死: %s", t2.Status)
	}
	if !strings.Contains(t2.Error, "停滞") {
		t.Fatalf("error 缺停滞说明: %q", t2.Error)
	}
}

func TestSweepProgressYoungTaskSkipped(t *testing.T) {
	s, st, ctx, aid := newWatchdogSched(t)
	// 阈值内（1 分钟 < 3 分钟）：即使 judge 会判停滞也不检查
	tid := runningTask(t, st, ctx, aid, 1*time.Minute)
	s.SetJudge(&fakeJudge{healthy: true, raw: `{"progress": false, "reason": "无论如何停滞"}`})

	s.sweepProgress(ctx)

	t2, _ := st.GetTask(ctx, tid)
	if t2.Status != model.TaskRunning {
		t.Fatalf("阈值内任务不应被打扰: %s", t2.Status)
	}
	evs, _ := st.EventsSince(ctx, 0, 100)
	for _, e := range evs {
		if e.Kind == "task.stalled" {
			t.Fatal("阈值内不应发 stalled 事件")
		}
	}
}

func TestSweepProgressFailOpen(t *testing.T) {
	s, st, ctx, aid := newWatchdogSched(t)
	// 超阈值但 judge 不健康 → 放行不误杀
	tid := runningTask(t, st, ctx, aid, 4*time.Minute)
	s.SetJudge(&fakeJudge{healthy: false})

	s.sweepProgress(ctx)

	t2, _ := st.GetTask(ctx, tid)
	if t2.Status != model.TaskRunning {
		t.Fatalf("fail-open 应放行: %s", t2.Status)
	}
}

func TestDigestSteps(t *testing.T) {
	// 空 → 兜底文案
	if got := digestSteps(nil); got != "（run 无任何步骤）" {
		t.Fatalf("空 steps 兜底不符: %q", got)
	}
	// 正常摘要行格式
	got := digestSteps([]model.RunStep{{Kind: model.StepLLM, Name: "decide", LatencyMS: 120}})
	if !strings.Contains(got, "[llm]") || !strings.Contains(got, "decide") || !strings.Contains(got, "120ms") {
		t.Fatalf("摘要格式不符: %q", got)
	}
	// 超 1200 rune 截断：大量步骤不再增长
	many := make([]model.RunStep, 0, 200)
	for i := 0; i < 200; i++ {
		many = append(many, model.RunStep{Kind: model.StepTool, Name: strings.Repeat("s", 50), LatencyMS: 1})
	}
	if n := len([]rune(digestSteps(many))); n > 2000 {
		t.Fatalf("摘要未截断: %d rune", n)
	}
}
