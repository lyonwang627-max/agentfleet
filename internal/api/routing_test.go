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

// 路由 A/B 对比（F18-c）：verdict 判定 + 结论翻转统计（MemStore 全真路径）。
func TestCompareEvalsVerdict(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()

	// 造两个已完成的评测（同 dataset 不同分数）
	ea := &model.EvalRun{DatasetID: 1, AgentID: 1, Status: "done",
		Report: &model.EvalReport{Cases: []model.EvalCaseResult{
			{CaseID: 1, Score: 6, Pass: true}, {CaseID: 2, Score: 5, Pass: false}},
			ScoreAvg: 5.5, PassRate: 50}}
	eb := &model.EvalRun{DatasetID: 1, AgentID: 1, Status: "done",
		Report: &model.EvalReport{Cases: []model.EvalCaseResult{
			{CaseID: 1, Score: 8, Pass: true}, {CaseID: 2, Score: 8, Pass: true}},
			ScoreAvg: 8, PassRate: 100}}
	_, _ = s.Store.CreateEvalRun(ctx, ea)
	_ = s.Store.UpdateEvalRun(ctx, ea)
	idB, _ := s.Store.CreateEvalRun(ctx, eb)
	_ = s.Store.UpdateEvalRun(ctx, eb)
	_ = idB

	req := httptest.NewRequest("GET", "/api/v1/evals/compare?a=1&b=2", nil)
	w := httptest.NewRecorder()
	s.compareEvals(w, req)
	if w.Code != 200 {
		t.Fatalf("compare 应 200，得到 %d: %s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"verdict":"improvement"`) {
		t.Fatal("5.5→8 应判 improvement")
	}
	if !contains(w.Body.String(), `"divergent_cases":1`) {
		t.Fatal("case2 翻转（fail→pass）应计 1")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
