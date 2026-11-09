package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/lyonwang/agentfleet/internal/model"
)

// chat 流式端点（F15-a）：POST /api/v1/chat/stream。
// 协议：响应为 chunked text 流——正文为增量文本片段，末行 "@@META@@{json}" 携带
// conversation_id / tokens / run 信息（前端按此切分）。会话落库与用量计量与同步版一致。

type chatStreamIn struct {
	AgentID        int64  `json:"agent_id"`
	Message        string `json:"message"`
	ConversationID int64  `json:"conversation_id"`
}

func (s *Server) chatStream(w http.ResponseWriter, r *http.Request) {
	var in chatStreamIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	agent, err := s.Store.GetAgent(ctx, in.AgentID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	convID := in.ConversationID
	if convID <= 0 {
		convID, _ = s.Store.CreateConversation(ctx, &model.Conversation{AgentID: agent.ID, Title: head2(in.Message, 30)})
	}
	_, _ = s.Store.AppendMessage(ctx, &model.Message{ConversationID: convID, Role: "user", Content: in.Message})

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Conversation-Id", fmt.Sprintf("%d", convID))
	w.Header().Set("Cache-Control", "no-cache")

	// I6 多轮上下文：历史（含刚入库的当前消息）压成有界时间线——
	// 此前流式端点同样每轮只发当前消息，试聊台多轮记忆缺失
	msgs, _ := s.Store.ListMessages(ctx, convID)
	transcript, _ := chatHistoryTranscript(msgs, chatKeepRecent)
	if transcript == "" {
		transcript = in.Message
	}

	var full []rune
	tokens, streamErr := s.LLM.StreamChat(ctx, agent.SystemPrompt, transcript, 900, func(chunk string) {
		full = append(full, []rune(chunk)...)
		fmt.Fprint(w, chunk)
		fl.Flush()
	})
	reply := string(full)
	if streamErr != nil && reply == "" {
		// 完全没输出：退化为错误响应（此时已发过 200 头，只能以 META 错误标记告知）
		reply = "（生成失败：" + streamErr.Error() + "）"
	}
	_, _ = s.Store.AppendMessage(ctx, &model.Message{ConversationID: convID, Role: "agent", Content: reply})
	_ = s.Store.AppendUsage(ctx, &model.UsageEvent{AgentID: agent.ID, AgentName: agent.Name,
		Model: agent.Model, Purpose: "chat", PromptTokens: len2(transcript), CompletionTokens: tokens, Requests: 1})

	meta, _ := json.Marshal(map[string]any{
		"conversation_id": convID, "tokens": tokens, "error": errStr(streamErr)})
	fmt.Fprintf(w, "\n\n@@META@@%s", meta)
	fl.Flush()
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
