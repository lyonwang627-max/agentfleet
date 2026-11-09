package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// @ 解析（I31）：中文/英文名、去重、@all 过滤。
func TestParseMentions(t *testing.T) {
	got := parseMentions("@fixer 看一下这个，@reviewer 也确认下 @fixer")
	if len(got) != 2 || got[0] != "fixer" || got[1] != "reviewer" {
		t.Fatalf("解析异常: %v", got)
	}
}

// 群聊全链路（mock 网关确定性）：@两个 agent → 都回帖入库带 agent_name → 共享同会话。
func TestChatRoomEndToEnd(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	gw := gateway.New("mock")
	s := &Server{Store: st, Hub: hub, LLM: gw, StartedAt: time.Now()}
	ctx := context.Background()
	for _, n := range []string{"fixer", "reviewer"} {
		if _, err := st.CreateAgent(ctx, &model.Agent{Name: n, Model: "mock", Runner: "mock",
			SystemPrompt: "你是" + n, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest("POST", "/api/v1/chat/room",
		strings.NewReader(`{"message":"@fixer 评估这个方案，@reviewer 复核"}`))
	w := httptest.NewRecorder()
	s.chatRoom(w, req)
	if w.Code != 200 {
		t.Fatalf("群聊应 200: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		ConversationID int64       `json:"conversation_id"`
		Replies        []roomReply `json:"replies"`
		Woken          int         `json:"woken"`
	}
	_ = jsonDec(w.Body.String(), &out)
	if out.Woken != 2 || len(out.Replies) != 2 {
		t.Fatalf("应唤醒 2 个: %+v", out)
	}
	names := map[string]bool{}
	for _, r := range out.Replies {
		names[r.AgentName] = true
	}
	if !names["fixer"] || !names["reviewer"] {
		t.Fatalf("两个都该回帖: %+v", out.Replies)
	}

	// 共享时间线：user + 2 agent 回帖 = 3 条，agent 消息带 agent_name
	msgs, _ := st.ListMessages(ctx, out.ConversationID)
	if len(msgs) != 3 {
		t.Fatalf("时间线应 3 条: %d", len(msgs))
	}
	if msgs[1].AgentName != "fixer" || msgs[2].AgentName != "reviewer" {
		t.Fatalf("agent_name 缺失: %+v", msgs)
	}

	// 续聊：同会话再 @ 一个人
	req2 := httptest.NewRequest("POST", "/api/v1/chat/room",
		strings.NewReader(`{"conversation_id":`+itoa64(out.ConversationID)+`,"message":"@reviewer 再看一眼"}`))
	w2 := httptest.NewRecorder()
	s.chatRoom(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("续聊失败: %d", w2.Code)
	}
	msgs2, _ := st.ListMessages(ctx, out.ConversationID)
	// ≥5：user+reviewer 必回帖；链式唤醒（回帖内容含 @ 其他成员）可能追加——这是设计行为
	if len(msgs2) < 5 {
		t.Fatalf("续聊后至少 5 条: %d", len(msgs2))
	}
	if msgs2[1].AgentName == "" { // 第二条必是 reviewer 或链式 agent 的回帖
		t.Fatalf("群聊消息缺 agent_name: %+v", msgs2[1])
	}
}

func jsonDec(s string, v any) error { return json.Unmarshal([]byte(s), v) }
func itoa64(n int64) string         { return strconv.FormatInt(n, 10) }
