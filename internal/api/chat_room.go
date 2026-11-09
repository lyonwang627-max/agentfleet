package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 群聊会话（I31，对标 Multica 群聊内核）：共享上下文 + @ 唤醒。
// - 一个会话一条共享消息流；@某 agent = 唤醒它读全量时间线并回帖
// - agent 之间可互 @（链式，深度限 2、单轮唤醒上限 4，防爆炸）
// - @all = 唤醒全部参与者

const (
	roomMaxWakePerTurn = 4 // 单轮（含链式）最多唤醒 agent 数
	roomMaxDepth       = 2 // agent 回帖再触发别人的深度
)

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_\-一-龥]+)`)

// parseMentions 从文本提取 @名 列表（保持出现顺序去重）。
func parseMentions(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		name := strings.Trim(m[1], "，。！？：；、")
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

type roomIn struct {
	ConversationID int64  `json:"conversation_id"`
	Message        string `json:"message"`
}

type roomReply struct {
	AgentName string `json:"agent_name"`
	Content   string `json:"content"`
}

func (s *Server) chatRoom(w http.ResponseWriter, r *http.Request) {
	var in roomIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Message) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("message 必填"))
		return
	}
	ctx := r.Context()

	// 会话：续用（须 room 模式）或新建
	convID := in.ConversationID
	if convID > 0 {
		conv, err := s.Store.GetConversation(ctx, convID)
		if err != nil || conv.Mode != "room" {
			writeErr(w, http.StatusBadRequest, errors.New("会话不存在或不是群聊会话"))
			return
		}
	} else {
		title := strings.TrimSpace(in.Message)
		if len([]rune(title)) > 30 {
			title = string([]rune(title)[:30])
		}
		convID, _ = s.Store.CreateConversation(ctx, &model.Conversation{
			Title: title, Mode: "room"})
	}

	// 用户消息入库
	if _, err := s.Store.AppendMessage(ctx, &model.Message{
		ConversationID: convID, Role: "user", Content: in.Message}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// 解析 @：显式列表 + 文本内 @；@all 特殊处理
	names := parseMentions(in.Message)
	var all bool
	filtered := names[:0]
	for _, n := range names {
		if n == "all" {
			all = true
			continue
		}
		filtered = append(filtered, n)
	}
	names = filtered
	if all {
		if autos, _ := s.Store.ListAgents(ctx, store.AgentFilter{}); len(autos) >= 0 {
			for _, a := range autos {
				if a.Enabled && a.Name != "all" {
					if !containsStr(names, a.Name) {
						names = append(names, a.Name)
					}
				}
			}
		}
	}

	// 唤醒循环（含 agent 互 @ 链式）
	replies, err := s.wakeRoomAgents(ctx, convID, names, map[string]bool{}, 0)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	// 用量：每 agent 一次
	for _, rep := range replies {
		if a, err := s.Store.GetAgentByName(ctx, rep.AgentName); err == nil {
			_ = s.Store.AppendUsage(ctx, &model.UsageEvent{AgentID: a.ID, AgentName: a.Name,
				Model: a.Model, Purpose: "chat", CompletionTokens: 64, Requests: 1})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": convID, "replies": replies, "woken": len(replies)})
}

// wakeRoomAgents 串行唤醒：读时间线 → 该 agent 人格回帖 → 扫描回帖里的 @ 链式。
func (s *Server) wakeRoomAgents(ctx context.Context, convID int64, names []string,
	woken map[string]bool, depth int) ([]roomReply, error) {
	if depth > roomMaxDepth || len(woken) >= roomMaxWakePerTurn {
		return nil, nil
	}
	var replies []roomReply
	for _, name := range names {
		if woken[name] || len(woken) >= roomMaxWakePerTurn {
			continue
		}
		agent, err := s.Store.GetAgentByName(ctx, name)
		if err != nil || !agent.Enabled {
			continue
		}
		woken[name] = true

		prompt := s.buildRoomPrompt(ctx, convID, agent, name)
		content, _, err := s.LLM.Chat(ctx, agent.SystemPrompt, prompt, 700)
		if err != nil {
			replies = append(replies, roomReply{AgentName: name,
				Content: "（唤醒失败：" + err.Error() + "）"})
			continue
		}
		if _, err := s.Store.AppendMessage(ctx, &model.Message{
			ConversationID: convID, Role: "agent", AgentName: name, Content: content}); err == nil {
			s.publish(ctx, "room.message", map[string]any{
				"conversation_id": convID, "agent": name, "head": headRoom(content, 60)})
		}
		replies = append(replies, roomReply{AgentName: name, Content: content})

		// 链式：回帖里 @ 了别人 → 递归唤醒（下一深度）
		if next := parseMentions(content); len(next) > 0 && depth+1 <= roomMaxDepth {
			more, err := s.wakeRoomAgents(ctx, convID, next, woken, depth+1)
			if err == nil {
				replies = append(replies, more...)
			}
		}
	}
	return replies, nil
}

// buildRoomPrompt：全量时间线 + 被唤醒语境 + 群聊行为约束。
func (s *Server) buildRoomPrompt(ctx context.Context, convID int64, agent *model.Agent, wokenBy string) string {
	msgs, _ := s.Store.ListMessages(ctx, convID)
	var tl strings.Builder
	for _, m := range msgs {
		if m.Role == "user" {
			tl.WriteString("[用户] ")
		} else {
			tl.WriteString("[" + m.AgentName + "] ")
		}
		c := m.Content
		if len([]rune(c)) > 300 {
			c = string([]rune(c)[:300]) + "…"
		}
		tl.WriteString(c + "\n")
	}
	return fmt.Sprintf(`你在一次群聊协作中被 @（%s）。会话时间线：

%s

规则：
- 基于时间线上下文直接回应 @你 的内容；可用 @其他成员名 请他们接力（只在确有必要时）。
- 回答保持简洁（3 句内优先），不要复述别人的发言。
- 你只输出你要说的话本身，不要任何前缀。`, wokenBy, tl.String())
}

func headRoom(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
