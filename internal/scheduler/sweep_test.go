package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Automations 触发（F1）：到期 → 建任务 → 推进 next_run；未到期不重复触发。
func TestRunAutomationsFiresAndAdvances(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid := mustAgent(t, st, "cron-worker", "mock")

	// next_run 设为过去 → 立即到期
	next := time.Now().Add(-time.Minute)
	autoID, err := st.CreateAutomation(ctx, &model.Automation{
		AgentID: aid, Name: "到期规则", CronExpr: "* * * * *",
		Title: "自动任务", Prompt: "普通任务，直接完成。", Priority: 5, Enabled: true, NextRun: &next})
	if err != nil {
		t.Fatal(err)
	}
	s.runAutomations(ctx)

	tasks, _, _ := st.ListTasks(ctx, nil, aid, 1, 50)
	if len(tasks) == 0 || tasks[0].Title != "自动任务" {
		t.Fatalf("到期规则未建任务: %+v", tasks)
	}
	a, _ := st.GetAutomation(ctx, autoID)
	if a.NextRun == nil || !a.NextRun.After(time.Now().Add(-time.Second)) {
		t.Fatalf("next_run 未推进到未来: %+v", a.NextRun)
	}
	// 再跑一轮：next_run 已在未来 → 不应重复建任务
	s.runAutomations(ctx)
	tasks2, _, _ := st.ListTasks(ctx, nil, aid, 1, 50)
	if len(tasks2) != len(tasks) {
		t.Fatalf("未到期规则被重复触发: %d → %d", len(tasks), len(tasks2))
	}
}

func TestRunAutomationsDisablesInvalidExpr(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid := mustAgent(t, st, "w2", "mock")

	bad := "* * * *" // 4 域，非法
	autoID, _ := st.CreateAutomation(ctx, &model.Automation{
		AgentID: aid, Name: "坏规则", CronExpr: bad, Title: "x", Prompt: "", Enabled: true})
	s.runAutomations(ctx)

	a, _ := st.GetAutomation(ctx, autoID)
	if a.Enabled {
		t.Fatal("非法表达式规则未被自动禁用")
	}
	// 禁用后不再触发（不建任务）
	tasks, _, _ := st.ListTasks(ctx, nil, aid, 1, 10)
	if len(tasks) != 0 {
		t.Fatalf("禁用规则仍建了任务: %+v", tasks)
	}
}

// 依赖失败级联（sweepDependents）：前置 failed → 依赖任务自动 failed 并带说明。
func TestSweepDependentsCascade(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid := mustAgent(t, st, "w3", "mock")

	depID, _ := st.CreateTask(ctx, &model.Task{Title: "会失败的前置", Prompt: "失败", AgentID: aid})
	fin := time.Now()
	_ = st.UpdateTask(ctx, depID, store.TaskPatch{Status: statusPtr2(model.TaskFailed), FinishedAt: &fin})

	childID, _ := st.CreateTask(ctx, &model.Task{Title: "依赖任务", Prompt: "普通", AgentID: aid, DependsOn: []int64{depID}})
	s.sweepDependents(ctx)

	child, _ := st.GetTask(ctx, childID)
	if child.Status != model.TaskFailed {
		t.Fatalf("级联失败未生效: %+v", child)
	}
	if !strings.Contains(child.Error, "前置任务") {
		t.Fatalf("级联说明缺失: %q", child.Error)
	}
}

func statusPtr2(s model.TaskStatus) *model.TaskStatus { return &s }
