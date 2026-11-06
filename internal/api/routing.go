package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 路由策略评估（F18-c，ADR-0002 四指标的可落地子集）：
//   - 质量不降：同数据集两次评测对比（全 M vs 路由策略），score 差 <1 为合格
//   - 升级率健康区间：5%-15%（<5% 大模型空转，>30% 小模型挡不住事）
//   - 决策 p99 / 一致率：需 per-decide 延迟采集与双判基建，标注后续（诚实边界）

// CompareVerdict A/B 对比结论。
type CompareVerdict struct {
	ScoreDelta      float64 `json:"score_delta"`       // b - a
	PassRateDelta   float64 `json:"pass_rate_delta"`   // b - a（百分点）
	QualityHeld     bool    `json:"quality_held"`      // 质量不降（|delta| < 1）
	Verdict         string  `json:"verdict"`           // pass | regression | improvement
	SameDataset     bool    `json:"same_dataset"`
	DivergentCases  int     `json:"divergent_cases"` // 结论翻转的 case 数
}

func (s *Server) compareEvals(w http.ResponseWriter, r *http.Request) {
	aID, _ := strconv.ParseInt(r.URL.Query().Get("a"), 10, 64)
	bID, _ := strconv.ParseInt(r.URL.Query().Get("b"), 10, 64)
	if aID <= 0 || bID <= 0 || aID == bID {
		writeErr(w, http.StatusBadRequest, errors.New("需要两个不同的评测 id（?a=&b=）"))
		return
	}
	ea, err := s.Store.GetEvalRun(r.Context(), aID)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("评测 a 不存在"))
		return
	}
	eb, err := s.Store.GetEvalRun(r.Context(), bID)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("评测 b 不存在"))
		return
	}
	if ea.Report == nil || eb.Report == nil {
		writeErr(w, http.StatusConflict, errors.New("两次评测都需已完成（有报告）"))
		return
	}

	scoreDelta := round4(eb.Report.ScoreAvg - ea.Report.ScoreAvg)
	passDelta := round4(eb.Report.PassRate - ea.Report.PassRate)
	qualityHeld := scoreDelta > -1.0 // 允许微小波动；跌超 1 分视为质量回退

	verdict := "pass"
	if scoreDelta > 0.5 {
		verdict = "improvement"
	} else if !qualityHeld {
		verdict = "regression"
	}

	// 结论翻转统计（case 级 diff，按 case_id 对齐）
	bCases := map[int64]model.EvalCaseResult{}
	for _, c := range eb.Report.Cases {
		bCases[c.CaseID] = c
	}
	divergent := 0
	for _, ca := range ea.Report.Cases {
		if cb, ok := bCases[ca.CaseID]; ok && ca.Pass != cb.Pass {
			divergent++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"a": map[string]any{"eval_id": aID, "agent_id": ea.AgentID, "dataset_id": ea.DatasetID,
			"score_avg": ea.Report.ScoreAvg, "pass_rate": ea.Report.PassRate},
		"b": map[string]any{"eval_id": bID, "agent_id": eb.AgentID, "dataset_id": eb.DatasetID,
			"score_avg": eb.Report.ScoreAvg, "pass_rate": eb.Report.PassRate},
		"score_delta":    scoreDelta,
		"pass_rate_delta": passDelta,
		"quality_held":   qualityHeld,
		"verdict":        verdict,
		"same_dataset":   ea.DatasetID == eb.DatasetID,
		"divergent_cases": divergent,
	})
}

// RoutingStats 升级率（F18-c）：重试过的任务占比（重试=升级触发的主要信号）。
func (s *Server) routingStats(w http.ResponseWriter, r *http.Request) {
	tasks, total, err := s.Store.ListTasks(r.Context(), nil, 0, 1, 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	escalated := 0
	for _, t := range tasks {
		if t.RetryCount > 0 {
			escalated++
		}
	}
	rate := 0.0
	if total > 0 {
		rate = round4(float64(escalated) / float64(total) * 100)
	}
	health := "healthy"
	switch {
	case rate > 30:
		health = "tier-S-overloaded" // 升级率过高：小模型挡不住事
	case rate < 5 && total >= 20:
		health = "M-underused" // 样本足够时升级率过低：大模型空转
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total_tasks": total, "escalated": escalated, "escalation_rate_pct": rate,
		"healthy_band": "5%-15%", "health": health,
		"note": "升级率=重试任务占比（重试是升级触发主信号）；p99/一致率指标需 per-decide 延迟采集与双判基建，后续落地",
	})
}
