package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 事件过滤查询（I22）：kind 过滤 + task_id 过滤 + limit。
func TestQueryEventsFilters(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()

	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.update", Data: map[string]any{"task_id": 1, "status": "queued"}})
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "run.step", Data: map[string]any{"run_id": 9, "task_id": 1}})
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.update", Data: map[string]any{"task_id": 2, "status": "done"}})

	// kind 过滤
	req := httptest.NewRequest("GET", "/api/v1/events/query?kind=run.step", nil)
	w := httptest.NewRecorder()
	s.queryEvents(w, req)
	var out struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&out)
	if out.Total != 1 || out.Items[0]["kind"] != "run.step" {
		t.Fatalf("kind 过滤异常: %+v", out)
	}

	// task_id 过滤（data 内嵌字段）
	req2 := httptest.NewRequest("GET", "/api/v1/events/query?task_id=2", nil)
	w2 := httptest.NewRecorder()
	s.queryEvents(w2, req2)
	_ = json.NewDecoder(w2.Body).Decode(&out)
	if out.Total != 1 {
		t.Fatalf("task_id 过滤异常: %+v", out)
	}

	// limit
	req3 := httptest.NewRequest("GET", "/api/v1/events/query?limit=1", nil)
	w3 := httptest.NewRecorder()
	s.queryEvents(w3, req3)
	_ = json.NewDecoder(w3.Body).Decode(&out)
	if out.Total != 1 {
		t.Fatalf("limit 失效: %+v", out)
	}
}
