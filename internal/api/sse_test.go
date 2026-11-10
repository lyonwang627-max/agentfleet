package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// SSE 实时流（此前 handler 0%）：事件格式（id:/data:）+ retry 首连提示 + since 过滤 +
// 客户端断开退出 + runEvents 404。httptest.Recorder 实现 Flusher，直测流协议。

// safeRecorder 线程安全包装（SSE goroutine 写 / 测试主 goroutine 读，-race 要求）。
type safeRecorder struct {
	mu sync.Mutex
	w  *httptest.ResponseRecorder
}

func (s *safeRecorder) WriteHeader(code int) { s.mu.Lock(); defer s.mu.Unlock(); s.w.WriteHeader(code) }
func (s *safeRecorder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
func (s *safeRecorder) Flush()              { s.mu.Lock(); defer s.mu.Unlock(); s.w.Flush() }
func (s *safeRecorder) Header() http.Header { return s.w.Header() }
func (s *safeRecorder) body() string        { s.mu.Lock(); defer s.mu.Unlock(); return s.w.Body.String() }

func TestSSEGlobalEvents(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 预置 1 条历史事件（seq=1）
	hub.Publish(ctx, "task.update", map[string]any{"task_id": 7, "status": "queued"})

	r := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)
	w := &safeRecorder{w: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		s.globalEvents(w, r)
		close(done)
	}()

	// 等 backlog 回放 + 首连提示落盘
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(w.body(), "data: ") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// 实时事件：订阅后发布（应推送进来）
	hub.Publish(ctx, "agent.update", map[string]any{"agent_id": 5})
	time.Sleep(150 * time.Millisecond)
	cancel() // 客户端断开 → serveSSE 退出
	<-done

	body := w.body()
	if !strings.Contains(body, "text/event-stream") && w.w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type 应 event-stream: %v", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, "retry: 3000") {
		t.Fatal("首连应带 retry 提示")
	}
	if !strings.Contains(body, "id: 1") || !strings.Contains(body, `"task_id":7`) {
		t.Fatalf("历史事件未回放: %s", body[:min(len(body), 200)])
	}
	if !strings.Contains(body, `"agent_id":5`) {
		t.Fatal("实时事件未推送")
	}
	// 事件格式：id: 行 + data: 行成对
	if !strings.Contains(body, "\ndata: {") {
		t.Fatal("SSE 格式不符")
	}
}

func TestSSESinceFilter(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, LLM: nil, StartedAt: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub.Publish(ctx, "task.update", map[string]any{"task_id": 1}) // seq1
	hub.Publish(ctx, "task.update", map[string]any{"task_id": 2}) // seq2
	time.Sleep(50 * time.Millisecond)

	// since=1 → 只回放 seq2
	r := httptest.NewRequest("GET", "/api/v1/events?since=1", nil).WithContext(ctx)
	w := &safeRecorder{w: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		s.globalEvents(w, r)
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(w.body(), "data: ") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	body := w.body()
	if strings.Contains(body, "id: 1\n") {
		t.Fatal("since=1 不应回放 seq1")
	}
	if !strings.Contains(body, "id: 2") {
		t.Fatalf("应回放 seq2: %s", body)
	}
}

func TestSSERunEventsNotFound(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	r := httptest.NewRequest("GET", "/api/v1/runs/999/events", nil)
	r.SetPathValue("id", "99999")
	w := httptest.NewRecorder()
	s.runEvents(w, r)
	if w.Code != 404 {
		t.Fatalf("不存在的 run 应 404: %d", w.Code)
	}
}
