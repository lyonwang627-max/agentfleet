package gateway

import (
	"os"
	"testing"
)

func TestCostUSDKnownAndUnknown(t *testing.T) {
	p := map[string]ModelPrice{"cloud-x": {Prompt: 0.5, Completion: 1.5}}
	// 已知模型：1M prompt + 2M completion
	if got := CostUSD(p, "cloud-x", 1_000_000, 2_000_000); got != 3.5 {
		t.Fatalf("cost = %v, want 3.5", got)
	}
	// 未知模型（本地）= 0
	if got := CostUSD(p, "qwen3.5", 1_000_000, 1_000_000); got != 0 {
		t.Fatalf("本地模型成本应为 0，得到 %v", got)
	}
	// 大小写不敏感
	if got := CostUSD(p, "CLOUD-X", 500_000, 0); got != 0.25 {
		t.Fatalf("cost = %v, want 0.25", got)
	}
}

func TestLoadPricingEnvOverride(t *testing.T) {
	t.Setenv("FLEET_PRICING", `{"deepseek-v3.1:671b-cloud":{"prompt":0.27,"completion":1.1}}`)
	p := LoadPricing(map[string]ModelPrice{})
	got := CostUSD(p, "DEEPSEEK-V3.1:671B-CLOUD", 1_000_000, 1_000_000)
	if got != 1.37 {
		t.Fatalf("env 覆写未生效: %v", got)
	}
	if _, ok := p["qwen3.5"]; ok {
		t.Fatal("本地模型不应有默认价")
	}
	_ = os.Unsetenv("FLEET_PRICING")
}
