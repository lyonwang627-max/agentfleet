package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/cronexpr"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Automations：cron 定时建任务的管理端点（F1-b）。
// 创建/修改时服务端校验 cron 并计算 NextRun——客户端不传调度状态。

type automationIn struct {
	AgentID  int64  `json:"agent_id"`
	Name     string `json:"name"`
	CronExpr string `json:"cron_expr"`
	Title    string `json:"title"`
	Prompt   string `json:"prompt"`
	Priority int    `json:"priority"`
}

func (s *Server) createAutomation(w http.ResponseWriter, r *http.Request) {
	var in automationIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Name) == "" || in.AgentID <= 0 || strings.TrimSpace(in.Title) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name / agent_id / title 必填"))
		return
	}
	expr, err := cronexpr.Parse(in.CronExpr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if in.Priority < 1 || in.Priority > 9 {
		in.Priority = 5
	}
	next := expr.Next(time.Now())
	a := &model.Automation{AgentID: in.AgentID, Name: in.Name, CronExpr: in.CronExpr,
		Title: in.Title, Prompt: in.Prompt, Priority: in.Priority, Enabled: true, NextRun: &next}
	id, err := s.Store.CreateAutomation(r.Context(), a)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.Hub.Publish(r.Context(), "automation.update", map[string]any{"automation_id": id, "action": "created"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "next_run": next})
}

func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request) {
	enabledOnly := r.URL.Query().Get("enabled") == "true"
	items, err := s.Store.ListAutomations(r.Context(), enabledOnly)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) getAutomation(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAutomation(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) patchAutomation(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	patch := store.AutomationPatch{}
	if v, ok := in["agent_id"].(float64); ok {
		n := int64(v)
		patch.AgentID = &n
	}
	if v, ok := in["name"].(string); ok {
		patch.Name = &v
	}
	if v, ok := in["title"].(string); ok {
		patch.Title = &v
	}
	if v, ok := in["prompt"].(string); ok {
		patch.Prompt = &v
	}
	if v, ok := in["priority"].(float64); ok {
		n := int(v)
		patch.Priority = &n
	}
	if v, ok := in["enabled"].(bool); ok {
		patch.Enabled = &v
	}
	if v, ok := in["cron_expr"].(string); ok {
		if _, err := cronexpr.Parse(v); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		patch.CronExpr = &v
		// 表达式变更 → 重算下次触发
		expr, _ := cronexpr.Parse(v)
		next := expr.Next(time.Now())
		if err := s.Store.UpdateAutomation(r.Context(), id, patch); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := s.Store.MarkAutomationRan(r.Context(), id, time.Now(), next); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.Hub.Publish(r.Context(), "automation.update", map[string]any{"automation_id": id, "action": "updated"})
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "next_run": next})
		return
	}
	if err := s.Store.UpdateAutomation(r.Context(), id, patch); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.Hub.Publish(r.Context(), "automation.update", map[string]any{"automation_id": id, "action": "updated"})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := s.Store.DeleteAutomation(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.Hub.Publish(r.Context(), "automation.update", map[string]any{"automation_id": id, "action": "deleted"})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (s *Server) setAutomation(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r)
		if err := s.Store.UpdateAutomation(r.Context(), id, store.AutomationPatch{Enabled: &enabled}); err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		s.Hub.Publish(r.Context(), "automation.update", map[string]any{"automation_id": id, "enabled": enabled})
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
	}
}
