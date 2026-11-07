package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// CRUD 回归锁：automations（含非法 cron 400）与 workspaces（创建/更新/删除）。

func TestAutomationsCRUD(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, LLM: nil, StartedAt: time.Now()}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "w", Model: "mock", Runner: "mock", Enabled: true})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", ts.URL+"/api/v1/automations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.createAutomation(w, req)
		return w
	}

	// 合法创建（agent_id 用真实 id）
	w := post(`{"agent_id":` + itoaInt64(aid) + `,"name":"晨会","cron_expr":"30 9 * * 1-5","title":"晨会摘要","priority":6}`)
	if w.Code != 201 {
		t.Fatalf("创建应 201: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID      int64  `json:"id"`
		NextRun string `json:"next_run"`
	}
	_ = json.NewDecoder(w.Body).Decode(&created)
	if created.ID == 0 || created.NextRun == "" {
		t.Fatalf("创建响应异常: %+v", created)
	}

	// 非法 cron → 400
	if w2 := post(`{"agent_id":` + itoaInt64(aid) + `,"name":"bad","cron_expr":"99 * * * *","title":"x"}`); w2.Code != 400 {
		t.Fatalf("非法 cron 应 400: %d", w2.Code)
	}

	// PATCH 改 cron → next_run 重算
	patch := httptest.NewRequest("PATCH", ts.URL+"/api/v1/automations/"+itoa(created.ID),
		strings.NewReader(`{"cron_expr":"0 8 * * *"}`))
	patch.SetPathValue("id", itoa(created.ID))
	w3 := httptest.NewRecorder()
	s.patchAutomation(w3, patch)
	if w3.Code != 200 || !strings.Contains(w3.Body.String(), "next_run") {
		t.Fatalf("PATCH 异常: %d %s", w3.Code, w3.Body.String())
	}

	// DELETE 后 GET 404
	del := httptest.NewRequest("DELETE", ts.URL+"/api/v1/automations/"+itoa(created.ID), nil)
	del.SetPathValue("id", itoa(created.ID))
	w4 := httptest.NewRecorder()
	s.deleteAutomation(w4, del)
	req := httptest.NewRequest("GET", ts.URL+"/api/v1/automations/"+itoa(created.ID), nil)
	req.SetPathValue("id", itoa(created.ID))
	w5 := httptest.NewRecorder()
	s.getAutomation(w5, req)
	if w5.Code != 404 {
		t.Fatalf("删除后应 404: %d", w5.Code)
	}
}

func TestWorkspaceCRUD(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()

	// 创建（缺 name 400）
	reqBad := httptest.NewRequest("POST", "/api/v1/workspaces", strings.NewReader(`{"branch":"x"}`))
	wBad := httptest.NewRecorder()
	s.createWorkspace(wBad, reqBad)
	if wBad.Code != 400 {
		t.Fatalf("缺 name 应 400: %d", wBad.Code)
	}

	// 创建
	req := httptest.NewRequest("POST", "/api/v1/workspaces",
		strings.NewReader(`{"name":"hotfix","root_path":"/tmp/hf","branch":"main"}`))
	w := httptest.NewRecorder()
	s.createWorkspace(w, req)
	if w.Code != 201 {
		t.Fatalf("创建应 201: %d", w.Code)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(w.Body).Decode(&created)

	// PATCH 归档
	archived := "archived"
	if err := s.Store.UpdateWorkspace(ctx, created.ID, store.WorkspacePatch{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	// active=true 过滤后应不可见
	ws, _ := s.Store.ListWorkspaces(ctx, true)
	if len(ws) != 0 {
		t.Fatalf("归档后 active 列表应空: %+v", ws)
	}

	// 删除
	del := httptest.NewRequest("DELETE", "/api/v1/workspaces/"+itoa(created.ID), nil)
	del.SetPathValue("id", itoa(created.ID))
	wDel := httptest.NewRecorder()
	s.deleteWorkspace(wDel, del)
	if wDel.Code != 200 {
		t.Fatalf("删除失败: %d", wDel.Code)
	}
}

func itoaInt64(n int64) string { return strconv.FormatInt(n, 10) }
