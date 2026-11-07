package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Hub 事件中枢：Publish 先持久化（拿全局 seq，供断线重连按 Last-Event-ID 续传），
// 再扇出到所有 SSE 订阅者。订阅者缓冲满时丢弃并关闭（慢客户端不拖垮主干）。
type Hub struct {
	store   store.Store
	mu      sync.Mutex
	subs    map[int64]chan model.Event // 订阅 id -> 通道
	lastSeq int64
	nextSub int64
}

func NewHub(st store.Store) *Hub {
	return &Hub{store: st, subs: map[int64]chan model.Event{}}
}

func (h *Hub) Publish(ctx context.Context, kind string, data map[string]any) {
	e := &model.Event{Kind: kind, Data: data}
	seq, err := h.store.AppendEvent(ctx, e)
	if err != nil {
		log.Printf("[hub] 持久化事件失败: %v", err)
		return
	}
	h.mu.Lock()
	h.lastSeq = seq
	for id, ch := range h.subs {
		select {
		case ch <- *e:
		default:
			close(ch)
			delete(h.subs, id) // 慢消费者踢掉，客户端 SSE 自动重连后按 Last-Event-ID 补
		}
	}
	h.mu.Unlock()
}

// Subscribe 返回事件通道；自 since 起先回放历史（断线重连），再收实时流。
func (h *Hub) Subscribe(ctx context.Context, since int64) (<-chan model.Event, func()) {
	h.mu.Lock()
	h.nextSub++
	id := h.nextSub
	ch := make(chan model.Event, 64)
	h.subs[id] = ch
	h.mu.Unlock()

	// 历史回放（≤200 条），在后台 goroutine 先入队，实时事件随后自然衔接
	go func() {
		backlog, err := h.store.EventsSince(ctx, since, 200)
		if err != nil {
			return
		}
		for _, e := range backlog {
			select {
			case ch <- e:
			default:
				return
			}
		}
	}()

	cancel := func() {
		h.mu.Lock()
		if _, ok := h.subs[id]; ok {
			close(ch)
			delete(h.subs, id)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

func (h *Hub) LastSeq() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSeq
}

func (h *Hub) WaitReady(ctx context.Context) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := h.store.Ping(ctx); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return h.store.Ping(ctx)
}
