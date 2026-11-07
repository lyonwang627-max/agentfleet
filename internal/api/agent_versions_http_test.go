package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Agent 版本管理 HTTP 层（F2 handler 直测）：publish → list → rollback 全流程。

func TestAgentVersionsHTTPFlow(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()
	srcID, _ := st.CreateAgent(ctx, &model.Agent{
		Name: "coder", Role: "开发", SystemPrompt: "v0", Model: "qwen3.5",
		Runner: "mock", MaxConcurrency: 1, Enabled: true})

	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, ts_url(path), strings.NewReader(body))
		req.SetPathValue("id", fmt.Sprint(srcID))
		// rollback 路径形如 /versions/{version}/rollback——version 在倒数第二段
		if parts := strings.Split(strings.Trim(path, "/"), "/"); len(parts) >= 2 && parts[len(parts)-1] == "rollback" {
			req.SetPathValue("version", parts[len(parts)-2])
		}
		w := httptest.NewRecorder()
		switch method {
		case "POST":
			if strings.HasSuffix(path, "/publish") {
				s.publishAgent(w, req)
			} else {
				s.rollbackAgent(w, req)
			}
		case "GET":
			s.listAgentVersions(w, req)
		}
		return w
	}

	// publish v1
	w1 := call("POST", fmt.Sprintf("/api/v1/agents/%d/publish", srcID), `{"note":"首次发布"}`)
	if w1.Code != 201 {
		t.Fatalf("publish 应 201: %d %s", w1.Code, w1.Body.String())
	}

	// list：1 条 v1
	wl := call("GET", fmt.Sprintf("/api/v1/agents/%d/versions", srcID), "")
	var listed struct {
		Items []struct {
			Version int    `json:"version"`
			Note    string `json:"note"`
		} `json:"items"`
		Total int `json:"total"`
	}
	_ = json.NewDecoder(wl.Body).Decode(&listed)
	if listed.Total != 1 || listed.Items[0].Version != 1 {
		t.Fatalf("版本列表异常: %+v", listed)
	}

	// rollback 到 v1（当前也是 v1，语义上仍产生新版本 v2）
	wr := call("POST", fmt.Sprintf("/api/v1/agents/%d/versions/1/rollback", srcID), "")
	if wr.Code != 200 {
		t.Fatalf("rollback 应 200: %d %s", wr.Code, wr.Body.String())
	}
	var rb struct {
		NewVersion int `json:"new_version"`
	}
	_ = json.NewDecoder(wr.Body).Decode(&rb)
	if rb.NewVersion != 2 {
		t.Fatalf("回滚应产生 v2: %+v", rb)
	}

	// 链上 2 条，最新 v2
	wl2 := call("GET", fmt.Sprintf("/api/v1/agents/%d/versions", srcID), "")
	_ = json.NewDecoder(wl2.Body).Decode(&listed)
	if listed.Total != 2 || listed.Items[0].Version != 2 {
		t.Fatalf("版本链异常: %+v", listed)
	}
}

// ts_url 辅助：httptest 内嵌 handler 直调，不需要真 server——占位保持签名清晰。
func ts_url(path string) string { return path }
