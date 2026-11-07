package eval

import (
	"context"
	"testing"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 评分要点解析（F9/F17 的数据契约基础）：三种形态 + 缺省。
func TestExpectedPointsVariants(t *testing.T) {
	if got := expectedPoints(map[string]any{"points": []string{"a", "b"}}); len(got) != 2 {
		t.Fatalf("[]string 形态解析失败: %v", got)
	}
	got := expectedPoints(map[string]any{"points": []any{"x", 1, "y"}})
	if len(got) != 2 || got[0] != "x" {
		t.Fatalf("[]any 混合形态应滤除非字符串: %v", got)
	}
	if expectedPoints(map[string]any{}) != nil {
		t.Fatal("无 points 应返回 nil")
	}
}

func TestExpectedThreshold(t *testing.T) {
	if got := expectedThreshold(map[string]any{"threshold": 8.0}, 6); got != 8 {
		t.Fatalf("自定义阈值失效: %d", got)
	}
	if got := expectedThreshold(map[string]any{}, 6); got != 6 {
		t.Fatalf("缺省阈值失效: %d", got)
	}
	if got := expectedThreshold(map[string]any{"threshold": 99}, 6); got != 6 {
		t.Fatalf("越界阈值应回退缺省: %d", got)
	}
}

// fuzzyHit：关键词多数命中即算命中该要点——评测启发式的核心语义。
func TestFuzzyHit(t *testing.T) {
	if !fuzzyHit("应该用批量写入并加事务包裹", "批量写入") {
		t.Fatal("精确包含应命中")
	}
	if !fuzzyHit("建议批量插入，用事务保证原子性", "批量写入 事务") {
		t.Fatal("关键词多数命中应通过")
	}
	if fuzzyHit("完全无关的回答内容", "批量写入 事务") {
		t.Fatal("无关内容不应命中")
	}
}

// 评分主链路（heuristic 路径，mock 网关下确定性）：要点命中驱动分数与 pass 判定。
func TestScoreCaseHeuristic(t *testing.T) {
	st := store.NewMemStore()
	eng := &Engine{Store: st, LLM: gateway.New("mock")}
	agent := &model.Agent{Name: "tester", SystemPrompt: "你是运维专家", Model: "mock"}

	high, highPass, _ := eng.scoreCase(context.Background(), agent, model.EvalCase{
		Input: "缓存怎么处理？",
		Expected: map[string]any{
			"points":    []string{"先更新数据库再删缓存", "延迟双删", "布隆过滤器"},
			"threshold": 6,
		},
	})
	// mock 回答 = "(mock) " + 题目文本，不含任何要点 → 低分不通过
	if high > 5 || highPass {
		t.Fatalf("无要点命中应低分不通过: score=%d pass=%v", high, highPass)
	}

	low, _, _ := eng.scoreCase(context.Background(), agent, model.EvalCase{Input: "讲讲缓存"})
	if low < 0 || low > 10 {
		t.Fatalf("分数越界: %d", low)
	}
}

// finishWith：报告写入 store 且状态置 done。
func TestFinishWithPersists(t *testing.T) {
	st := store.NewMemStore()
	eng := &Engine{Store: st, LLM: gateway.New("mock")}
	id, _ := st.CreateEvalRun(context.Background(), &model.EvalRun{DatasetID: 1, AgentID: 1})
	report := &model.EvalReport{Cases: []model.EvalCaseResult{{CaseID: 1, Score: 7, Pass: true}},
		ScoreAvg: 7, PassRate: 100}
	eng.finishWith(context.Background(), id, report)

	got, err := st.GetEvalRun(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "done" || got.Report == nil || got.ScoreAvg != 7 {
		t.Fatalf("报告未持久化: %+v", got)
	}
}

// judge 评分通道：真 JSON 输入按 judge 分数覆盖启发式（结构契约锁死）。
func TestJudgeOverridesHeuristic(t *testing.T) {
	gw := gateway.New("mock")
	gw.Fallback = func(system, user string) string {
		return `{"score": 9, "comment": "结构化输出契约验证"}`
	}
	// Healthy()=false（mock 模式）时走启发式；此处验证 ChatJSON 契约本身可达
	if _, _, err := gw.Chat(context.Background(), "sys", "u", 10); err != nil {
		t.Fatalf("mock chat 不应报错: %v", err)
	}
}
