package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Agent 版本管理（F2-a）：发布=快照当前配置；回滚=旧快照覆盖配置+前进新版本。
// 版本链只前进，任何变更（包括回滚）都可追溯。

// snapshotConfig 从 agent 提取参与版本管理的配置字段。
// rampNote 灰度备注后缀（版本链可见灰度意图）。
func rampNote(pct int) string {
	if pct > 0 && pct <= 100 {
		return fmt.Sprintf("（灰度 %d%%）", pct)
	}
	return ""
}

// applyVersionSnapshot 灰度版本快照覆盖 agent（promote 收敛用；与 scheduler 同逻辑）。
func applyVersionSnapshot(a *model.Agent, cfg map[string]any) {
	if v, ok := cfg["system_prompt"].(string); ok {
		a.SystemPrompt = v
	}
	if v, ok := cfg["model"].(string); ok && v != "" {
		a.Model = v
	}
}

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
		Note        string `json:"note"`
		RampPct     int    `json:"ramp_pct"`     // I38：灰度百分比（0=立即全量；>0=按 task ID 分桶路由）
		PromoteRamp bool   `json:"promote_ramp"` // I38：灰度收敛——ramping 版本全量（不产生新版本）
	}
	_ = json.NewDecoder(r.Body).Decode(&in) // note 可选

	// I38 promote：灰度收敛——ramping 版本全量生效（配置已在 live？否——ramping 版本配置只
	// 在灰度桶生效过；收敛=live 配置更新为 ramping 快照 + 清灰度标记。版本链不膨胀。）
	if in.PromoteRamp {
		a, err := s.Store.GetAgent(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		if a.RampingVersion == 0 || a.RampPct == 0 {
			writeErr(w, http.StatusConflict, errors.New("无进行中的灰度"))
			return
		}
		snap, err := s.Store.GetAgentVersion(r.Context(), id, a.RampingVersion)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		applyVersionSnapshot(a, snap.Config) // scheduler 同款覆盖
		patch := store.AgentPatch{SystemPrompt: &a.SystemPrompt, Model: &a.Model}
		if err := s.Store.UpdateAgent(r.Context(), id, patch); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		rv, pv := 0, 0
		_ = s.Store.UpdateAgent(r.Context(), id, store.AgentPatch{RampingVersion: &rv, RampPct: &pv})
		s.publish(r.Context(), "agent.update", map[string]any{
			"agent_id": id, "action": "ramp_promoted", "version": a.RampingVersion})
		writeJSON(w, http.StatusOK, map[string]any{"agent_id": id, "promoted_version": a.RampingVersion})
		return
	}

	// I14：有草稿先落线（draft → live），再对落线后的 live 做版本快照；无草稿=纯版本快照（F2 语义保留）
	// I38 灰度例外：ramp>0 时草稿**只进版本快照不落线**——current 桶保持旧配置（灰度才有意义），
	// ramping 桶用 vN 快照；promote 时才落线。
	if a.DraftConfig != nil && in.RampPct == 0 {
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
	v := &model.AgentVersion{AgentID: id, Config: snapshotConfig(a),
		Note: in.Note + rampNote(in.RampPct)}
	// I38 灰度：ramp>0 时灰度候选=草稿内容（live 未落线），快照取草稿
	if in.RampPct > 0 && a.DraftConfig != nil {
		v.Config = a.DraftConfig
	}
	_, version, err := s.Store.SaveAgentVersion(r.Context(), v)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// I38：灰度——ramp_pct>0 时新版本仅按分桶灰度（RampingVersion 落库），live 配置不动
	if in.RampPct > 0 && in.RampPct <= 100 {
		rv, pv := version, in.RampPct
		_ = s.Store.UpdateAgent(r.Context(), id, store.AgentPatch{RampingVersion: &rv, RampPct: &pv})
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
