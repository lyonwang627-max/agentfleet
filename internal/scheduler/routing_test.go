package scheduler

import (
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/store"
)

func TestScoreTier(t *testing.T) {
	// 高优+审批 → L
	if got := ScoreTier("紧急发布", "涉及审批的发布任务", 9, 1.0, 0); got != TierL {
		t.Fatalf("高优审批任务应 L: %v", got)
	}
	// 低优短任务 → S
	if got := ScoreTier("小任务", "直接完成", 2, 1.0, 0); got != TierS {
		t.Fatalf("低优短任务应 S: %v", got)
	}
	// 标准任务 → M
	if got := ScoreTier("常规处理", strings.Repeat("说明。", 100), 5, 1.0, 0); got != TierM {
		t.Fatalf("常规任务应 M: %v", got)
	}
	// 近期表现差 → 直接 L（质量问题先升级）
	if got := ScoreTier("普通", "普通任务", 5, 0.3, 5); got != TierL {
		t.Fatalf("近期表现差应 L: %v", got)
	}
}

func TestTierToModel(t *testing.T) {
	// llm 未注入时回退 fallback（MemStore 测试场景）
	s := New(store.NewMemStore(), nil)
	if got := s.TierToModel(TierL, "agent-model"); got != "agent-model" {
		t.Fatalf("无网关应回退 agent 模型: %v", got)
	}
}
