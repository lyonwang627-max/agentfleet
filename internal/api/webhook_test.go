package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
	"github.com/lyonwang/agentfleet/internal/webhook"
)

// 出站 Webhook（I7 第 1 轮）：HMAC 签名 + 事件过滤 + CRUD。

func TestWebhookSignDeterministic(t *testing.T) {
	body := []byte(`{"kind":"task.update"}`)
	sig := webhook.Sign("secret-1", body)
	if sig != webhook.Sign("secret-1", body) {
		t.Fatal("签名应确定")
	}
	if sig == webhook.Sign("secret-2", body) {
		t.Fatal("不同 secret 签名应不同")
	}
	// 接收方校验路径（HMAC 标准用法）
	mac := hmac.New(sha256.New, []byte("secret-1"))
	mac.Write(body)
	if !hmac.Equal([]byte(sig), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		t.Fatal("签名与接收方重算不一致")
	}
}

func TestWebhookDeliverFiltered(t *testing.T) {
	var mu sync.Mutex
	var gotBody []byte
	var gotSig, gotEvent, gotDelivery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Fleet-Signature")
		gotEvent = r.Header.Get("X-Fleet-Event")
		gotDelivery = r.Header.Get("X-Fleet-Delivery")
	}))
	defer srv.Close()
	ctx := context.Background()
	hook := &model.Webhook{Name: "n", URL: srv.URL, Secret: "sec", Events: []string{"task.update"}, Active: true}

	// 未订阅事件 → 静默跳过（不请求）
	if err := webhook.Deliver(ctx, srv.Client(), hook, model.Event{Kind: "agent.update", Seq: 1}, "d1"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if gotBody != nil {
		t.Fatal("未订阅事件不应请求")
	}
	mu.Unlock()

	// 订阅事件 → 投递 + 签名头
	if err := webhook.Deliver(ctx, srv.Client(), hook, model.Event{Kind: "task.update", Seq: 2,
		Data: map[string]any{"task_id": 9}}, "d2"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotEvent != "task.update" || !strings.Contains(string(gotBody), `"task_id":9`) {
		t.Fatalf("投递内容不符: %s %s", gotEvent, gotBody)
	}
	// 幂等头（I7 第 3 轮）：接收方按 ID 去重的依据——重试链内稳定
	if gotDelivery != "d2" {
		t.Fatalf("X-Fleet-Delivery 透传不符: %q", gotDelivery)
	}
	// 接收方重算签名
	mac := hmac.New(sha256.New, []byte("sec"))
	mac.Write(gotBody)
	if gotSig != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("签名不符: %s", gotSig)
	}
}

func TestWebhookDeliveryFailureStatus(t *testing.T) {
	// 500 响应 → 投递错误（第 2 轮重试退避的输入）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	hook := &model.Webhook{URL: srv.URL, Secret: "s", Events: []string{"*"}, Active: true}
	err := webhook.Deliver(context.Background(), srv.Client(), hook, model.Event{Kind: "task.update"}, "d3")
	if err == nil || !strings.Contains(err.Error(), "http 500") {
		t.Fatalf("5xx 应报错: %v", err)
	}
}

func TestWebhookCRUDHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}

	// 创建（缺 url 400）
	r0 := httptest.NewRequest("POST", "/api/v1/webhooks", strings.NewReader(`{"name":"n"}`))
	w0 := httptest.NewRecorder()
	s.createWebhook(w0, r0)
	if w0.Code != 400 {
		t.Fatalf("缺 url 应 400: %d", w0.Code)
	}
	// 创建 → 201 + secret 一次性返回
	r := httptest.NewRequest("POST", "/api/v1/webhooks",
		strings.NewReader(`{"name":"客户系统","url":"https://hooks.example/x","events":["task.done","task.failed"]}`))
	w := httptest.NewRecorder()
	s.createWebhook(w, r)
	if w.Code != 201 {
		t.Fatalf("创建应 201: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID     int64  `json:"id"`
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Secret == "" || !strings.HasPrefix(created.Secret, "whk-") {
		t.Fatalf("secret 应一次性返回: %q", created.Secret)
	}
	// 库内 secret 保留（签名用）
	hooks, _ := st.ListWebhooks(context.Background())
	if len(hooks) != 1 || hooks[0].Secret != created.Secret {
		t.Fatalf("库内 secret 不符: %+v", hooks)
	}
	// 列表
	wl := httptest.NewRecorder()
	s.listWebhooks(wl, httptest.NewRequest("GET", "/api/v1/webhooks", nil))
	if wl.Code != 200 || !strings.Contains(wl.Body.String(), "客户系统") {
		t.Fatalf("列表不符: %s", wl.Body.String())
	}
	// 删除
	rd := httptest.NewRequest("DELETE", "/api/v1/webhooks/"+itoaInt64(created.ID), nil)
	rd.SetPathValue("id", itoaInt64(created.ID))
	wd := httptest.NewRecorder()
	s.deleteWebhook(wd, rd)
	if wd.Code != 200 {
		t.Fatalf("删除应 200: %d", wd.Code)
	}
	wd2 := httptest.NewRecorder()
	rd2 := httptest.NewRequest("DELETE", "/api/v1/webhooks/"+itoaInt64(created.ID), nil)
	rd2.SetPathValue("id", itoaInt64(created.ID))
	s.deleteWebhook(wd2, rd2)
	if wd2.Code != 404 {
		t.Fatalf("重复删除应 404: %d", wd2.Code)
	}
}

// 入站回调（I37，与出站对称）：token 校验 → payload 映射建任务 → 事件留痕。
func TestIngestWebhookHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "ingest-a", Model: "mock", Runner: "mock", Enabled: true})
	hid, _ := st.CreateWebhook(ctx, &model.Webhook{Name: "in", URL: "https://x/y",
		Secret: "sec-1", Events: []string{"*"}, Active: true})
	hooks, _ := st.ListWebhooks(ctx)
	token := webhook.IngestToken(&hooks[0])

	call := func(hookID int64, tok, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/webhooks/"+itoaInt64(hookID)+"/ingest/"+tok,
			strings.NewReader(body))
		r.SetPathValue("id", itoaInt64(hookID))
		r.SetPathValue("token", tok)
		w := httptest.NewRecorder()
		s.ingestWebhook(w, r)
		return w
	}

	// 无效 token → 401
	if w := call(hid, "bad-token", `{"agent_id":` + itoaInt64(aid) + `,"title":"t","prompt":"p"}`); w.Code != 401 {
		t.Fatalf("无效 token 应 401: %d", w.Code)
	}
	// 缺字段 → 400
	if w := call(hid, token, `{"agent_id":` + itoaInt64(aid) + `}`); w.Code != 400 {
		t.Fatalf("缺 title/prompt 应 400: %d", w.Code)
	}
	// agent 不存在 → 400
	if w := call(hid, token, `{"agent_id":99999,"title":"t","prompt":"p"}`); w.Code != 400 {
		t.Fatalf("agent 不存在应 400: %d", w.Code)
	}
	// 正常 → 201 建任务 + webhook 标签 + 事件留痕
	w := call(hid, token, `{"agent_id":` + itoaInt64(aid) + `,"title":"告警进站","prompt":"处理告警","priority":8,"labels":["ops"]}`)
	if w.Code != 201 {
		t.Fatalf("正常应 201: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		TaskID int64 `json:"task_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	task, _ := st.GetTask(ctx, created.TaskID)
	if task.Title != "告警进站" || task.Priority != 8 {
		t.Fatalf("任务映射不符: %+v", task)
	}
	// 来源标签（webhook:<id>）
	tagged := false
	for _, l := range task.Labels {
		if strings.HasPrefix(l, "webhook:") {
			tagged = true
		}
	}
	if !tagged || len(task.Labels) != 2 {
		t.Fatalf("来源标签不符: %v", task.Labels)
	}
	// ingest 事件留痕
	evs, _ := st.EventsSince(ctx, 0, 50)
	found := false
	for _, e := range evs {
		if e.Kind == "webhook.ingest" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺 webhook.ingest 事件")
	}
}
