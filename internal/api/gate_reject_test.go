package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// I33 第 2 轮：review 状态下 cancel = 审批拒绝 → gate-reject 步留痕（与 approved-by-human 对称）。
func TestCancelOnReviewLeavesGateReject(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "rj", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "高风险任务", Prompt: "p", AgentID: aid})
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskReview), RiskLevel: ptrStr("high")})
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid, Status: model.RunRunning})

	r := httptest.NewRequest("POST", "/api/v1/tasks/"+itoaInt64(tid)+"/cancel", nil)
	r.SetPathValue("id", itoaInt64(tid))
	w := httptest.NewRecorder()
	s.taskAction(w, r)
	if w.Code != 200 {
		t.Fatalf("cancel 应 200: %d %s", w.Code, w.Body.String())
	}
	t2, _ := st.GetTask(ctx, tid)
	if t2.Status != model.TaskCanceled {
		t.Fatalf("应 canceled: %s", t2.Status)
	}
	steps, _ := st.ListSteps(ctx, rid)
	found := false
	for _, s := range steps {
		if s.Kind == model.StepGate && s.Name == "gate-reject" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺 gate-reject 步")
	}
	// 非 review 状态取消不产生 reject 留痕
	tid2, _ := st.CreateTask(ctx, &model.Task{Title: "排队任务", Prompt: "p", AgentID: aid})
	rid2, _ := st.CreateRun(ctx, &model.Run{TaskID: tid2, AgentID: aid, Status: model.RunRunning})
	r2 := httptest.NewRequest("POST", "/api/v1/tasks/"+itoaInt64(tid2)+"/cancel", nil)
	r2.SetPathValue("id", itoaInt64(tid2))
	w2 := httptest.NewRecorder()
	s.taskAction(w2, r2)
	steps2, _ := st.ListSteps(ctx, rid2)
	for _, s := range steps2 {
		if s.Name == "gate-reject" {
			t.Fatal("非 review 取消不应有 gate-reject")
		}
	}
	if !strings.Contains(w2.Body.String(), "action") {
		t.Fatalf("cancel 响应形状不符: %s", w2.Body.String())
	}
}
