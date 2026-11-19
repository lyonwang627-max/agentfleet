package runner

import (
	"context"
	"strings"
	"testing"
)

// builtin:kb_search（K1）：mock 回调注入——检索命中/空结果/未注入三路径。
func TestKBSearchBuiltin(t *testing.T) {
	SetKBSearch(func(ctx context.Context, kbID int, query string, topK int) ([]KBHit, error) {
		if query == "连接池" {
			return []KBHit{{DocName: "runbook-db", Seq: 2, Content: "db 连接池打满先查 slow query"}}, nil
		}
		return nil, nil
	})
	t.Cleanup(func() { SetKBSearch(nil) })

	u := UnifiedTools()
	out, err := u.Invoke("builtin:kb_search", map[string]any{"query": "连接池"})
	if err != nil || !strings.Contains(out, "slow query") || !strings.Contains(out, "runbook-db") {
		t.Fatalf("kb_search 命中不符: %q %v", out, err)
	}
	// 空命中 → 友好文案非错误
	out2, err := u.Invoke("builtin:kb_search", map[string]any{"query": "无关词"})
	if err != nil || !strings.Contains(out2, "未找到") {
		t.Fatalf("空命中应友好文案: %q %v", out2, err)
	}
}

func TestKBSearchNotWired(t *testing.T) {
	// store 未注入（kbSearchFn nil）→ 友好提示而非 panic
	out, err := UnifiedTools().Invoke("builtin:kb_search", map[string]any{"query": "x"})
	if err != nil || !strings.Contains(out, "未就绪") {
		t.Fatalf("未注入应友好提示: %q %v", out, err)
	}
}
