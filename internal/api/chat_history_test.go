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

// 会话历史上下文（I6）：确定性压缩 + 三处接线（sync/stream/room）。

func mkMsgs(n int, content string) []model.Message {
	msgs := make([]model.Message, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "agent"
		}
		msgs = append(msgs, model.Message{ConversationID: 1, Role: role, Content: content})
	}
	return msgs
}

func TestChatHistoryTranscriptNoCompress(t *testing.T) {
	// ≤ 阈值：原样全量，无压缩头
	out, folded := chatHistoryTranscript(mkMsgs(10, "第几轮都无所谓"), chatKeepRecent)
	if folded != 0 || strings.Contains(out, "[较早") {
		t.Fatalf("未超阈值不应压缩: folded=%d", folded)
	}
	if got := strings.Count(out, "[用户]"); got != 5 {
		t.Fatalf("用户行数不符: %d", got)
	}
}

func TestChatHistoryTranscriptCompressedAndBounded(t *testing.T) {
	// 30 条、每条 2000 rune：较早 18 条折叠为 40 rune 单行，最近 12 条全文（300 头）
	huge := strings.Repeat("长", 2000)
	out, folded := chatHistoryTranscript(mkMsgs(30, huge), chatKeepRecent)
	if folded != 30-chatKeepRecent {
		t.Fatalf("折叠数不符: %d", folded)
	}
	if !strings.Contains(out, "[较早 18 条消息已压缩") || !strings.Contains(out, "[最近对话]") {
		t.Fatalf("缺压缩结构: %s", out[:min(len(out), 120)])
	}
	if strings.Count(out, "[最近对话]") != 1 {
		t.Fatal("最近对话段应只出现一次")
	}
	// 长度有界：18×~50 + 12×~310 ≈ 4620 rune（原始 30×2000=60000）
	if r := len([]rune(out)); r > 6000 {
		t.Fatalf("prompt 未有界: %d rune", r)
	}
}

func TestChatHistoryTranscriptEmpty(t *testing.T) {
	out, folded := chatHistoryTranscript(nil, chatKeepRecent)
	if out != "" || folded != 0 {
		t.Fatalf("空输入应空输出: %q %d", out, folded)
	}
}

// 多轮记忆实弹（mock 网关回显 user prompt）：第 1 轮埋暗号，第 2 轮不重复暗号，
// 模型收到的 prompt（即回显正文）里必须含第 1 轮内容——证明历史真的进了上下文。
func TestChatStreamMultiTurnHistory(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	if _, err := st.CreateAgent(ctx, &model.Agent{Name: "tester", Model: "mock", Runner: "mock", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	post := func(body string) string {
		r := httptest.NewRequest("POST", "/api/v1/chat/stream", strings.NewReader(body))
		r.RemoteAddr = "7.7.7.7:1"
		w := httptest.NewRecorder()
		s.chatStream(w, r)
		return w.Body.String()
	}
	resp1 := post(`{"agent_id":1,"message":"记住暗号 alpha77"}`)
	var m1 struct {
		ConversationID int64 `json:"conversation_id"`
	}
	idx := strings.Index(resp1, "@@META@@")
	_ = json.Unmarshal([]byte(resp1[idx+len("@@META@@"):]), &m1)

	resp2 := post(`{"agent_id":1,"conversation_id":` + itoaInt64(m1.ConversationID) + `,"message":"我刚才说的暗号是什么"}`)
	if !strings.Contains(resp2, "alpha77") {
		t.Fatalf("第 2 轮 prompt 未携带第 1 轮历史: %s", resp2[:min(len(resp2), 200)])
	}
	if strings.Contains(resp1, "我刚才说的暗号是什么") {
		t.Fatal("第 1 轮不应看到第 2 轮消息")
	}
}

func TestChatSyncMultiTurnHistory(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	if _, err := st.CreateAgent(ctx, &model.Agent{Name: "tester", Model: "mock", Runner: "mock", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	call := func(body string) map[string]any {
		r := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.chat(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return m
	}
	r1 := call(`{"agent_id":1,"message":"设定坐标 beta42"}`)
	convID := int64(r1["conversation_id"].(float64))
	r2 := call(`{"agent_id":1,"conversation_id":` + itoaInt64(convID) + `,"message":"坐标是多少"}`)
	reply := r2["reply"].(string)
	if !strings.Contains(reply, "beta42") {
		t.Fatalf("同步 chat 历史缺失: %q", reply)
	}
}

// 群聊时间线压缩：30 条消息的 room prompt 应有界且含压缩结构。
func TestBuildRoomPromptCompressed(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	convID, _ := st.CreateConversation(ctx, &model.Conversation{Title: "群聊", Mode: "room"})
	huge := strings.Repeat("压", 2000)
	for i := 0; i < 30; i++ {
		role, name := "user", ""
		if i%2 == 1 {
			role, name = "agent", "成员甲"
		}
		_, _ = st.AppendMessage(ctx, &model.Message{ConversationID: convID, Role: role, AgentName: name, Content: huge})
	}
	agent := &model.Agent{Name: "成员乙", Model: "mock", Runner: "mock", Enabled: true}
	prompt := s.buildRoomPrompt(ctx, convID, agent, "成员乙")
	if !strings.Contains(prompt, "[较早 14 条消息已压缩") || !strings.Contains(prompt, "[最近对话]") {
		t.Fatalf("room prompt 未压缩: %s", prompt[:min(len(prompt), 150)])
	}
	if r := len([]rune(prompt)); r > 8000 {
		t.Fatalf("room prompt 未有界: %d rune", r)
	}
}
