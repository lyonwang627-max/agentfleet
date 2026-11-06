// Package eval 评测引擎：对指定 agent 跑评测集，LLM-as-judge 按要点打分。
// judge 用 mock/llm 与被评 agent 同一网关——模型不可用时 judge 也走 mock（确定性分数），评测链路永远可演示。
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

type Engine struct {
	Store store.Store
	LLM   *gateway.LLM
}

// Start 异步执行评测（API 立即返回 eval_run.id，前端轮询/SSE 看进度）。
func (e *Engine) Start(ctx context.Context, evalRunID, datasetID, agentID int64) {
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		agent, err := e.Store.GetAgent(bg, agentID)
		if err != nil {
			e.finish(bg, evalRunID, "failed", nil)
			return
		}
		cases, err := e.Store.ListCases(bg, datasetID)
		if err != nil || len(cases) == 0 {
			e.finish(bg, evalRunID, "failed", nil)
			return
		}

		results := make([]model.EvalCaseResult, 0, len(cases))
		var scoreSum int
		var passCount int
		for _, c := range cases {
			score, pass, comment := e.scoreCase(bg, agent, c)
			results = append(results, model.EvalCaseResult{CaseID: c.ID, Score: score, Pass: pass, JudgeComment: comment})
			scoreSum += score
			if pass {
				passCount++
			}
		}
		report := &model.EvalReport{Cases: results,
			ScoreAvg: round1(float64(scoreSum) / float64(len(cases))),
			PassRate: round1(float64(passCount) / float64(len(cases)) * 100)}
		log.Printf("[eval] run#%d 完成：avg=%.1f pass=%.0f%%", evalRunID, report.ScoreAvg, report.PassRate)
		e.finishWith(bg, evalRunID, report)
	}()
}

func (e *Engine) scoreCase(ctx context.Context, agent *model.Agent, c model.EvalCase) (int, bool, string) {
	// 1) 被评 agent 作答（直接走网关，不进调度器——评测是旁路流量）
	// max_tokens=1600：评测作答要求完整性，截断会误伤 evidence/structure 评分（R2-b 修复）
	start := time.Now()
	answer, _, err := e.LLM.Chat(ctx, agent.SystemPrompt, c.Input, 1600)
	_ = time.Since(start)
	if err != nil {
		return 0, false, "agent 作答失败: " + err.Error()
	}

	// 2) judge 打分（要点命中 + LLM 评语；mock 模式下为确定性要点匹配）
	points := expectedPoints(c.Expected)
	threshold := expectedThreshold(c.Expected, 6)
	hit := 0
	for _, p := range points {
		if strings.Contains(answer, p) || fuzzyHit(answer, p) {
			hit++
		}
	}
	score := 3
	if len(points) > 0 {
		score = 3 + hit*7/len(points)
		if score > 10 {
			score = 10
		}
	}
	comment := fmt.Sprintf("要点命中 %d/%d", hit, len(points))
	if e.LLM.Healthy() {
		if judged, _, jerr := e.LLM.ChatJSON(ctx,
			"你是严格的技术评审（judge）。对照评分要点给回答打 0-10 分。只输出 JSON：{\"score\": 0-10, \"comment\": \"两句话内\"}",
			fmt.Sprintf("# 评分要点\n%s\n\n# 题目\n%s\n\n# 回答\n%s", strings.Join(points, "\n- "), c.Input, answer[:min(len(answer), 2000)]), 300); jerr == nil {
			var j struct {
				Score   int    `json:"score"`
				Comment string `json:"comment"`
			}
			if json.Unmarshal(judged, &j) == nil && j.Score >= 0 && j.Score <= 10 {
				score = j.Score
				comment = j.Comment
			}
		}
	}
	return score, score >= threshold, comment
}

func (e *Engine) finish(ctx context.Context, id int64, status string, report *model.EvalReport) {
	e.Store.UpdateEvalRun(ctx, &model.EvalRun{ID: id, Status: status, Report: report})
}

func (e *Engine) finishWith(ctx context.Context, id int64, report *model.EvalReport) {
	_ = e.Store.UpdateEvalRun(ctx, &model.EvalRun{ID: id, Status: "done",
		ScoreAvg: report.ScoreAvg, PassRate: report.PassRate, Report: report})
}

// ---- expected 解析工具 ----

func expectedPoints(expected map[string]any) []string {
	raw, ok := expected["points"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func expectedThreshold(expected map[string]any, def int) int {
	if v, ok := expected["threshold"].(float64); ok && v >= 0 && v <= 10 {
		return int(v)
	}
	return def
}

// fuzzyHit 命中判断：要点关键词（去标点后 ≥2 字的词）多数出现在回答中即算命中该要点。
func fuzzyHit(answer, point string) bool {
	keywords := strings.FieldsFunc(point, func(r rune) bool {
		return strings.ContainsRune("，。；：、（）() ", r)
	})
	if len(keywords) == 0 {
		return strings.Contains(answer, point)
	}
	hits := 0
	for _, k := range keywords {
		if len([]rune(k)) >= 2 && strings.Contains(answer, k) {
			hits++
		}
	}
	return hits*2 >= len(keywords)
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
