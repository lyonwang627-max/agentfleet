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

// native（Ollama 原生 /api/chat 通道）直测：此前全项目所有测试都走 mock 或 StreamChat，
// 生产主通道的 HTTP 语义（成功解析/错误路径/forceJSON 载荷）零覆盖。
// 用 httptest 假 Ollama 端点，手动构造 Mode=ollama 的 LLM（绕过 Resolve 探测）。

func newNativeLLM(t *testing.T, handler http.HandlerFunc) (*LLM, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	l := &LLM{Mode: "ollama", BaseURL: srv.URL, Model: "test-model",
		client: &http.Client{Timeout: 5 * time.Second}}
	return l, srv
}

func TestNativeSuccess(t *testing.T) {
	var gotPayload map[string]any
	l, srv := newNativeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"你好，已处理"},"eval_count":42}`))
	})
	defer srv.Close()

	content, tokens, err := l.native(context.Background(), "sys", "usr", false, 800, nil)
	if err != nil {
		t.Fatal(err)
	}
	if content != "你好，已处理" || tokens != 42 {
		t.Fatalf("解析不符: %q %d", content, tokens)
	}
	// 请求形态契约：stream:false + think:false + options.num_predict 透传
	if gotPayload["stream"] != false || gotPayload["think"] != false {
		t.Fatalf("stream/think 契约不符: %v", gotPayload)
	}
	if gotPayload["model"] != "test-model" {
		t.Fatalf("model 不符: %v", gotPayload["model"])
	}
	opts, _ := gotPayload["options"].(map[string]any)
	if opts == nil || opts["num_predict"] != float64(800) {
		t.Fatalf("num_predict 未透传: %v", gotPayload["options"])
	}
}

func TestNativeForceJSON(t *testing.T) {
	// ChatJSON → native(forceJSON=true)：载荷含 format:"json"（结构化输出契约）
	var gotPayload map[string]any
	l, srv := newNativeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		_, _ = w.Write([]byte(`{"message":{"content":"{\"score\":7}"},"eval_count":9}`))
	})
	defer srv.Close()

	raw, tokens, err := l.ChatJSON(context.Background(), "sys", "usr", 300)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"score":7}` || tokens != 9 {
		t.Fatalf("ChatJSON 解析不符: %s %d", raw, tokens)
	}
	if gotPayload["format"] != "json" {
		t.Fatalf("forceJSON 未置 format: %v", gotPayload["format"])
	}
}

func TestNativeHTTP500(t *testing.T) {
	l, srv := newNativeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model overheated", http.StatusInternalServerError)
	})
	defer srv.Close()
	_, _, err := l.native(context.Background(), "s", "u", false, 100, nil)
	if err == nil || !strings.Contains(err.Error(), "http 500") {
		t.Fatalf("HTTP 500 应报错: %v", err)
	}
}

func TestNativeMalformedBody(t *testing.T) {
	l, srv := newNativeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	})
	defer srv.Close()
	if _, _, err := l.native(context.Background(), "s", "u", false, 100, nil); err == nil {
		t.Fatal("非法响应体应报错")
	}
}

func TestNativeServerDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // 立即关闭：连接拒绝路径
	l := &LLM{Mode: "ollama", BaseURL: url, Model: "m", client: &http.Client{Timeout: 2 * time.Second}}
	_, _, err := l.native(context.Background(), "s", "u", false, 100, nil)
	if err == nil {
		t.Fatal("连接拒绝应报错")
	}
}

func TestHealthyAndChatMock(t *testing.T) {
	// Healthy：ollama=true mock=false；Chat mock 无 Fallback 时回显前缀
	l := New("mock")
	if l.Healthy() {
		t.Fatal("mock 模式不应 Healthy")
	}
	reply, tokens, err := l.Chat(context.Background(), "sys", "问题", 100)
	if err != nil || !strings.HasPrefix(reply, "(mock) 问题") || tokens != 64 {
		t.Fatalf("mock Chat 形态不符: %q %d %v", reply, tokens, err)
	}
}
