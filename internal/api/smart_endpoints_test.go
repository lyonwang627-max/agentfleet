package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 三大智能端点 HTTP 回归锁（此前仅手工实弹）：F16 预审 / F18-c 评测对比 / F17 处方。
// precheck/prescribe 需 Healthy LLM——httptest 假 Ollama 端点（native 通道，同 native_test 先例）。

// ollamaFake 假 Ollama：/api/chat 返回固定 content（native 协议单响应）。
func ollamaFake(t *testing.T, content string) (*gateway.LLM, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":` + jsonString(content) + `},"eval_count":30}`))
	}))
	return gateway.NewWithBaseURL("fake", srv.URL), srv
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestPrecheckHTTP(t *testing.T) {
	precheckJSON := `{"recommendation":"deny","risk_level":"high","reason":"涉及对外发布且未回应诉求"}`
	fakeLLM, srv := ollamaFake(t, precheckJSON)
	defer srv.Close()

	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "p", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "预审任务", Prompt: "p", AgentID: aid})
	rq := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/tasks/"+itoaInt64(tid)+"/precheck", nil)
		r.SetPathValue("id", itoaInt64(tid))
		w := httptest.NewRecorder()
		s.precheckTask(w, r)
		return w
	}

	// 非 review → 409
	if w := rq(); w.Code != 409 {
		t.Fatalf("非 review 应 409: %d", w.Code)
	}
	// mock LLM → 503（预审需要真模型）
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskReview)})
	if w := rq(); w.Code != 503 {
		t.Fatalf("mock LLM 应 503: %d", w.Code)
	}
	// 真（假）LLM → 200 + gate 步留痕
	s.LLM = fakeLLM
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid})
	w := rq()
	if w.Code != 200 {
		t.Fatalf("预审应 200: %d %s", w.Code, w.Body.String())
	}
	var p PrecheckResult
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Recommendation != "deny" || p.RiskLevel != "high" {
		t.Fatalf("预审结果不符: %+v", p)
	}
	steps, _ := st.ListSteps(ctx, rid)
	found := false
	for _, s := range steps {
		if s.Kind == model.StepGate && s.Name == "ai-precheck" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺 ai-precheck gate 步")
	}
}

func TestCompareEvalsHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	mkRun := func(avg float64, pass float64, scoreA, scoreB int) int64 {
		id, _ := st.CreateEvalRun(context.Background(), &model.EvalRun{Status: "done"})
		_ = st.UpdateEvalRun(context.Background(), &model.EvalRun{ID: id, Status: "done",
			Report: &model.EvalReport{ScoreAvg: avg, PassRate: pass,
				Cases: []model.EvalCaseResult{
					{CaseID: 1, Score: scoreA, Pass: true}, {CaseID: 2, Score: scoreB, Pass: false}}}})
		return id
	}
	a, b := mkRun(5, 50, 5, 5), mkRun(8, 100, 9, 7)

	// 缺参 400 / 同 id 400
	w0 := httptest.NewRecorder()
	s.compareEvals(w0, httptest.NewRequest("GET", "/api/v1/evals/compare?a=1&b=1", nil))
	if w0.Code != 400 {
		t.Fatalf("同 id 应 400: %d", w0.Code)
	}
	// 不存在 404
	w1 := httptest.NewRecorder()
	s.compareEvals(w1, httptest.NewRequest("GET", "/api/v1/evals/compare?a=999&b=9999", nil))
	if w1.Code != 404 {
		t.Fatalf("不存在应 404: %d", w1.Code)
	}
	// 正常对比：5→8 improvement，case 翻转统计
	w := httptest.NewRecorder()
	s.compareEvals(w, httptest.NewRequest("GET", "/api/v1/evals/compare?a="+itoaInt64(a)+"&b="+itoaInt64(b), nil))
	if w.Code != 200 {
		t.Fatalf("对比应 200: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Verdict    string  `json:"verdict"`
		ScoreDelta float64 `json:"score_delta"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Verdict != "improvement" || out.ScoreDelta != 3 {
		t.Fatalf("对比结果不符: %+v", out)
	}
}

func TestPrescribeHTTP(t *testing.T) {
	rxJSON := `{"diagnosis":"失败模式：未分点","system_prompt_addition":"作答必须分点，每点一句。","rationale":"结构化提升可评性"}`
	fakeLLM, srv := ollamaFake(t, rxJSON)
	defer srv.Close()

	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	id, _ := st.CreateEvalRun(ctx, &model.EvalRun{Status: "done", ScoreAvg: 5, PassRate: 50,
		Report: &model.EvalReport{ScoreAvg: 5, PassRate: 50,
			Cases: []model.EvalCaseResult{{CaseID: 1, Score: 3, Pass: false, JudgeComment: "未分点"}}}})

	rq := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/evals/"+itoaInt64(id)+"/prescription", nil)
		r.SetPathValue("id", itoaInt64(id))
		w := httptest.NewRecorder()
		s.prescribe(w, r)
		return w
	}
	// mock LLM → 503
	if w := rq(); w.Code != 503 {
		t.Fatalf("mock 应 503: %d", w.Code)
	}
	// 真（假）LLM → 200 + 处方写回 report
	s.LLM = fakeLLM
	w := rq()
	if w.Code != 200 {
		t.Fatalf("处方应 200: %d %s", w.Code, w.Body.String())
	}
	var p model.Prescription
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if !strings.Contains(p.SystemPromptAddition, "分点") {
		t.Fatalf("处方内容不符: %+v", p)
	}
	e, _ := st.GetEvalRun(ctx, id)
	if e.Report == nil || e.Report.Prescription == nil || e.Report.Prescription.BasedOnEval != id {
		t.Fatalf("处方未写回报告: %+v", e.Report)
	}
}

// I40 第 2 轮：compare 版本对齐提示——数据集变更后对比不可信的字段级提示。
func TestCompareEvalsVersionAlignment(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	did, _ := st.CreateDataset(ctx, &model.EvalDataset{Name: "版本对比集"})
	mkRun := func(score float64) int64 {
		id, _ := st.CreateEvalRun(ctx, &model.EvalRun{DatasetID: did, AgentID: 1, Status: "done"})
		_ = st.UpdateEvalRun(ctx, &model.EvalRun{ID: id, Status: "done",
			Report: &model.EvalReport{ScoreAvg: score, PassRate: 100,
				Cases: []model.EvalCaseResult{{CaseID: 1, Score: int(score), Pass: true}}}})
		return id
	}
	a := mkRun(5)
	// 数据集未变更（version=1）：两 run 版本一致
	_ = st.SaveCases(ctx, did, []model.EvalCase{{Input: "题", Expected: nil}}) // version→2
	b := mkRun(8)

	cmp := func() map[string]any {
		r := httptest.NewRequest("GET", "/api/v1/evals/compare?a="+itoaInt64(a)+"&b="+itoaInt64(b), nil)
		w := httptest.NewRecorder()
		s.compareEvals(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return m
	}

	out := cmp()
	// a 在 version=1 评测、b 在 version=2 评测 → 版本不对齐提示
	if out["same_dataset_version"] != false {
		t.Fatalf("版本应不对齐: %+v", out["same_dataset_version"])
	}
	// 数据集未变更场景：两 run 同版本 → 对齐
	a2, b2 := mkRun(5), mkRun(8)
	r2 := httptest.NewRequest("GET", "/api/v1/evals/compare?a="+itoaInt64(a2)+"&b="+itoaInt64(b2), nil)
	w2 := httptest.NewRecorder()
	s.compareEvals(w2, r2)
	var out2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &out2)
	if out2["same_dataset_version"] != true {
		t.Fatalf("同版本应对齐: %v", out2["same_dataset_version"])
	}
	_ = cmp
}
