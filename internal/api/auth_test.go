package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 认证渐进语义（I19）：无 key 开放 → 建 key 即强制 → 有效 key 放行 → 错 key 403 → 撤销恢复开放。
func TestAuthProgressiveSemantics(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ts := httptest.NewServer(s.authMiddleware(s.Routes()))
	defer ts.Close()

	get := func(path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", ts.URL+path, nil)
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		w := httptest.NewRecorder()
		handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		handler.ServeHTTP(w, req)
		return w
	}

	// 1) 无 key：开放
	if w := get("/api/v1/tasks", ""); w.Code != 200 {
		t.Fatalf("无 key 阶段应开放: %d", w.Code)
	}

	// 2) 创建 key（明文仅此一次）
	wc := httptest.NewRequest("POST", ts.URL+"/api/v1/auth/keys", strings.NewReader(`{"name":"ci"}`))
	wk := httptest.NewRecorder()
	s.createAuthKey(wk, wc)
	if wk.Code != 201 {
		t.Fatalf("建 key 失败: %d", wk.Code)
	}
	var created struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	_ = json.NewDecoder(wk.Body).Decode(&created)
	if !strings.HasPrefix(created.Key, "fleet-") {
		t.Fatalf("key 前缀异常: %q", created.Key)
	}

	// 3) 有 key：无 header 401
	if w := get("/api/v1/tasks", ""); w.Code != 401 {
		t.Fatalf("认证启用后无 key 应 401: %d", w.Code)
	}
	// 4) 错 key 403
	if w := get("/api/v1/tasks", "fleet-wrong"); w.Code != 403 {
		t.Fatalf("错 key 应 403: %d", w.Code)
	}
	// 5) 有效 key 200
	if w := get("/api/v1/tasks", created.Key); w.Code != 200 {
		t.Fatalf("有效 key 应 200: %d", w.Code)
	}
	// 6) metrics/health 豁免
	if w := get("/metrics", ""); w.Code != 200 {
		t.Fatalf("metrics 应豁免: %d", w.Code)
	}

	// 7) 撤销后恢复开放
	del := httptest.NewRequest("DELETE", ts.URL+"/api/v1/auth/keys/"+strconvI(created.ID), nil)
	del.SetPathValue("id", strconvI(created.ID))
	wr := httptest.NewRecorder()
	s.revokeAuthKey(wr, del)
	if wr.Code != 200 {
		t.Fatalf("撤销失败: %d", wr.Code)
	}
	if w := get("/api/v1/tasks", ""); w.Code != 200 {
		t.Fatalf("撤销后应恢复开放: %d", w.Code)
	}
}

func strconvI(n int64) string { return strconv.FormatInt(n, 10) }
