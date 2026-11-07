package api

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 审批风险分级（I29）：high 任务 approve 无意见 400 / 带意见 200；medium 不强制。
func TestRiskLevelApprovalPolicy(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, StartedAt: time.Now()}
	ctx := context.Background()

	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "w", Model: "mock", Runner: "mock", Enabled: true})

	// high 风险任务：进 review 后无意见 approve → 400
	highID, _ := st.CreateTask(ctx, &model.Task{Title: "高风险", Prompt: "对外发布", AgentID: aid,
		Status: model.TaskReview, RiskLevel: "high"})
	wNoNote := httptest.NewRecorder()
	reqNo := httptest.NewRequest("POST", "/api/v1/tasks/"+strconvI(highID)+"/approve",
		strings.NewReader(`{}`))
	reqNo.SetPathValue("id", strconv.FormatInt(highID, 10))
	s.taskAction(wNoNote, reqNo)
	if wNoNote.Code != 400 {
		t.Fatalf("高风险无意见 approve 应 400: %d", wNoNote.Code)
	}

	// 带意见 → 200
	wNote := httptest.NewRecorder()
	reqYes := httptest.NewRequest("POST", "/api/v1/tasks/"+strconvI(highID)+"/approve",
		strings.NewReader(`{"note":"已确认发布窗口与回滚预案"}`))
	reqYes.SetPathValue("id", strconv.FormatInt(highID, 10))
	s.taskAction(wNote, reqYes)
	if wNote.Code != 200 {
		t.Fatalf("带意见 approve 应 200: %d", wNote.Code)
	}

	// medium 任务：无意见 approve → 200（不强制）
	medID, _ := st.CreateTask(ctx, &model.Task{Title: "中风险", Prompt: "常规审批", AgentID: aid,
		Status: model.TaskReview, RiskLevel: "medium"})
	wMed := httptest.NewRecorder()
	reqMed := httptest.NewRequest("POST", "/api/v1/tasks/"+strconvI(medID)+"/approve",
		strings.NewReader(`{}`))
	reqMed.SetPathValue("id", strconv.FormatInt(medID, 10))
	s.taskAction(wMed, reqMed)
	if wMed.Code != 200 {
		t.Fatalf("medium 无意见 approve 应 200: %d", wMed.Code)
	}
}
