package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ChatRaw 多轮直通（F18 前置）：native /api/chat NDJSON 解析 + 多消息透传 + 错误路径。
func TestChatRawMultiMessage(t *testing.T) {
	var gotModel string
	var gotMsgCount int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		gotMsgCount = len(req.Messages)
		w.Header().Set("Content-Type", "application/json")
		// stream:false 单响应（ChatRaw 语义；NDJSON 流式走 StreamChat）
		w.Write([]byte(`{"message":{"role":"assistant","content":"第一段第二段"},"done":true,"eval_count":42}`))
	}))
	defer fake.Close()

	l := &LLM{Mode: "ollama", BaseURL: fake.URL, Model: "test-model",
		client: fake.Client(), Pricing: map[string]ModelPrice{}}
	out, tokens, err := l.ChatRaw(context.Background(), []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "usr"},
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if out != "第一段第二段" {
		t.Fatalf("增量拼接失败: %q", out)
	}
	if tokens != 42 {
		t.Fatalf("token 数应取 eval_count: %d", tokens)
	}
	if gotModel != "test-model" || gotMsgCount != 2 {
		t.Fatalf("请求透传异常: model=%q msgs=%d", gotModel, gotMsgCount)
	}
}

func TestChatRawHTTPError(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer fake.Close()
	l := &LLM{Mode: "ollama", BaseURL: fake.URL, Model: "m", client: fake.Client()}
	if _, _, err := l.ChatRaw(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, 10); err == nil {
		t.Fatal("500 应返回错误")
	}
}

// mock 模式 ChatRaw：fallback 或默认拼接（离线确定性）。
func TestChatRawMockMode(t *testing.T) {
	l := &LLM{Mode: "mock", Fallback: func(system, user string) string { return "mocked:" + user }}
	out, _, err := l.ChatRaw(context.Background(), []ChatMessage{{Role: "user", Content: "abc"}}, 10)
	if err != nil || !strings.Contains(out, "mocked:abc") {
		t.Fatalf("mock ChatRaw 异常: %q %v", out, err)
	}
}
