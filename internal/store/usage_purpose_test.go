package store

import (
	"context"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// by_purpose 聚合（F18-a 收尾）：不同 purpose 的用量分桶互不串。
func TestUsageByPurpose(t *testing.T) {
	st := NewMemStore()
	ctx := context.Background()
	_ = st.AppendUsage(ctx, &model.UsageEvent{AgentID: 1, AgentName: "a", Model: "qwen3.5",
		Purpose: "execute", PromptTokens: 100, CompletionTokens: 200, Requests: 1})
	_ = st.AppendUsage(ctx, &model.UsageEvent{AgentID: 1, AgentName: "a", Model: "lfm25:latest",
		Purpose: "judge", PromptTokens: 10, CompletionTokens: 20, Requests: 2})
	_ = st.AppendUsage(ctx, &model.UsageEvent{AgentID: 2, AgentName: "b", Model: "qwen3.5",
		Purpose: "chat", PromptTokens: 30, CompletionTokens: 40, Requests: 1})

	out, err := st.UsageByPurpose(ctx, 24*time.Hour) // 超长窗口覆盖全部
	if err != nil {
		t.Fatal(err)
	}
	byP := map[string]model.UsageEvent{}
	for _, u := range out {
		byP[u.Purpose] = u
	}
	if len(byP) != 3 {
		t.Fatalf("应分 3 桶，得到 %d: %+v", len(byP), byP)
	}
	if byP["execute"].PromptTokens != 100 || byP["judge"].Requests != 2 || byP["chat"].PromptTokens != 30 {
		t.Fatalf("分桶数据串了: %+v", byP)
	}
}
