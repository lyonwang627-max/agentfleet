package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 模型级冷却（I35）：连续失败计数 → 冷却 → 到期回轮换；成功重置；TierToModel 过滤。

func TestCooldownLifecycle(t *testing.T) {
	l, srv := newNativeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusTooManyRequests)
	})
	defer srv.Close()
	// 快速时钟推进探测：替换 timeNow
	orig := timeNow
	cur := timeNow()
	timeNow = func() time.Time { return cur }
	defer func() { timeNow = orig }()

	// 连续 3 次失败 → 进入冷却
	for i := 0; i < 3; i++ {
		_, _, _ = l.native(context.Background(), "s", "u", false, 100, nil)
	}
	if !l.IsCooling(l.Model) {
		t.Fatal("3 次连续失败应进入冷却")
	}
	if len(l.CoolingList()) != 1 {
		t.Fatal("CoolingList 应含冷却模型")
	}

	// 冷却期内 → 到期后回轮换
	cur = cur.Add(modelCooldown + time.Second)
	if l.IsCooling(l.Model) {
		t.Fatal("冷却到期应回轮换")
	}

	// 成功重置：失败 2 次 + 1 成功 → 计数清零，再 1 次失败不冷却
	failMode := true
	l2, srv2 := newNativeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		if failMode {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"message":{"content":"ok"},"eval_count":1}`))
	})
	defer srv2.Close()
	_, _, _ = l2.native(context.Background(), "s", "u", false, 100, nil)
	_, _, _ = l2.native(context.Background(), "s", "u", false, 100, nil)
	failMode = false
	_, _, _ = l2.native(context.Background(), "s", "u", false, 100, nil) // 成功重置
	failMode = true
	_, _, _ = l2.native(context.Background(), "s", "u", false, 100, nil) // 再 1 次失败
	if l2.IsCooling(l2.Model) {
		t.Fatal("成功应重置计数——2 失败 + 1 成功 + 1 失败不应冷却")
	}
}

func TestSelectForSkipsCooling(t *testing.T) {
	t.Setenv("FLEET_TIER_S", "tiny-model:latest")
	t.Setenv("FLEET_TIER_M", "main-model")
	// 假端点同时服务 /api/tags（tiny-model 可用）与 /api/chat
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"tiny-model:latest"},{"name":"main-model"}]}`))
		default:
			w.Write([]byte(`{"message":{"content":"ok"},"eval_count":1}`))
		}
	}))
	defer srv.Close()
	l := NewWithBaseURL("main-model", srv.URL)
	orig := timeNow
	cur := timeNow()
	timeNow = func() time.Time { return cur }
	defer func() { timeNow = orig }()

	// 前置：tiny-model 未冷却 → S 档命中
	if got := l.SelectFor(PurposeJudge); got != "tiny-model:latest" {
		t.Fatalf("未冷却应命中 S 档: %q", got)
	}
	// tiny-model 冷却中 → S 档被过滤 → 回退 Execute 档（main-model）
	l.Model = "tiny-model:latest"
	l.cooling = &coolingState{until: cur.Add(time.Hour)}
	if got := l.SelectFor(PurposeJudge); got != "main-model" {
		t.Fatalf("冷却中 S 档应回退 M 档: %q", got)
	}
}

func gatewayNewCoolingTest() *LLM {
	l := New("mock")
	return l
}

// WithModel 派生视图与原视图共享冷却状态（I35 重构语义锁：cooling 指针随值拷贝共享，
// 双视图并发安全——recordFailure 在派生视图上生效，原视图 IsCooling 同步可见）。
func TestWithModelSharesCooling(t *testing.T) {
	l := gatewayNewCoolingTest()
	derived := l.WithModel("other-model")
	// 派生视图上 recordFailure（3 连败 → 冷却）
	for i := 0; i < 3; i++ {
		derived.recordFailure()
	}
	// 派生视图自身冷却（IsCooling 按自身 Model 匹配）
	if !derived.IsCooling("other-model") {
		t.Fatal("派生视图自身应冷却")
	}
	// 原视图按自身 Model 查询——单槽冷却按 model 名匹配，跨模型查询不可见（单槽限制锁）
	if l.IsCooling("mock") {
		t.Fatal("原视图自身模型不应受派生视图冷却影响")
	}
}

// Chat mock Fallback 注入（与 runner 测试同款用法——gateway 包内锁定）。
func TestChatMockFallbackPaths(t *testing.T) {
	l := New("mock")
	l.Fallback = func(system, user string) string { return `{"reply":"fb"}` }
	// Chat → Fallback 输出
	reply, _, err := l.Chat(context.Background(), "sys", "u", 100)
	if err != nil || reply != `{"reply":"fb"}` {
		t.Fatalf("Chat Fallback 不符: %q %v", reply, err)
	}
	// ChatJSON → 同 Fallback
	raw, _, err := l.ChatJSON(context.Background(), "sys", "u", 100)
	if err != nil || string(raw) != `{"reply":"fb"}` {
		t.Fatalf("ChatJSON Fallback 不符: %s %v", raw, err)
	}
	// StreamChat mock 无 Fallback → 逐字前缀
	full := ""
	tokens, err := l.StreamChat(context.Background(), "s", "内容", 100, func(chunk string) { full += chunk })
	if err != nil || !strings.Contains(full, "(mock) 内容") || tokens == 0 {
		t.Fatalf("StreamChat mock 形态不符: %q %d %v", full, tokens, err)
	}
}

// ChatJSONSchema（I42）：payload.format 传 schema 对象（strict 级契约锁）。
func TestChatJSONSchemaPayload(t *testing.T) {
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Write([]byte(`{"message":{"content":"{\"score\":8}"},"eval_count":5}`))
	}))
	defer srv.Close()
	l := NewWithBaseURL("m", srv.URL)
	schema := json.RawMessage(`{"type":"object","required":["score"]}`)
	raw, _, err := l.ChatJSONSchema(context.Background(), "sys", "usr", 200, schema)
	if err != nil || string(raw) != `{"score":8}` {
		t.Fatalf("ChatJSONSchema 不符: %s %v", raw, err)
	}
	// format 是 schema 对象而非 "json" 字符串（strict 级判定）
	fmt_, ok := gotPayload["format"].(map[string]any)
	if !ok || fmt_["type"] != "object" {
		t.Fatalf("format 应为 schema 对象: %v", gotPayload["format"])
	}
}
