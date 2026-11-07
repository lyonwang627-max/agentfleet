package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Agent 克隆（I3，对标 Multica 的派生变体心智）：从现有 agent 复制配置创建新 agent，
// 用于「同角色不同模型/提示词」的 A/B 调参与评测对比（F18-c 的对象生成器）。
// 克隆体：全新版本链（不继承历史——回溯看源 agent）、默认启用、名称自动去重。

func (s *Server) cloneAgent(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	src, err := s.Store.GetAgent(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Name string `json:"name"` // 可选；缺省自动命名
	}
	_ = json.NewDecoder(r.Body).Decode(&in)

	newName := strings.TrimSpace(in.Name)
	if newName == "" {
		newName = src.Name + "-clone"
	}
	// 名称唯一性：与现存 agent 撞名时自动加序号
	existing, _ := s.Store.ListAgents(r.Context(), store.AgentFilter{})
	taken := map[string]bool{}
	for _, a := range existing {
		taken[a.Name] = true
	}
	base, seq := newName, 1
	for taken[newName] {
		seq++
		newName = fmt.Sprintf("%s-%d", base, seq)
	}

	clone := &model.Agent{
		Name: newName, Role: src.Role, SystemPrompt: src.SystemPrompt,
		Model: src.Model, Tools: src.Tools, Runner: src.Runner,
		MaxConcurrency: src.MaxConcurrency, TokenBudget24h: src.TokenBudget24h,
		Enabled: true,
	}
	newID, err := s.Store.CreateAgent(r.Context(), clone)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// 版本链：克隆体 v1 = 源 agent 当前配置（继承配置语义，不继承历史）
	v := &model.AgentVersion{AgentID: newID, Config: snapshotConfig(clone),
		Note: fmt.Sprintf("clone from agent#%d (%s)", src.ID, src.Name)}
	if _, version, err := s.Store.SaveAgentVersion(r.Context(), v); err == nil {
		_ = version
	}
	s.Hub.Publish(r.Context(), "agent.update", map[string]any{
		"agent_id": newID, "action": "cloned", "from": src.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"id": newID, "name": newName, "cloned_from": src.ID})
}
