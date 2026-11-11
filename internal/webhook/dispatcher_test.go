package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Dispatcher（I7 第 2 轮）：轮询增量投递 / 指数退避重试 / 重试耗尽放弃 / 观测落库。

func TestDispatcherDeliversNewEvents(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer srv.Close()

	st := store.NewMemStore()
	ctx := context.Background()
	st.CreateWebhook(ctx, &model.Webhook{Name: "n", URL: srv.URL, Secret: "s",
		Events: []string{"task.done"}, Active: true})

	d := NewDispatcher(st)
	// 先产事件再直驱 scanOnce（Run 的初始化会跳过启动前历史——重启风暴防护语义）
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.done", Data: map[string]any{"task_id": 5}})
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.failed", Data: map[string]any{}}) // 未订阅

	d.scanOnce(context.Background())
	mu.Lock()
	if hits != 1 {
		h := hits
		mu.Unlock()
		t.Fatalf("应恰好投递 1 次（未订阅事件跳过）: %d", h)
	}
	mu.Unlock()
	// 事件已消费：再扫一轮不重复投递（增量 lastSeq）
	d.scanOnce(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("重复投递: %d", hits)
	}
}

func TestDispatcherRetryBackoffAndRecovery(t *testing.T) {
	fail := true
	var mu sync.Mutex
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		a := attempts
		mu.Unlock()
		if fail && a < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	st := store.NewMemStore()
	ctx := context.Background()
	_, _ = st.CreateWebhook(ctx, &model.Webhook{Name: "r", URL: srv.URL, Secret: "s",
		Events: []string{"*"}, Active: true})
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.done"})

	d := NewDispatcher(st)
	// scanOnce 直驱 + 时钟快进（1s/2s 退避内 3 次尝试收敛——fail 连挂 2 次后放行）
	orig := timeNow
	cur := timeNow()
	timeNow = func() time.Time { return cur }
	defer func() { timeNow = orig }()
	for i := 0; i < 30; i++ {
		cur = cur.Add(600 * time.Millisecond)
		d.scanOnce(ctx)
		mu.Lock()
		a := attempts
		mu.Unlock()
		hooks, _ := st.ListWebhooks(ctx)
		if a >= 3 && hooks[0].LastStatus == "ok" {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts < 3 {
		t.Fatalf("应重试至成功: %d", attempts)
	}
	hooks, _ := st.ListWebhooks(context.Background())
	if hooks[0].LastStatus != "ok" {
		t.Fatalf("观测状态应为 ok: %q", hooks[0].LastStatus)
	}
	if hooks[0].LastDeliveryAt == nil {
		t.Fatal("last_delivery_at 未落库")
	}
}

func TestDispatcherRetryExhausted(t *testing.T) {
	attempts := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError) // 永远失败
	}))
	defer srv.Close()

	st := store.NewMemStore()
	ctx := context.Background()
	_, _ = st.CreateWebhook(ctx, &model.Webhook{Name: "x", URL: srv.URL, Secret: "s",
		Events: []string{"*"}, Active: true})
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.done"})

	d := NewDispatcher(st)
	orig := timeNow
	cur := timeNow()
	timeNow = func() time.Time { return cur }
	defer func() { timeNow = orig }()
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.done"})
	// 快进时钟 + 直驱（1+2+4+8=15s 内 5 次尝试耗尽）
	for i := 0; i < 60 && attempts < retryMaxAttempts; i++ {
		cur = cur.Add(time.Second)
		d.scanOnce(ctx)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != retryMaxAttempts {
		t.Fatalf("应恰好尝试 %d 次后放弃: %d", retryMaxAttempts, attempts)
	}
	hooks, _ := st.ListWebhooks(context.Background())
	if !strings.Contains(hooks[0].LastStatus, "http 500") {
		t.Fatalf("观测状态应记录失败: %q", hooks[0].LastStatus)
	}
}
