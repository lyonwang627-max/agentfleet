package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// RSI 处方闭环（F17，借鉴 LoopForge 外环复盘开处方）：
// 评测报告 → LLM 诊断失败模式 → 生成 system_prompt 改进处方 →（F17-b）一键应用=版本管理发布新版本 → 再评测验证。
// 处方只是建议：应用走版本管理（可回滚），人始终在环上。

func (s *Server) prescribe(w http.ResponseWriter, r *http.Request) {
	evalID := pathID(r)
	e, err := s.Store.GetEvalRun(r.Context(), evalID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if e.Report == nil || len(e.Report.Cases) == 0 {
		writeErr(w, http.StatusConflict, errors.New("该评测无报告（未完成或无 case）"))
		return
	}
	if s.LLM.Mode == "mock" || !s.LLM.Healthy() {
		writeErr(w, http.StatusServiceUnavailable, errors.New("处方生成需要可用 LLM（当前 mock/不可用）"))
		return
	}
	// ADR-0002：审查类调用走 L 档（质量最敏感）
	reviewLLM := s.LLM.WithModel(s.LLM.SelectFor(gateway.PurposeReview))

	// 失败/低分 case 优先喂给诊断（成功 case 只取评语前几条做对照）
	var failures, passes strings.Builder
	for _, c := range e.Report.Cases {
		line := "case#" + strconv.Itoa(int(c.CaseID)) + " score=" + strconv.Itoa(c.Score) + "：" + c.JudgeComment + "\n"
		if !c.Pass {
			failures.WriteString(line)
		} else {
			passes.WriteString(line)
		}
	}
	raw, _, err := reviewLLM.ChatJSON(r.Context(),
		"你是研发流程复盘教练（RSI 外环）。根据评测报告中的失败 case，诊断 agent 的系统提示词缺陷，"+
			"并给出一段可追加到 system_prompt 的改进处方。要求：处方是具体的行为约束/结构指引（不要空话），"+
			"针对失败模式逐条对症。只输出 JSON："+
			`{"diagnosis": "失败模式两句话诊断", "system_prompt_addition": "建议追加的提示词段落（80-200字）", "rationale": "为什么这样改（两句话）"}`,
		"# 评测结果（score_avg="+strconv.Itoa(int(e.Report.ScoreAvg))+" pass_rate="+strconv.Itoa(int(e.Report.PassRate))+"）\n"+
			"## 失败/低分 case\n"+failures.String()+"\n## 通过 case（对照）\n"+passes.String()[:400], 700)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	var p model.Prescription
	if err := json.Unmarshal(raw, &p); err != nil || strings.TrimSpace(p.SystemPromptAddition) == "" {
		writeErr(w, http.StatusBadGateway, errors.New("处方输出异常，请重试"))
		return
	}
	p.BasedOnEval = evalID
	p.FailureCases = countFailures(e.Report.Cases)

	// 处方留痕：写回 eval report（additive 字段，报告与处方同处可查）
	e.Report.Prescription = &p
	_ = s.Store.UpdateEvalRun(r.Context(), e)
	writeJSON(w, http.StatusOK, p)
}

// applyPrescription 一键应用（F17-b）：处方 addendum 追加到 agent system_prompt，
// 并经版本管理发布新版本（链只前进，可回滚）——评测驱动的自进化闭环。
func (s *Server) applyPrescription(w http.ResponseWriter, r *http.Request) {
	agentID := pathID(r)
	var in struct {
		EvalID int64 `json:"eval_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.EvalID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("eval_id 必填"))
		return
	}
	e, err := s.Store.GetEvalRun(r.Context(), in.EvalID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if e.Report == nil || e.Report.Prescription == nil || strings.TrimSpace(e.Report.Prescription.SystemPromptAddition) == "" {
		writeErr(w, http.StatusConflict, errors.New("该评测没有可用处方（先 POST /evals/{id}/prescription 生成）"))
		return
	}
	a, err := s.Store.GetAgent(r.Context(), agentID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	p := e.Report.Prescription
	newPrompt := strings.TrimSpace(a.SystemPrompt) + "\n\n" + p.SystemPromptAddition
	if err := s.Store.UpdateAgent(r.Context(), agentID, storePatchPrompt(newPrompt)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	v := &model.AgentVersion{AgentID: agentID, Config: snapshotConfig(a), Note: "RSI 处方应用（eval#" + strconv.Itoa(int(in.EvalID)) + "）"}
	// 注意：快照要取更新后的配置
	if fresh, err := s.Store.GetAgent(r.Context(), agentID); err == nil {
		v.Config = snapshotConfig(fresh)
	}
	_, version, err := s.Store.SaveAgentVersion(r.Context(), v)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "agent.update", map[string]any{
		"agent_id": agentID, "action": "prescription_applied", "eval_id": in.EvalID, "version": version})
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": agentID, "applied_from_eval": in.EvalID, "version": version,
		"diagnosis": p.Diagnosis})
}

func countFailures(cases []model.EvalCaseResult) int {
	n := 0
	for _, c := range cases {
		if !c.Pass {
			n++
		}
	}
	return n
}

func storePatchPrompt(prompt string) store.AgentPatch {
	return store.AgentPatch{SystemPrompt: &prompt}
}
