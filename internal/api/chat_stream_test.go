package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 流式 chat（F15-a）：chunked 输出 + 末行 META + 会话/用量落库（mock 网关确定性）。
func TestChatStreamChunksAndMeta(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	if _, err := st.CreateAgent(ctx, &model.Agent{Name: "tester", Model: "mock", Runner: "mock", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	body := `{"agent_id":1,"message":"流式测试消息"}`
	r := httptest.NewRequest("POST", "/api/v1/chat/stream", strings.NewReader(body))
	r.RemoteAddr = "7.7.7.7:1"
	w := httptest.NewRecorder()
	s.chatStream(w, r)

	resp := w.Body.String()
	// I6 起 user prompt 是含历史的有界时间线，mock 回显正文=时间线全文；
	// 断言当前消息确实进入了模型上下文并回显
	if !strings.Contains(resp, "流式测试消息") {
		t.Fatalf("正文缺当前消息回显: %s", resp[:min(len(resp), 200)])
	}
	idx := strings.Index(resp, "@@META@@")
	if idx < 0 {
		t.Fatal("缺 META 行")
	}
	var meta struct {
		ConversationID int64  `json:"conversation_id"`
		Tokens         int    `json:"tokens"`
		Error          string `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp[idx+len("@@META@@"):]), &meta); err != nil {
		t.Fatalf("META 解析失败: %v", err)
	}
	if meta.ConversationID == 0 || meta.Error != "" {
		t.Fatalf("META 异常: %+v", meta)
	}
	// 会话已落库（user + agent 两条）
	msgs, _ := st.ListMessages(context.Background(), meta.ConversationID)
	if len(msgs) != 2 {
		t.Fatalf("会话消息应 2 条，得到 %d", len(msgs))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
