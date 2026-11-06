package scheduler

import (
	"strings"
	"testing"
)

// 上下文 Spill（F11-b）：短 prompt 不压缩；长 prompt 压缩为摘要+run 引用（链式召回锚点）。
func TestSpillIfNeededShortUnchanged(t *testing.T) {
	short := "orderservice P99 上涨，需要定位。" + strings.Repeat("补充细节。", 20) // < 1500 rune
	got := spillIfNeeded(short, "任务标题", 1, 42, "初步结论：连接池打满")
	if got != short {
		t.Fatal("短 prompt 不应被压缩")
	}
}

func TestSpillIfNeededLongCompressed(t *testing.T) {
	long := strings.Repeat("历史交接细节。", 300) // ≈2100 rune > 1500
	note := strings.Repeat("上一棒结论很长的内容。", 50)
	got := spillIfNeeded(long, "P2 告警：订单服务延迟", 2, 42, note)

	if runeLen(got) >= spillThreshold {
		t.Fatalf("压缩后仍超阈值: %d", runeLen(got))
	}
	for _, want := range []string{"run#42", "原始诉求：P2 告警：订单服务延迟", "历经 2 次交接", "上一结论："} {
		if !strings.Contains(got, want) {
			t.Fatalf("压缩稿缺少锚点 %q: %s", want, got)
		}
	}
	// 链式召回锚点：run#ID 可经 API 取回完整 trace
	if !strings.Contains(got, "已压缩，完整历史见 run#42") {
		t.Fatalf("召回指引缺失: %s", got)
	}
}
