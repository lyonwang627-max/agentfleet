package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lyonwang/agentfleet/internal/model"
)

// AI 预审审批门（F16，借鉴 LoopForge 独立审查代批）：
// review 状态的任务先过「无工具预审」——模型只看材料给建议（approve/deny + 风险 + 理由），
// 不执行任何操作、无凭证；人仍是终审。预审结果落 trace（kind=gate，瀑布图可见）。

type PrecheckResult struct {
	Recommendation string `json:"recommendation"` // approve | deny | needs_human
	RiskLevel      string `json:"risk_level"`     // low | medium | high
	Reason         string `json:"reason"`
}

func (s *Server) precheckTask(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	t, err := s.Store.GetTask(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if t.Status != model.TaskReview {
		writeErr(w, http.StatusConflict, errors.New("仅 review 状态可预审（先让任务进审批门）"))
		return
	}
	if s.LLM.Mode == "mock" || !s.LLM.Healthy() {
		writeErr(w, http.StatusServiceUnavailable, errors.New("预审需要可用 LLM（当前 mock/不可用）——人工直接 approve 即可"))
		return
	}

	// 无工具预审：材料 = 任务原始诉求 + 当前产出草稿 + 涉及的操作语义
	raw, _, err := s.LLM.ChatJSON(r.Context(),
		"你是独立的审批预审员（无工具、无凭证，不执行任何操作）。评估两点："+
			"1) 该产出若被放行执行，操作风险多大（对外发布/删除/资金/数据变更 vs 只读内部动作）；"+
			"2) 产出是否回应了原始诉求。只输出 JSON："+
			`{"recommendation": "approve|deny|needs_human", "risk_level": "low|medium|high", "reason": "两句话内"}`+
			"（needs_human=材料不足以判断；高风险操作一律 needs_human 或 deny，由人拍板）",
		"# 原始任务\n"+t.Title+"\n"+clip(t.Prompt, 1500)+
			"\n\n# 当前产出草稿\n"+clip(t.Result, 1500), 400)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	var p PrecheckResult
	if err := json.Unmarshal(raw, &p); err != nil ||
		(p.Recommendation != "approve" && p.Recommendation != "deny" && p.Recommendation != "needs_human") {
		writeErr(w, http.StatusBadGateway, errors.New("预审输出异常，建议人工直接审批"))
		return
	}

	// 预审留痕：落产生草稿那次 run 的 trace（kind=gate）
	if runs, _ := s.Store.ListRuns(r.Context(), id); len(runs) > 0 {
		_, _ = s.Store.AppendStep(r.Context(), runs[0].ID, &model.RunStep{
			Kind: model.StepGate, Name: "ai-precheck",
			Output: map[string]any{"recommendation": p.Recommendation, "risk_level": p.RiskLevel},
			Note:   p.Reason})
	}
	s.publish(r.Context(), "task.precheck", map[string]any{
		"task_id": id, "recommendation": p.Recommendation, "risk_level": p.RiskLevel})
	writeJSON(w, http.StatusOK, p)
}

// clip 按字符安全截断（中文按 rune，越界 panic 防护）。
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
