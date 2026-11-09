package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/eval"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// evals HTTP 全生命周期直测（此前 handler 0%）：建数据集→发起评测→轮询终态→报告形状。
// createEval 是异步入口（Start goroutine），用 mock 网关确定性收口。

func TestEvalsLifecycleHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	s.Eval = &eval.Engine{Store: st, LLM: gateway.New("mock")}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "e-test", Model: "mock", Runner: "mock", Enabled: true})
	did, _ := st.CreateDataset(ctx, &model.EvalDataset{Name: "生命周期集", Description: "HTTP 直测"})
	_ = st.SaveCases(ctx, did, []model.EvalCase{
		{Input: "请解释 延迟双删 的作用", Expected: map[string]any{"points": []string{"延迟双删"}}},
	})

	call := func(method, path, body string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.SetPathValue("id", strings.TrimPrefix(path, "/api/v1/evals/"))
		w := httptest.NewRecorder()
		switch method {
		case "GET":
			if strings.HasPrefix(path, "/api/v1/evals/") && len(path) > len("/api/v1/evals/") {
				s.getEval(w, r)
			} else if strings.HasPrefix(path, "/api/v1/evals") {
				s.listEvals(w, r)
			} else {
				s.listDatasets(w, r)
			}
		case "POST":
			s.createEval(w, r)
		}
		return w.Code, w.Body.String()
	}

	// 缺参 400
	if code, _ := call("POST", "/api/v1/evals", `{"agent_id":1}`); code != 400 {
		t.Fatalf("缺 dataset_id 应 400: %d", code)
	}
	// 发起评测 → 201 running
	code, body := call("POST", "/api/v1/evals", `{"agent_id":` + itoaInt64(aid) + `,"dataset_id":` + itoaInt64(did) + `}`)
	if code != 201 {
		t.Fatalf("createEval 应 201: %d %s", code, body)
	}
	var created struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(body), &created)
	if created.Status != "running" {
		t.Fatalf("初始状态应 running: %q", created.Status)
	}

	// 轮询终态（mock 引擎快速完成）
	var run *model.EvalRun
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		code2, body2 := call("GET", "/api/v1/evals/"+itoaInt64(created.ID), "")
		if code2 != 200 {
			t.Fatalf("getEval 应 200: %d", code2)
		}
		run = &model.EvalRun{}
		_ = json.Unmarshal([]byte(body2), run)
		if run.Status == "done" || run.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run == nil || run.Status != "done" {
		t.Fatalf("评测未完成: %+v", run)
	}
	if run.Report == nil || len(run.Report.Cases) != 1 || run.ScoreAvg != 10 {
		t.Fatalf("报告形状不符: %+v", run.Report)
	}

	// 列表两端点
	code3, body3 := call("GET", "/api/v1/evals", "")
	if code3 != 200 || !strings.Contains(body3, itoaInt64(created.ID)) {
		t.Fatalf("listEvals 应含新 run: %d", code3)
	}
	code4, body4 := call("GET", "/api/v1/eval-datasets", "")
	if code4 != 200 || !strings.Contains(body4, "生命周期集") {
		t.Fatalf("listDatasets 应含数据集: %d", code4)
	}
	// 不存在的 eval → 404
	code5, _ := call("GET", "/api/v1/evals/99999", "")
	if code5 != 404 {
		t.Fatalf("不存在的评测应 404: %d", code5)
	}
}
