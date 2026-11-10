package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 深度功能面 handler 回归锁（此前 0%）：F17-b 处方应用（自进化闭环另一半）/
// F18-c 路由观测 / I30 小队 CRUD。

func TestApplyPrescriptionHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "self-evolve", Model: "mock", Runner: "mock",
		SystemPrompt: "原始提示词", Enabled: true})
	eid, _ := st.CreateEvalRun(ctx, &model.EvalRun{AgentID: aid, Status: "done",
		Report: &model.EvalReport{ScoreAvg: 5, PassRate: 50,
			Cases:       []model.EvalCaseResult{{CaseID: 1, Score: 3, Pass: false}},
			Prescription: &model.Prescription{Diagnosis: "未分点", SystemPromptAddition: "作答必须分点。",
				Rationale: "结构化", FailureCases: 1}}})
	// 处方回填 eval 引用（Prescription 结构无循环依赖问题——直接改 report 落库态）
	if e, err := st.GetEvalRun(ctx, eid); err == nil && e.Report != nil && e.Report.Prescription != nil {
		e.Report.Prescription.BasedOnEval = eid
	}

	// 无处方 eval → 409
	eid2, _ := st.CreateEvalRun(ctx, &model.EvalRun{AgentID: aid, Status: "done",
		Report: &model.EvalReport{Cases: []model.EvalCaseResult{{CaseID: 1, Score: 5}}}})
	r0 := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/apply-prescription",
		strings.NewReader(`{"eval_id":`+itoaInt64(eid2)+`}`))
	r0.SetPathValue("id", itoaInt64(aid))
	w0 := httptest.NewRecorder()
	s.applyPrescription(w0, r0)
	if w0.Code != 409 {
		t.Fatalf("无处方应 409: %d", w0.Code)
	}

	// 正常应用：prompt 追加 addendum + 版本链发布
	r := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/apply-prescription",
		strings.NewReader(`{"eval_id":`+itoaInt64(eid)+`}`))
	r.SetPathValue("id", itoaInt64(aid))
	w := httptest.NewRecorder()
	s.applyPrescription(w, r)
	if w.Code != 200 {
		t.Fatalf("应用应 200: %d %s", w.Code, w.Body.String())
	}
	a, _ := st.GetAgent(ctx, aid)
	if !strings.Contains(a.SystemPrompt, "原始提示词") || !strings.Contains(a.SystemPrompt, "作答必须分点") {
		t.Fatalf("addendum 未追加: %q", a.SystemPrompt)
	}
	vers, _ := st.ListAgentVersions(ctx, aid)
	if len(vers) != 1 || !strings.Contains(vers[0].Note, "RSI") {
		t.Fatalf("版本链未发布: %+v", vers)
	}
}

func TestRoutingStatsHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "rt", Model: "mock", Runner: "mock", Enabled: true})
	// 10 任务 2 重试 → 20% healthy
	for i := 0; i < 10; i++ {
		tid, _ := st.CreateTask(ctx, &model.Task{Title: "t", Prompt: "p", AgentID: aid})
		if i < 2 {
			n := 1
			_ = st.UpdateTask(ctx, tid, store.TaskPatch{RetryCount: &n})
		}
	}
	w := httptest.NewRecorder()
	s.routingStats(w, httptest.NewRequest("GET", "/api/v1/routing/stats", nil))
	if w.Code != 200 {
		t.Fatalf("应 200: %d", w.Code)
	}
	var out struct {
		EscalationRate float64 `json:"escalation_rate_pct"`
		Health         string  `json:"health"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.EscalationRate != 20 || out.Health != "healthy" {
		t.Fatalf("升级率/健康度不符: %+v", out)
	}
}

func TestSquadsHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	lid, _ := st.CreateAgent(ctx, &model.Agent{Name: "lead", Model: "mock", Runner: "mock", Enabled: true})
	mid, _ := st.CreateAgent(ctx, &model.Agent{Name: "member", Model: "mock", Runner: "mock", Enabled: true})

	// 创建（leader 必为 agent；member 混编）
	r := httptest.NewRequest("POST", "/api/v1/squads",
		strings.NewReader(`{"name":"告警小队","leader_id":`+itoaInt64(lid)+`,"member_ids":[`+itoaInt64(mid)+`]}`))
	w := httptest.NewRecorder()
	s.createSquad(w, r)
	if w.Code != 201 {
		t.Fatalf("建队应 201: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 列表 + 详情
	wl := httptest.NewRecorder()
	s.listSquads(wl, httptest.NewRequest("GET", "/api/v1/squads", nil))
	if wl.Code != 200 || !strings.Contains(wl.Body.String(), "告警小队") {
		t.Fatalf("列表不符: %s", wl.Body.String())
	}
	rg := httptest.NewRequest("GET", "/api/v1/squads/"+itoaInt64(created.ID), nil)
	rg.SetPathValue("id", itoaInt64(created.ID))
	wg := httptest.NewRecorder()
	s.getSquad(wg, rg)
	if wg.Code != 200 {
		t.Fatalf("详情应 200: %d", wg.Code)
	}

	// 成员增删
	ra := httptest.NewRequest("POST", "/api/v1/squads/"+itoaInt64(created.ID)+"/members",
		strings.NewReader(`{"agent_id":`+itoaInt64(lid)+`}`))
	ra.SetPathValue("id", itoaInt64(created.ID))
	wa := httptest.NewRecorder()
	s.addSquadMember(wa, ra)
	if wa.Code != 200 && wa.Code != 201 {
		t.Fatalf("加成员应成功: %d %s", wa.Code, wa.Body.String())
	}
	// 删除
	rd := httptest.NewRequest("DELETE", "/api/v1/squads/"+itoaInt64(created.ID), nil)
	rd.SetPathValue("id", itoaInt64(created.ID))
	wd := httptest.NewRecorder()
	s.deleteSquad(wd, rd)
	if wd.Code != 200 {
		t.Fatalf("删队应 200: %d", wd.Code)
	}
	wl2 := httptest.NewRecorder()
	s.listSquads(wl2, httptest.NewRequest("GET", "/api/v1/squads", nil))
	if strings.Contains(wl2.Body.String(), "告警小队") {
		t.Fatal("删队后列表不应含")
	}
	_ = ctx
}
