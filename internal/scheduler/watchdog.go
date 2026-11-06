package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 进展看门狗（F14，借鉴 dsh-superagent Watchdog）：
// 进程活着 ≠ 任务在推进。定时检查 running 超过阈值的任务，把 run_steps 摘要喂给模型
// 判断「有无实质进展」；判定停滞则走 failTask（自动重试退避/告警），不误杀有进展的长任务。
//
// 与资源看门狗（sweepBudgets）的分工：预算看的是花了多少，这里看的是产出了没有。

const (
	taskStallThreshold = 3 * time.Minute // running 超过此时长才进入检查
	watchdogInterval   = 2 * time.Minute // 检查周期（由 tick 驱动，内部按时间片流控）
)

// JudgeProgress 判定函数（独立出来便于单测）：
// 输入任务标题、已执行时长、steps 摘要；返回是否有实质进展。
// LLM 不可用（mock）时保守返回 true——看门狗宁漏杀不误杀（fail-open）。
func JudgeProgress(ctx context.Context, llm LLMJudge, title string, running time.Duration, stepsDigest string) (bool, string) {
	if llm == nil || !llm.Healthy() {
		return true, "（LLM 不可用，看门狗放行）"
	}
	raw, _, err := llm.ChatJSON(ctx,
		"你是任务进展审查员。只依据给出的执行记录判断该任务是否有实质进展（产出/推进/新信息），"+
			"不要猜测未给出的内容。只输出 JSON：{\"progress\": true/false, \"reason\": \"一句话依据\"}",
		fmt.Sprintf("# 任务\n%s\n\n# 已运行\n%s\n\n# 执行记录摘要\n%s", title, running.Round(time.Second), stepsDigest), 300)
	if err != nil {
		return true, "判定调用失败，放行: " + err.Error()
	}
	var v struct {
		Progress bool   `json:"progress"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return true, "判定输出解析失败，放行"
	}
	return v.Progress, v.Reason
}

// LLMJudge 看门狗依赖的最小接口（消费侧定义，避免直接耦合 gateway）。
type LLMJudge interface {
	Healthy() bool
	ChatJSON(ctx context.Context, system, user string, maxTokens int) (json.RawMessage, int, error)
}

func (s *Scheduler) sweepProgress(ctx context.Context) {
	running := model.TaskRunning
	tasks, _, err := s.store.ListTasks(ctx, &running, 0, 1, 100)
	if err != nil {
		return
	}
	now := timeNow()
	for i := range tasks {
		t := &tasks[i]
		if t.StartedAt == nil || now.Sub(*t.StartedAt) < taskStallThreshold {
			continue
		}
		// 该任务最近一次 run 的 steps 摘要
		digest := "（无执行记录——可能已卡在认领后未真正执行）"
		if runs, _ := s.store.ListRuns(ctx, t.ID); len(runs) > 0 {
			steps, _ := s.store.ListSteps(ctx, runs[0].ID)
			digest = digestSteps(steps)
		}
		progress, reason := JudgeProgress(ctx, s.judge, t.Title, now.Sub(*t.StartedAt), digest)
		if progress {
			continue
		}
		log.Printf("[watchdog] task#%d 「%s」疑似停滞：%s → 触发重试", t.ID, t.Title, reason)
		s.emit(ctx, "task.stalled", map[string]any{
			"task_id": t.ID, "title": t.Title, "running_s": now.Sub(*t.StartedAt).Seconds(),
			"reason": reason, "action": "retry"})
		s.failTask(ctx, t, "看门狗判定停滞: "+reason)
	}
}

// maybeWatchdog 按 watchdogInterval 流控（tick 是 800ms，看门狗不必每 tick 跑）。
func (s *Scheduler) maybeWatchdog(ctx context.Context) {
	now := timeNow()
	if now.Sub(s.lastWatchdog) < watchdogInterval {
		return
	}
	s.lastWatchdog = now
	s.sweepProgress(ctx)
}

func digestSteps(steps []model.RunStep) string {
	if len(steps) == 0 {
		return "（run 无任何步骤）"
	}
	out := ""
	for _, s := range steps {
		out += fmt.Sprintf("- [%s] %s (%dms)\n", s.Kind, s.Name, s.LatencyMS)
		if len(out) > 1200 {
			break
		}
	}
	return out
}
