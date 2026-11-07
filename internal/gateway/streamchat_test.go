package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// StreamChat（F15 底座）回归锁：NDJSON 增量解析 / done 截断 / eval_count 优先 / HTTP 错误。
func TestStreamChatNDJSONIncremental(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"message":{"content":"你"},"done":false}` + "\n"))
		w.Write([]byte(`{"message":{"content":"好"},"done":false}` + "\n"))
		w.Write([]byte(`{"message":{"content":"。"},"done":true,"eval_count":7}` + "\n"))
	}))
	defer fake.Close()

	l := &LLM{Mode: "ollama", BaseURL: fake.URL, Model: "m", client: fake.Client()}
	var got strings.Builder
	tokens, err := l.StreamChat(context.Background(), "sys", "usr", 100, func(chunk string) {
		got.WriteString(chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "你好。" {
		t.Fatalf("增量拼接失败: %q", got.String())
	}
	if tokens != 7 { // eval_count 优先于行计数
		t.Fatalf("tokens 应取 eval_count=7: %d", tokens)
	}
}

func TestStreamChatDoneWithoutEvalCount(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"message":{"content":"a"},"done":false}` + "\n"))
		w.Write([]byte(`{"message":{"content":"b"},"done":true}` + "\n"))
	}))
	defer fake.Close()

	l := &LLM{Mode: "ollama", BaseURL: fake.URL, Model: "m", client: fake.Client()}
	tokens, err := l.StreamChat(context.Background(), "s", "u", 100, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if tokens != 2 { // 无 eval_count 时回退行计数
		t.Fatalf("tokens 应回退行计数 2: %d", tokens)
	}
}

func TestStreamChatHTTPError(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer fake.Close()
	l := &LLM{Mode: "ollama", BaseURL: fake.URL, Model: "m", client: fake.Client()}
	if _, err := l.StreamChat(context.Background(), "s", "u", 10, func(string) {}); err == nil {
		t.Fatal("500 应返回错误")
	}
}

func TestStreamChatMockMode(t *testing.T) {
	l := &LLM{Mode: "mock"}
	var got strings.Builder
	tokens, err := l.StreamChat(context.Background(), "s", "用户消息", 10, func(c string) { got.WriteString(c) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.String(), "用户消息") {
		t.Fatalf("mock 流式应逐字回放用户消息: %q", got.String())
	}
	if tokens == 0 {
		t.Fatal("mock tokens 应非零")
	}
}
