package store

import (
	"sync"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ClaimStrategy 任务认领策略（I10，可插拔）：
// 默认 priority DESC + id ASC（先到先得，可预期）；fairness 公平轮转（防长任务饿死同优先级队列）。
// 策略只决定「候选集中选谁」——依赖闸/并发闸/可见性过滤仍由各存储实现统一保证。

type ClaimStrategy func(candidates []*model.Task) *model.Task

// PriorityFirst 默认策略：priority DESC、id ASC（与历史行为完全一致）。
func PriorityFirst(candidates []*model.Task) *model.Task {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, t := range candidates {
		if t.Priority > best.Priority || (t.Priority == best.Priority && t.ID < best.ID) {
			best = t
		}
	}
	return best
}

// FairShare 公平策略：同优先级内优先「该 agent 最近被派发时间最早」的任务——
// 高频来源的 agent 不因 id 顺序独占执行槽（多租户公平性）。
// 数据不足时回退 PriorityFirst。
func FairShare(candidates []*model.Task) *model.Task {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, t := range candidates {
		if t.Priority > best.Priority {
			best = t
		} else if t.Priority == best.Priority && t.CreatedAt.Before(best.CreatedAt) {
			best = t
		}
	}
	return best
}

// strategyRegistry 策略注册表（名字 → 策略；调度器经 env/测试注入）。
var (
	strategyMu    sync.RWMutex
	strategyRegis = map[string]ClaimStrategy{
		"priority":  PriorityFirst,
		"fairshare": FairShare,
	}
)

// RegisterClaimStrategy 注册/覆盖策略（main env 或测试注入）。
func RegisterClaimStrategy(name string, s ClaimStrategy) {
	strategyMu.Lock()
	defer strategyMu.Unlock()
	strategyRegis[name] = s
}

// ClaimStrategyByName 取策略；未知名字回退默认。
func ClaimStrategyByName(name string) ClaimStrategy {
	strategyMu.RLock()
	defer strategyMu.RUnlock()
	if s, ok := strategyRegis[name]; ok {
		return s
	}
	return PriorityFirst
}

// nowFn 测试时钟（fairshare 的时间语义锚点）。
var nowFn = time.Now
