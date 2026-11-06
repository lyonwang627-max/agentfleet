package api

import (
	"net/http"
)

// 会话回放数据层（F6-a）：列表（带消息数/关联 run 数）+ 详情（全量消息 + 关联 runs）。

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListConversations(r.Context(), 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	conv, err := s.Store.GetConversation(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	msgs, err := s.Store.ListMessages(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	agentName := ""
	if a, err := s.Store.GetAgent(r.Context(), conv.AgentID); err == nil {
		agentName = a.Name
	}
	runs := []map[string]any{}
	for _, run := range s.recentRuns(r.Context(), conv.AgentID, 50) {
		runs = append(runs, run)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": conv.ID, "agent_id": conv.AgentID, "agent_name": agentName, "title": conv.Title,
		"created_at": conv.CreatedAt, "messages": msgs, "runs": runs,
	})
}
