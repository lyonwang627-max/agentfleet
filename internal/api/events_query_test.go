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

// 相对时间语法（I39）：since=NOW()-<dur> → 按 created_at 过滤；绝对 seq 兼容。
func TestEventsQueryRelativeTime(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	s.Hub.Publish(ctx, "task.update", map[string]any{"task_id": 1}) // 旧事件
	time.Sleep(30 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(30 * time.Millisecond)
	_ = cutoff
	s.Hub.Publish(ctx, "task.update", map[string]any{"task_id": 2}) // 新事件

	call := func(since string) (int, string) {
		r := httptest.NewRequest("GET", "/api/v1/events/query?since="+since, nil)
		w := httptest.NewRecorder()
		s.queryEvents(w, r)
		return w.Code, w.Body.String()
	}

	// NOW()-30ms：cutoff 后的新事件命中、旧事件过滤（两事件间 sleep 30ms）
	code, body := call("NOW()-30ms")
	if code != 200 || !strings.Contains(body, `"task_id":2`) || strings.Contains(body, `"task_id":1`) {
		t.Fatalf("相对时间过滤不符: %d %s", code, body[:min(len(body), 150)])
	}
	// NOW()-1h：全部命中
	_, body = call("NOW()-1h")
	if !strings.Contains(body, `"task_id":1`) || !strings.Contains(body, `"task_id":2`) {
		t.Fatalf("1h 应全命中: %s", body[:min(len(body), 200)])
	}
	// 纯 seq 兼容：since=1 → 只 seq2
	code, body = call("1")
	if code != 200 || !strings.Contains(body, `"task_id":2`) || strings.Contains(body, `"task_id":1`) {
		t.Fatalf("seq 兼容不符: %d", code)
	}
	// 非法相对时间 → 回退 seq 解析（非数字 → 0 → 全量）
	code, _ = call("NOW()-bad")
	if code != 200 {
		t.Fatalf("非法相对时间应回退: %d", code)
	}
	// 天单位：NOW()-1d 全命中
	code, body = call("NOW()-1d")
	if code != 200 || !strings.Contains(body, `"task_id":1`) {
		t.Fatalf("天单位不符: %d", code)
	}
}
