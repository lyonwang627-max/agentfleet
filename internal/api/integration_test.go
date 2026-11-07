package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/eval"
	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Q1 集成测试：httptest 起完整 Routes（MemStore + mock 网关/runner），
// 走 HTTP 全链路：健康 → 建 agent → 建任务 → 调度执行 → trace → 审批闭环 → 用量计量。
type testEnv struct {
	ts  *httptest.Server
	st  store.Store
	ctx context.Context
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	gw := gateway.New("mock")
	runner.Register("mock", runner.NewMock())
	runner.Register("llm", runner.NewLLM(gw))
	sched := scheduler.New(st, hub)
	eng := &eval.Engine{Store: st, LLM: gw}
	sched.Start(context.Background())
	t.Cleanup(sched.Stop)
	s := &Server{Store: st, Hub: hub, LLM: gw, Eval: eng, Sched: sched, StartedAt: time.Now()}
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return &testEnv{ts: ts, st: st, ctx: context.Background()}
}

func (e *testEnv) do(t *testing.T, method, path string, body any) map[string]any {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequestWithContext(e.ctx, method, e.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out) // 空体/非 JSON → 空map，不 panic
	out["__status"] = resp.StatusCode
	return out
}

func (e *testEnv) waitTask(t *testing.T, id int64, want string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out := e.do(t, "GET", fmt.Sprintf("/api/v1/tasks/%d", id), nil)
		if out["__status"] == 200 && out["status"] == want {
			return out
		}
		time.Sleep(80 * time.Millisecond)
	}
	out := e.do(t, "GET", fmt.Sprintf("/api/v1/tasks/%d", id), nil)
	t.Fatalf("任务未在 %v 内到达 %s: %v", timeout, want, out["status"])
	return nil
}

func TestFullFlowIntegration(t *testing.T) {
	e := newTestEnv(t)

	// 1) health
	h := e.do(t, "GET", "/api/v1/health", nil)
	if h["__status"] != 200 || h["status"] != "ok" {
		t.Fatalf("health 异常: %v", h)
	}

	// 2) 建 agent
	a := e.do(t, "POST", "/api/v1/agents", map[string]any{
		"name": "worker", "role": "集成测试", "runner": "mock", "model": "mock", "max_concurrency": 2})
	if a["__status"] != 201 {
		t.Fatalf("建 agent 失败: %v", a)
	}
	agentID := int64(a["id"].(float64))
	if _, err := e.st.CreateAgent(e.ctx, &model.Agent{
		Name: "reviewer", Role: "质检", Model: "mock", Runner: "mock", MaxConcurrency: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	// 3) 建普通任务 → 等 done → trace 有步骤
	task := e.do(t, "POST", "/api/v1/tasks", map[string]any{
		"title": "集成普通任务", "prompt": "普通任务，直接完成。", "agent_id": agentID, "priority": 5})
	if task["__status"] != 201 {
		t.Fatalf("建任务失败: %v", task)
	}
	id1 := int64(task["id"].(float64))
	done := e.waitTask(t, id1, "done", 8*time.Second)
	if done["result"] == "" {
		t.Fatal("result 为空")
	}
	// 该端点返回裸数组（契约如此）
	reqRuns, _ := http.NewRequestWithContext(e.ctx, "GET", e.ts.URL+fmt.Sprintf("/api/v1/tasks/%d/runs", id1), nil)
	respRuns, err := http.DefaultClient.Do(reqRuns)
	if err != nil {
		t.Fatal(err)
	}
	var runArr []map[string]any
	_ = json.NewDecoder(respRuns.Body).Decode(&runArr)
	respRuns.Body.Close()
	if len(runArr) == 0 {
		t.Fatal("无 run 记录")
	}

	// 4) 审批门任务 → review → approve → done
	gate := e.do(t, "POST", "/api/v1/tasks", map[string]any{
		"title": "集成审批任务", "prompt": "对外发布操作，需要审批。", "agent_id": agentID, "priority": 7})
	id2 := int64(gate["id"].(float64))
	e.waitTask(t, id2, "review", 8*time.Second)
	ap := e.do(t, "POST", fmt.Sprintf("/api/v1/tasks/%d/approve", id2), map[string]any{"note": "集成测试放行"})
	if ap["__status"] != 200 {
		t.Fatalf("approve 失败: %v", ap)
	}
	e.waitTask(t, id2, "done", 8*time.Second)

	// 5) 用量计量有数（run 回填）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		u := e.do(t, "GET", "/api/v1/usage?window=24h", nil)
		if u["requests"].(float64) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	u := e.do(t, "GET", "/api/v1/usage?window=24h", nil)
	if u["requests"].(float64) == 0 {
		t.Fatal("用量未回填")
	}
}
