package gateway

import (
	"encoding/json"
	"os"
	"strings"
)

// 模型定价（F10）：$/1M tokens。本地模型成本为 0（私有化叙事的核心数字）；
// 云端/托管模型按需通过 FLEET_PRICING env 覆写：
//   FLEET_PRICING='{"deepseek-v3.1:671b-cloud":{"prompt":0.27,"completion":1.1}}'
// 未知模型默认 0 成本（宁可不显示钱，不显示编造的价格）。

type ModelPrice struct {
	Prompt     float64 `json:"prompt"`     // $/1M input tokens
	Completion float64 `json:"completion"` // $/1M output tokens
}

// DefaultPricing 内置参考价（仅云端托管模型；本地 Ollama 一律 0）。
// 价格为公开列表价的粗略量级，精确计费以厂商账单为准——面板定位是"成本量级感知"而非财务结算。
var DefaultPricing = map[string]ModelPrice{}

// LoadPricing 从 env 合并用户定价覆写。
func LoadPricing(base map[string]ModelPrice) map[string]ModelPrice {
	out := map[string]ModelPrice{}
	for k, v := range base {
		out[strings.ToLower(k)] = v
	}
	if raw := os.Getenv("FLEET_PRICING"); raw != "" {
		var overrides map[string]ModelPrice
		if err := json.Unmarshal([]byte(raw), &overrides); err == nil {
			for k, v := range overrides {
				out[strings.ToLower(k)] = v
			}
		}
	}
	return out
}

// CostUSD 计算一次调用成本；未知/本地模型返回 0。
func CostUSD(pricing map[string]ModelPrice, modelName string, promptTokens, completionTokens int) float64 {
	p, ok := pricing[strings.ToLower(modelName)]
	if !ok {
		return 0
	}
	return float64(promptTokens)/1e6*p.Prompt + float64(completionTokens)/1e6*p.Completion
}
