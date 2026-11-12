package webhook

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Dispatcher 事件扫描 → 命中订阅并发投递（I7 第 2 轮）。
// 轮询 EventsSince(lastSeq)：重启从最新 seq 起（跳过历史，防重启风暴）——at-most-once 语义。
// 失败重试：指数退避（1s/2s/4s…上限 30s，attempt≤5），内存队列。

const (
	retryMaxAttempts = 5
	retryBase        = time.Second
	retryMax         = 30 * time.Second
)

type Dispatcher struct {
	Store    store.Store
	Client   *http.Client
	Interval time.Duration // 扫描周期

	mu      sync.Mutex
	lastSeq int64
	retries map[int64]*retryTask // hookID -> 待重试任务
	started bool
}

type retryTask struct {
	hook      *model.Webhook
	event     model.Event
	attempt   int
	nextRetry time.Time
}

func NewDispatcher(st store.Store) *Dispatcher {
	return &Dispatcher{Store: st, Client: &http.Client{Timeout: 5 * time.Second},
		Interval: 2 * time.Second, retries: map[int64]*retryTask{}}
}

// Run 主循环（fleetd goroutine；ctx 取消退出）。
func (d *Dispatcher) Run(ctx context.Context) {
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return
	}
	d.started = true
	// 重启从最新事件起：不重放历史（避免重启投递风暴）
	if evs, _ := d.Store.EventsSince(ctx, 0, 1); len(evs) > 0 {
		d.lastSeq = evs[len(evs)-1].Seq
	} else {
		d.lastSeq = 0
	}
	d.mu.Unlock()
	log.Printf("[webhook] dispatcher 启动（interval=%s，lastSeq=%d）", d.Interval, d.lastSeq)

	t := time.NewTicker(d.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.scanOnce(ctx)
		}
	}
}

// scanOnce 单轮：新事件投递 + 重试队列到期的再投。
// 锁纪律：d.mu 只护 lastSeq/retries 读写；Deliver 是网络 IO，绝不可持锁调用
// （dispatch 内部会再取锁——不可重入，曾在此死锁）。
func (d *Dispatcher) scanOnce(ctx context.Context) {
	hooks, err := d.Store.ListWebhooks(ctx)
	if err != nil {
		return
	}
	active := map[int64]*model.Webhook{}
	for i := range hooks {
		if hooks[i].Active {
			active[hooks[i].ID] = &hooks[i]
		}
	}

	// 1) 新事件（增量拉取）：先快照 seq，投递完成后再推进
	since := d.currSeq()
	evs, _ := d.Store.EventsSince(ctx, since, 100)
	for _, ev := range evs {
		for _, h := range active {
			if Matches(h, ev.Kind) {
				d.dispatch(ctx, h, ev, 0) // 首投 attempt=0
			}
		}
		if ev.Seq > since {
			since = ev.Seq
		}
	}
	d.mu.Lock()
	if since > d.lastSeq {
		d.lastSeq = since
	}
	// 2) 重试队列到期项：attempt 随条目保留（移出再重建会重置计数——曾致退避永不耗尽）
	now := timeNow()
	var due []*retryTask
	for id, rt := range d.retries {
		h, ok := active[rt.hook.ID]
		if !ok || !h.Active {
			delete(d.retries, id) // 订阅已删/停：放弃重试
			continue
		}
		if now.Before(rt.nextRetry) {
			continue
		}
		due = append(due, rt)
	}
	d.mu.Unlock()
	for _, rt := range due {
		d.dispatch(ctx, rt.hook, rt.event, rt.attempt)
	}
}

// dispatch 单次投递 + 状态落库 + 失败入重试队列（自身管理锁，不可在持锁区调用）。
// prevAttempt：首投 0，重试传条目累计值——attempt 只增不减。
func (d *Dispatcher) dispatch(ctx context.Context, h *model.Webhook, ev model.Event, prevAttempt int) {
	err := Deliver(ctx, d.Client, h, ev, DeliveryID(h, ev))
	status := "ok"
	if err != nil {
		status = fmt.Sprintf("%.80v", err)
	}
	_ = d.Store.SetWebhookStatus(ctx, h.ID, status)
	if err == nil {
		d.mu.Lock()
		delete(d.retries, h.ID) // 成功清队列
		d.mu.Unlock()
		return
	}
	attempt := prevAttempt + 1
	d.mu.Lock()
	defer d.mu.Unlock()
	if attempt >= retryMaxAttempts {
		log.Printf("[webhook] hook#%d %s 重试耗尽（%d 次），放弃: %v", h.ID, ev.Kind, attempt, err)
		delete(d.retries, h.ID)
		return
	}
	backoff := retryBase << (attempt - 1) // 1s/2s/4s/8s
	if backoff > retryMax {
		backoff = retryMax
	}
	rt, ok := d.retries[h.ID]
	if !ok {
		rt = &retryTask{hook: h, event: ev}
		d.retries[h.ID] = rt
	}
	rt.attempt = attempt
	rt.nextRetry = timeNow().Add(backoff)
	log.Printf("[webhook] hook#%d 投递失败（第 %d 次），%.0fs 后重试: %v", h.ID, attempt, backoff.Seconds(), err)
}

// currSeq 并发安全读。
func (d *Dispatcher) currSeq() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastSeq
}

var timeNow = time.Now // 可替换时钟（重试退避测试用）
