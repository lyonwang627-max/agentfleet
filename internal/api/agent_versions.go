package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Agent 版本管理（F2-a）：发布=快照当前配置；回滚=旧快照覆盖配置+前进新版本。
// 版本链只前进，任何变更（包括回滚）都可追溯。

// snapshotConfig 从 agent 提取参与版本管理的配置字段。
func snapshotConfig(a *model.Agent) map[string]any {
	return map[string]any{
		"name": a.Name, "role": a.Role, "system_prompt": a.SystemPrompt, "model": a.Model,
		"tools": a.Tools, "runner": a.Runner, "max_concurrency": a.MaxConcurrency,
	}
}

func (s *Server) publishAgent(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	a, err := s.Store.GetAgent(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in) // note 可选

	// I14：有草稿先落线（draft → live），再对落线后的 live 做版本快照；无草稿=纯版本快照（F2 语义保留）
	if a.DraftConfig != nil {
		if err := s.Store.PublishAgentDraft(r.Context(), id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		a, err = s.Store.GetAgent(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	v := &model.AgentVersion{AgentID: id, Config: snapshotConfig(a), Note: in.Note}
	_, version, err := s.Store.SaveAgentVersion(r.Context(), v)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "agent.update", map[string]any{"agent_id": id, "action": "published", "version": version})
	writeJSON(w, http.StatusCreated, map[string]any{"agent_id": id, "version": version})
}

func (s *Server) listAgentVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.Store.ListAgentVersions(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": versions, "total": len(versions)})
}

func (s *Server) rollbackAgent(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	version, _ := strconv.Atoi(r.PathValue("version"))
	target, err := s.Store.GetAgentVersion(r.Context(), id, version)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	// 快照覆盖当前配置（缺失字段不覆盖）
	patch := store.AgentPatch{}
	if v, ok := target.Config["name"].(string); ok {
		patch.Name = &v
	}
	if v, ok := target.Config["role"].(string); ok {
		patch.Role = &v
	}
	if v, ok := target.Config["system_prompt"].(string); ok {
		patch.SystemPrompt = &v
	}
	if v, ok := target.Config["model"].(string); ok {
		patch.Model = &v
	}
	if v, ok := target.Config["runner"].(string); ok {
		patch.Runner = &v
	}
	if v, ok := target.Config["max_concurrency"].(float64); ok {
		n := int(v)
		patch.MaxConcurrency = &n
	}
	if raw, ok := target.Config["tools"].([]any); ok {
		tools := []string{}
		for _, t := range raw {
			if ts, ok := t.(string); ok {
				tools = append(tools, ts)
			}
		}
		patch.Tools = &tools
	}
	if err := s.Store.UpdateAgent(r.Context(), id, patch); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// I14：回退覆盖 live 后清空草稿——回退即生效且回到干净态（草稿差异不再悬挂）
	if err := s.Store.ClearAgentDraft(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a, err := s.Store.GetAgent(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	note := "rollback from v" + strconv.Itoa(version)
	nv := &model.AgentVersion{AgentID: id, Config: snapshotConfig(a), Note: note}
	_, newVersion, err := s.Store.SaveAgentVersion(r.Context(), nv)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "agent.update", map[string]any{"agent_id": id, "action": "rolled_back",
		"from_version": version, "new_version": newVersion})
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": id, "rolled_back_from": version, "new_version": newVersion})
}
