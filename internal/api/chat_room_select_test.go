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

// 群聊候选集收窄（I32）：@all 智能选人，失败回退全员；显式 @ 不经此通道。

// newSelectTestServer 三成员 store + mock 网关：system==roomSelectorSystem 时按
// script 回复选人 JSON，其余调用（成员回帖）返回普通回帖。
func newSelectTestServer(t *testing.T, selectorRaw string) *Server {
	st := store.NewMemStore()
	gw := gateway.New("mock")
	gw.Fallback = func(system, user string) string {
		if system == roomSelectorSystem {
			return selectorRaw
		}
		return "（" + "成员回帖内容" + "）"
	}
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gw, StartedAt: time.Now()}
	ctx := context.Background()
	for _, a := range []model.Agent{
		{Name: "intake", Role: "需求 intake", SystemPrompt: "负责需求澄清", Enabled: true},
		{Name: "fixer", Role: "缺陷修复", SystemPrompt: "负责修 bug", Enabled: true},
		{Name: "ops", Role: "运维值守", SystemPrompt: "负责告警响应", Enabled: true},
	} {
		if _, err := st.CreateAgent(ctx, &model.Agent{Model: "mock", Runner: "mock", Name: a.Name,
			Role: a.Role, SystemPrompt: a.SystemPrompt, Enabled: a.Enabled}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func postRoom(s *Server, body string) (int, string) {
	r := httptest.NewRequest("POST", "/api/v1/chat/room", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.chatRoom(w, r)
	return w.Code, w.Body.String()
}

type roomOut struct {
	ConversationID int64           `json:"conversation_id"`
	Replies        []roomReply     `json:"replies"`
	Woken          int             `json:"woken"`
	Selection      []roomSelection `json:"selection"`
}

func TestRoomAllSmartSelection(t *testing.T) {
	s := newSelectTestServer(t,
		`{"selections":[{"name":"fixer","reason":"消息是缺陷修复类问题"},{"name":"ghost","reason":"不在名单应被过滤"}]}`)
	code, body := postRoom(s, `{"message":"@all 系统报错了谁来看下"}`)
	if code != 200 {
		t.Fatalf("应 200: %d %s", code, body)
	}
	var out roomOut
	_ = json.Unmarshal([]byte(body), &out)
	// 只唤醒选中的 fixer（ghost 被过滤），不全员
	if out.Woken != 1 || len(out.Replies) != 1 || out.Replies[0].AgentName != "fixer" {
		t.Fatalf("应只唤醒 fixer: woken=%d replies=%+v", out.Woken, out.Replies)
	}
	// 选人明细带理由（验收：模型选人输出带理由）
	if len(out.Selection) != 1 || out.Selection[0].Name != "fixer" ||
		out.Selection[0].Reason != "消息是缺陷修复类问题" {
		t.Fatalf("selection 不符: %+v", out.Selection)
	}
}

func TestRoomAllFallbackOnSelectorFailure(t *testing.T) {
	// 选人器输出非法 JSON → 回退全员唤醒（原有行为兜底）
	s := newSelectTestServer(t, `这不是 JSON`)
	code, body := postRoom(s, `{"message":"@all 谁有空"}`)
	if code != 200 {
		t.Fatalf("应 200: %d %s", code, body)
	}
	var out roomOut
	_ = json.Unmarshal([]byte(body), &out)
	if out.Woken != 3 || out.Selection != nil {
		t.Fatalf("失败应回退全员且无 selection: woken=%d sel=%+v", out.Woken, out.Selection)
	}
}

func TestRoomAllFallbackWhenNoValidSelection(t *testing.T) {
	// 选人器只返回名单外的幽灵名 → 无有效选人 → 回退全员
	s := newSelectTestServer(t, `{"selections":[{"name":"ghost","reason":"x"}]}`)
	_, body := postRoom(s, `{"message":"@all 顶一下"}`)
	var out roomOut
	_ = json.Unmarshal([]byte(body), &out)
	if out.Woken != 3 || out.Selection != nil {
		t.Fatalf("全无效名应回退全员: woken=%d sel=%+v", out.Woken, out.Selection)
	}
}

func TestRoomExplicitMentionNotAffected(t *testing.T) {
	// 显式 @ 不走选人器（selector 不被调用），行为与 I31 完全一致
	s := newSelectTestServer(t, `{"selections":[{"name":"ops","reason":"不应被调用"}]}`)
	_, body := postRoom(s, `{"message":"@fixer 你看一下"}`)
	var out roomOut
	_ = json.Unmarshal([]byte(body), &out)
	if out.Woken != 1 || out.Replies[0].AgentName != "fixer" {
		t.Fatalf("显式 @ 行为不回归: %+v", out)
	}
	if out.Selection != nil {
		t.Fatalf("显式 @ 不应产生 selection: %+v", out.Selection)
	}
}
