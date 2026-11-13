package webhook

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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

// 幂等头核心语义（I7 第 3 轮）：重试链内 X-Fleet-Delivery 稳定——接收方按 ID 去重的依据。
func TestDeliveryIdStableAcrossRetries(t *testing.T) {
	var mu sync.Mutex
	seenDeliveries := []string{}
	failUntil := 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenDeliveries = append(seenDeliveries, r.Header.Get("X-Fleet-Delivery"))
		n := len(seenDeliveries)
		mu.Unlock()
		if n <= failUntil {
			w.WriteHeader(http.StatusInternalServerError) // 前两次失败逼出重试
		}
	}))
	defer srv.Close()

	st := store.NewMemStore()
	ctx := context.Background()
	hook := &model.Webhook{Name: "idem", URL: srv.URL, Secret: "s", Events: []string{"*"}, Active: true}
	st.CreateWebhook(ctx, hook)
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.done"})

	d := NewDispatcher(st)
	orig := timeNow
	cur := timeNow()
	timeNow = func() time.Time { return cur }
	defer func() { timeNow = orig }()
	for i := 0; i < 20; i++ {
		cur = cur.Add(time.Second)
		d.scanOnce(ctx)
		mu.Lock()
		n := len(seenDeliveries)
		mu.Unlock()
		if n >= 3 {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seenDeliveries) < 3 {
		t.Fatalf("重试链未收敛: %v", seenDeliveries)
	}
	// 全部投递（首投+重试）共享同一 delivery ID
	for i, dv := range seenDeliveries {
		if dv != seenDeliveries[0] || dv == "" {
			t.Fatalf("第 %d 次投递 ID 不稳定: %q vs %q", i+1, dv, seenDeliveries[0])
		}
	}
}

// Run 主循环（ticker 驱动）：启动后产事件 → tick 内投递（真 goroutine 路径）。
func TestDispatcherRunTick(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer srv.Close()

	st := store.NewMemStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _ = st.CreateWebhook(ctx, &model.Webhook{Name: "tick", URL: srv.URL, Secret: "s",
		Events: []string{"*"}, Active: true})

	d := NewDispatcher(st)
	d.Interval = 40 * time.Millisecond
	go d.Run(ctx)
	time.Sleep(60 * time.Millisecond) // 等初始化落定（lastSeq 快照）
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.done"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		h := hits
		mu.Unlock()
		if h >= 1 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits < 1 {
		t.Fatalf("tick 循环未投递: %d", hits)
	}
}

// slog 字段化断言（I41 第 2 轮）：重试日志 hook_id/attempt 为独立字段而非消息文本。
func TestSlogFieldsInRetry(t *testing.T) {
	buf := &lockedBuffer2{}
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	attempts := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError) // 恒失败逼出重试日志
	}))
	defer srv.Close()

	st := store.NewMemStore()
	ctx := context.Background()
	_, _ = st.CreateWebhook(ctx, &model.Webhook{Name: "sf", URL: srv.URL, Secret: "s",
		Events: []string{"*"}, Active: true})
	d := NewDispatcher(st)
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.done"})

	orig := timeNow
	cur := timeNow()
	timeNow = func() time.Time { return cur }
	defer func() { timeNow = orig }()
	for i := 0; i < 8; i++ {
		cur = cur.Add(time.Second)
		d.scanOnce(ctx)
	}

	// 解析 JSON 行：投递失败记录的 hook_id/attempt 为独立字段
	found := false
	for _, line := range strings.Split(buf.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec["msg"] == "webhook 投递失败，退避重试" {
			found = true
			if rec["hook_id"].(float64) != 1 {
				t.Fatalf("hook_id 字段不符: %v", rec["hook_id"])
			}
			if _, ok := rec["attempt"]; !ok {
				t.Fatal("缺 attempt 字段")
			}
			break
		}
	}
	if !found {
		t.Fatalf("缺结构化重试日志: %s", buf.String()[:min(len(buf.String()), 200)])
	}
}

type lockedBuffer2 struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer2) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer2) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
