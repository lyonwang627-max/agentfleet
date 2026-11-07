package api

import (
	"context"
	"encoding/json"
	"fmt"
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

// Skills CRUD（v1.4）+ chat 同步端点直测。

func newAssetServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, LLM: gateway.New("mock"), StartedAt: time.Now()}
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestSkillsCRUD(t *testing.T) {
	_, ts := newAssetServer(t)
	doJSON := func(method, path, body string) map[string]any {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		out["__status"] = resp.StatusCode
		return out
	}

	// 创建
	c := doJSON("POST", "/api/v1/skills", `{"name":"发布 SOP","content":"先审批再发布","tags":["ops"]}`)
	if c["__status"] != 201 {
		t.Fatalf("创建失败: %v", c)
	}
	id := fmt.Sprintf("%.0f", c["id"].(float64))

	// 列表带引用计数
	l := doJSON("GET", "/api/v1/skills", "")
	items := l["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["usage_count"].(float64) != 0 {
		t.Fatalf("列表异常: %v", l)
	}

	// 更新
	u := doJSON("PATCH", "/api/v1/skills/"+id, `{"content":"更新后的 SOP"}`)
	if u["__status"] != 200 {
		t.Fatalf("更新失败: %v", u)
	}

	// 删除
	d := doJSON("DELETE", "/api/v1/skills/"+id, "")
	if d["__status"] != 200 {
		t.Fatalf("删除失败: %v", d)
	}
	// 删除后再查 404
	g := doJSON("GET", "/api/v1/skills/"+id, "")
	if g["__status"] != 404 {
		t.Fatalf("删除后应 404: %v", g)
	}
}

func TestChatSyncShape(t *testing.T) {
	s, ts := newAssetServer(t)
	ctx := context.Background()
	if _, err := s.Store.CreateAgent(ctx, &model.Agent{Name: "t", Model: "mock", Runner: "mock", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/chat", strings.NewReader(
		`{"agent_id":1,"message":"同步 chat 测试"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || out["reply"] == "" || out["conversation_id"].(float64) <= 0 {
		t.Fatalf("chat 响应异常: %v", out)
	}
}
