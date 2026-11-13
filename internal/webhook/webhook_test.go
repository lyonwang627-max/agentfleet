package webhook

import (
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
)

// IngestToken（I37）：派生确定性 + secret 区分 + 与签名通道独立。
func TestIngestTokenDerivation(t *testing.T) {
	h1 := &model.Webhook{ID: 1, Secret: "sec-a"}
	h2 := &model.Webhook{ID: 1, Secret: "sec-b"}
	t1 := IngestToken(h1)
	if t1 != IngestToken(h1) {
		t.Fatal("派生应确定")
	}
	if t1 == IngestToken(h2) {
		t.Fatal("不同 secret 应不同 token")
	}
	if len(t1) != 24 {
		t.Fatalf("token 应 24 hex: %q", t1)
	}
	// token 不等于签名通道输出（用途隔离）
	if t1 == Sign("sec-a", []byte("x")) {
		t.Fatal("token 不应与签名通道同源同值")
	}
}
