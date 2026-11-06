package scheduler

import (
	"context"
	"encoding/json"
	"testing"
)

// 看门狗判定（F14）单测：fail-open 语义——judge 缺失/不健康/调用失败/输出异常，一律放行不误杀。
type fakeJudge struct {
	healthy bool
	raw     string
	err     error
}

func (f *fakeJudge) Healthy() bool { return f.healthy }
func (f *fakeJudge) ChatJSON(_ context.Context, _, _ string, _ int) (json.RawMessage, int, error) {
	return json.RawMessage(f.raw), 0, f.err
}

func TestJudgeProgressFailOpen(t *testing.T) {
	ctx := context.Background()
	// 无 judge → 放行
	if ok, _ := JudgeProgress(ctx, nil, "任务", 0, "steps"); !ok {
		t.Fatal("nil judge 应放行（fail-open）")
	}
	// 不健康 → 放行
	if ok, _ := JudgeProgress(ctx, &fakeJudge{healthy: false}, "任务", 0, "steps"); !ok {
		t.Fatal("不健康 judge 应放行")
	}
}

func TestJudgeProgressVerdict(t *testing.T) {
	ctx := context.Background()
	ok, reason := JudgeProgress(ctx, &fakeJudge{healthy: true,
		raw: `{"progress": false, "reason": "执行记录无新产出"}`}, "任务", 0, "steps")
	if ok || reason != "执行记录无新产出" {
		t.Fatalf("应判定停滞: ok=%v reason=%q", ok, reason)
	}
	ok2, _ := JudgeProgress(ctx, &fakeJudge{healthy: true,
		raw: `不是 JSON`}, "任务", 0, "steps")
	if !ok2 {
		t.Fatal("异常输出应放行（fail-open）")
	}
}
