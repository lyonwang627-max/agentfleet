package gateway

import (
	"testing"
)

// 步骤级分流（F18-a，ADR-0002）：judge 走 S 档、S 不可用回退 M；WithModel 不污染原客户端。
func TestSelectForTiers(t *testing.T) {
	t.Setenv("FLEET_TIER_S", "lfm25:latest")
	t.Setenv("FLEET_TIER_M", "")
	t.Setenv("FLEET_TIER_L", "")
	l := New("qwen3.5") // 本机 ollama：lfm25 已装（ListModels 可发现）
	if got := l.SelectFor(PurposeJudge); got != "lfm25:latest" {
		t.Fatalf("judge 应走 S 档 lfm25，得到 %q", got)
	}
	if got := l.SelectFor(PurposeExecute); got != "qwen3.5" {
		t.Fatalf("execute 应走默认 M 档，得到 %q", got)
	}
	if got := l.SelectFor(PurposeReview); got != "qwen3.5" {
		t.Fatalf("review 应走默认 L 档，得到 %q", got)
	}
}

func TestSelectForFallbackWhenTierUnavailable(t *testing.T) {
	t.Setenv("FLEET_TIER_S", "nonexistent-model:xyz")
	t.Setenv("FLEET_TIER_M", "")
	l := New("qwen3.5")
	if got := l.SelectFor(PurposeJudge); got != "qwen3.5" {
		t.Fatalf("S 档不可用应回退 M 档，得到 %q", got)
	}
}

func TestWithModelIsolation(t *testing.T) {
	base := New("mock") // mock 模式（确定性）
	derived := base.WithModel("anything")
	if derived.Model != "anything" {
		t.Fatalf("派生视图模型未生效: %q", derived.Model)
	}
	if base.Model != "mock" {
		t.Fatalf("WithModel 污染了原客户端: %q", base.Model)
	}
	if base.Mode != "mock" || derived.Mode != "mock" {
		t.Fatal("mock 模式应保持")
	}
}
