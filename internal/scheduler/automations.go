package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/lyonwang/agentfleet/internal/cronexpr"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// sweepBudgets 预算熔断（F5）：24h 用量达预算的 agent 自动停用并广播告警事件。
// 停用后不再被 sweep 命中（无事件风暴）；人工重新启用即恢复——恢复口径由人决定。
func (s *Scheduler) sweepBudgets(ctx context.Context) {
	agents, err := s.store.ListAgents(ctx, store.AgentFilter{EnabledOnly: true})
	if err != nil {
		return
	}
	since := timeNow().Add(-24 * time.Hour)
	for i := range agents {
		a := &agents[i]
		if a.TokenBudget24h <= 0 {
			continue
		}
		used, err := s.store.AgentTokensSince(ctx, a.ID, since)
		if err != nil || used < int64(a.TokenBudget24h) {
			continue
		}
		disabled := false
		if err := s.store.UpdateAgent(ctx, a.ID, store.AgentPatch{Enabled: &disabled}); err != nil {
			continue
		}
		s.emit(ctx, "agent.budget_exceeded", map[string]any{
			"agent_id": a.ID, "agent": a.Name, "used_24h": used,
			"budget": a.TokenBudget24h, "action": "paused"})
		log.Printf("[budget] agent %q 24h 用量 %d 达预算 %d，自动停用（人工可重新启用）",
			a.Name, used, a.TokenBudget24h)
	}
}

// runAutomations 每轮 tick 调用：把到期的自动化规则落成任务。
// NextRun 由解析器计算并持久化（MarkAutomationRan），多实例下因 ClaimNextTask 语义天然幂等——
// 最坏情况两个实例各建一个任务，靠 tick 间隔远大于执行时长来避免，严格去重留给 F1-b 的分布式锁。
func (s *Scheduler) runAutomations(ctx context.Context) {
	autos, err := s.store.ListAutomations(ctx, true)
	if err != nil {
		return
	}
	now := timeNow()
	for i := range autos {
		a := &autos[i]
		if a.NextRun != nil && a.NextRun.After(now) {
			continue
		}
		expr, err := cronexpr.Parse(a.CronExpr)
		if err != nil {
			log.Printf("[automations] %q 表达式非法，禁用: %v", a.Name, err)
			enabled := false
			_ = s.store.UpdateAutomation(ctx, a.ID, store.AutomationPatch{Enabled: &enabled})
			continue
		}
		next := expr.Next(now)
		if next.IsZero() {
			continue
		}
		// 创建任务（agent 必须存在；禁用 agent 的自动化跳过本轮但推进调度）
		task := &model.Task{Title: a.Title, Prompt: a.Prompt, AgentID: a.AgentID, Priority: a.Priority}
		if _, err := s.store.CreateTask(ctx, task); err != nil {
			log.Printf("[automations] %q 建任务失败: %v", a.Name, err)
			continue
		}
		last := now
		_ = s.store.MarkAutomationRan(ctx, a.ID, last, next)
		s.emit(ctx, "automation.fired", map[string]any{
			"automation_id": a.ID, "name": a.Name, "task_id": task.ID, "next_run": next.Format(time.RFC3339)})
		log.Printf("[automations] %q → task#%d（下次 %s）", a.Name, task.ID, next.Format("01-02 15:04"))
	}
}
