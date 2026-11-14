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

// 长期记忆（ADR-0003 M1）：CRUD 生命周期 + 确定性注入预算。

func TestMemoryLifecycleHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "mem", Model: "mock", Runner: "mock", Enabled: true})

	// 创建（缺 content 400 / 正常 201 active——手动直写）
	r0 := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/memories", strings.NewReader(`{}`))
	r0.SetPathValue("id", itoaInt64(aid))
	w0 := httptest.NewRecorder()
	s.createMemory(w0, r0)
	if w0.Code != 400 {
		t.Fatalf("缺 content 应 400: %d", w0.Code)
	}
	r := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/memories",
		strings.NewReader(`{"content":"staging 库是只读副本","kind":"fact"}`))
	r.SetPathValue("id", itoaInt64(aid))
	w := httptest.NewRecorder()
	s.createMemory(w, r)
	if w.Code != 201 {
		t.Fatalf("创建应 201: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Status != "active" {
		t.Fatalf("手动直写应 active: %q", created.Status)
	}

	// 列表
	wl := httptest.NewRecorder()
	rl := httptest.NewRequest("GET", "/api/v1/agents/"+itoaInt64(aid)+"/memories", nil)
	rl.SetPathValue("id", itoaInt64(aid))
	s.listMemories(wl, rl)
	if wl.Code != 200 || !strings.Contains(wl.Body.String(), "staging") {
		t.Fatalf("列表不符: %d", wl.Code)
	}

	// expire → 状态过期
	re := httptest.NewRequest("POST", "/api/v1/memories/"+itoaInt64(created.ID)+"/expire", nil)
	re.SetPathValue("id", itoaInt64(created.ID))
	we := httptest.NewRecorder()
	s.memoryAction("expire")(we, re)
	if we.Code != 200 {
		t.Fatalf("expire 应 200: %d", we.Code)
	}
	mems, _ := st.ListMemories(ctx, aid, []string{"active"})
	if len(mems) != 0 {
		t.Fatalf("expire 后 active 应空: %d", len(mems))
	}

	// 删除 → 404
	rd := httptest.NewRequest("DELETE", "/api/v1/memories/"+itoaInt64(created.ID), nil)
	rd.SetPathValue("id", itoaInt64(created.ID))
	wd := httptest.NewRecorder()
	s.deleteMemory(wd, rd)
	if wd.Code != 200 {
		t.Fatalf("删除应 200: %d", wd.Code)
	}
	rd2 := httptest.NewRequest("DELETE", "/api/v1/memories/"+itoaInt64(created.ID), nil)
	rd2.SetPathValue("id", itoaInt64(created.ID))
	wd2 := httptest.NewRecorder()
	s.deleteMemory(wd2, rd2)
	if wd2.Code != 404 {
		t.Fatalf("重复删除应 404: %d", wd2.Code)
	}
}
