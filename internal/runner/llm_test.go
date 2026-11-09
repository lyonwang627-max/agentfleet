package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
)

// LLMRunner.Execute 全链路直测：mock 网关 + Fallback 注入确定性控制每轮 ChatJSON 输出，
// 覆盖 finish / 非 JSON 降级 / 工具循环（失败观察回灌 + 步数耗尽强制收尾）/ 取消传播 /
// 插话注入 / 长回复产物固化 / system prompt 组装。此前 Execute 0% 覆盖（生产主力路径）。

// newTestLLM 构造 mock 网关，fallback 逐轮出队 responses（耗尽后重复最后一个）。
// 每轮收到的 user prompt 记入 seenUsers（断言观察回灌/插话注入）。
func newTestLLM(responses []string, seenUsers *[]string) *gateway.LLM {
	llm := gateway.New("mock")
	idx := 0
	llm.Fallback = func(system, user string) string {
		if seenUsers != nil {
			*seenUsers = append(*seenUsers, user)
		}
		r := responses[idx]
		if idx < len(responses)-1 {
			idx++
		}
		return r
	}
	return llm
}

// stepCollector Recorder 收集器。
func stepCollector(steps *[]*model.RunStep) Recorder {
	return func(_ context.Context, s *model.RunStep) error {
		*steps = append(*steps, s)
		return nil
	}
}

func llmReq(llm *gateway.LLM, steps *[]*model.RunStep) ExecRequest {
	return ExecRequest{
		Agent:    model.Agent{Name: "tester", Model: "mock", SystemPrompt: "你是测试执行者。"},
		Task:     model.Task{Title: "单测任务", Prompt: "把事情做好"},
		Recorder: stepCollector(steps),
	}
}

func TestLLMExecuteFinish(t *testing.T) {
	llm := newTestLLM([]string{`{"reply":"已按要求完成","handoff_to":"","needs_approval":false}`}, nil)
	var steps []*model.RunStep
	res, err := NewLLM(llm).Execute(context.Background(), llmReq(llm, &steps))
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "已按要求完成" || res.HandoffTo != "" || res.NeedsApproval || len(res.Artifacts) != 0 {
		t.Fatalf("结果不符: %+v", res)
	}
	// decide 步留痕：steps=1、observations=0、tokens=64（mock 固定）
	var decide *model.RunStep
	for _, s := range steps {
		if s.Kind == model.StepLLM && s.Name == "decide" {
			decide = s
		}
	}
	if decide == nil {
		t.Fatalf("缺 decide 步: %+v", steps)
	}
	if decide.Input["steps"] != 1 || decide.Input["observations"] != 0 {
		t.Fatalf("decide 步计数不符: %v", decide.Input)
	}
	if decide.Tokens != 64 {
		t.Fatalf("token 计数不符: %d", decide.Tokens)
	}
}

func TestLLMExecuteNonJSONFallback(t *testing.T) {
	// 模型输出非 JSON：原文当 reply 不让任务失败
	llm := newTestLLM([]string{"模型自由发挥的一段纯文本，不是 JSON"}, nil)
	var steps []*model.RunStep
	res, err := NewLLM(llm).Execute(context.Background(), llmReq(llm, &steps))
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "模型自由发挥的一段纯文本，不是 JSON" {
		t.Fatalf("原文未采用: %q", res.Reply)
	}
	if !strings.Contains(res.Note, "非 JSON") {
		t.Fatalf("缺降级说明: %q", res.Note)
	}
}

func TestLLMExecuteToolLoopObservation(t *testing.T) {
	// 第 1 轮请求工具（未注册 server → 失败观察）→ 第 2 轮基于观察给最终 reply
	var seenUsers []string
	llm := newTestLLM([]string{
		`{"tool":"mcp:ghost/nope","tool_args":{"msg":"hi"},"reply":""}`,
		`{"reply":"根据观察完成","handoff_to":""}`,
	}, &seenUsers)
	var steps []*model.RunStep
	res, err := NewLLM(llm).Execute(context.Background(), llmReq(llm, &steps))
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "根据观察完成" {
		t.Fatalf("最终 reply 不符: %q", res.Reply)
	}
	// 观察回灌：第 2 轮 user 含工具名与失败观察
	if len(seenUsers) != 2 {
		t.Fatalf("应 2 轮 decide: %d", len(seenUsers))
	}
	if !strings.Contains(seenUsers[1], "已执行工具 mcp:ghost/nope") ||
		!strings.Contains(seenUsers[1], "工具执行失败") {
		t.Fatalf("第 2 轮未回灌观察: %q", seenUsers[1])
	}
	// decide 步:steps=2、observations=1、token 合计 128
	for _, s := range steps {
		if s.Kind == model.StepLLM && s.Name == "decide" {
			if s.Input["steps"] != 2 || s.Input["observations"] != 1 || s.Tokens != 128 {
				t.Fatalf("decide 步计数不符: %v tokens=%d", s.Input, s.Tokens)
			}
		}
	}
	// tool 步留痕
	toolSteps := 0
	for _, s := range steps {
		if s.Kind == model.StepTool {
			toolSteps++
		}
	}
	if toolSteps != 1 {
		t.Fatalf("tool 步应 1 条: %d", toolSteps)
	}
}

func TestLLMExecuteToolStepsExhausted(t *testing.T) {
	// 每轮都请求工具 → 步数耗尽强制收尾（不失败）
	toolReq := `{"tool":"mcp:ghost/nope","tool_args":{},"reply":""}`
	llm := newTestLLM([]string{toolReq}, nil)
	var steps []*model.RunStep
	res, err := NewLLM(llm).Execute(context.Background(), llmReq(llm, &steps))
	if err != nil {
		t.Fatalf("步数耗尽不应失败: %v", err)
	}
	if res.Reply != "（已达工具调用步数上限，基于已有观察收尾）" {
		t.Fatalf("收尾说明不符: %q", res.Reply)
	}
	for _, s := range steps {
		if s.Kind == model.StepLLM && s.Name == "decide" {
			if s.Input["steps"] != maxToolSteps || s.Input["observations"] != maxToolSteps {
				t.Fatalf("步数计数不符: %v", s.Input)
			}
		}
	}
}

func TestLLMExecuteCancel(t *testing.T) {
	// 取消传播：CancelCheck=true → 首轮 decide 前退出，llm 不被调用
	called := false
	llm := gateway.New("mock")
	llm.Fallback = func(system, user string) string { called = true; return `{"reply":"x"}` }
	var steps []*model.RunStep
	req := llmReq(llm, &steps)
	req.CancelCheck = func() bool { return true }
	_, err := NewLLM(llm).Execute(context.Background(), req)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("应返回 ErrCanceled: %v", err)
	}
	if called {
		t.Fatal("取消后不应调用 LLM")
	}
	// llm-failed 步留痕
	found := false
	for _, s := range steps {
		if s.Kind == model.StepLLM && s.Name == "llm-failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺 llm-failed 步")
	}
}

func TestLLMExecuteSteerInjection(t *testing.T) {
	// 运行中插话：SteerDrain 首轮返回文本并注入当轮 user
	var seenUsers []string
	llm := newTestLLM([]string{`{"reply":"结合插话完成"}`}, &seenUsers)
	var steps []*model.RunStep
	req := llmReq(llm, &steps)
	req.SteerDrain = func() string { return "请优先校验边界条件" }
	res, err := NewLLM(llm).Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "结合插话完成" {
		t.Fatalf("reply 不符: %q", res.Reply)
	}
	if len(seenUsers) != 1 ||
		!strings.Contains(seenUsers[0], "请优先校验边界条件") ||
		!strings.Contains(seenUsers[0], "（请结合以上插话调整后续行动）") {
		t.Fatalf("插话未注入当轮: %q", seenUsers[0])
	}
}

func TestLLMExecuteArtifactOnLongReply(t *testing.T) {
	// >400 rune 且无 handoff → 固化为 report 产物
	long := strings.Repeat("长", 450)
	llm := newTestLLM([]string{`{"reply":"` + long + `","handoff_to":""}`}, nil)
	var steps []*model.RunStep
	res, err := NewLLM(llm).Execute(context.Background(), llmReq(llm, &steps))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("长回复应固化 1 产物: %d", len(res.Artifacts))
	}
	a := res.Artifacts[0]
	if a.Kind != "report" || a.Name != "reply-report.md" || a.Content != long {
		t.Fatalf("产物不符: %+v", a)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	// 有 SystemPrompt：原样保留为开头
	p := buildSystemPrompt(model.Agent{Name: "a", Role: "r", SystemPrompt: "自定义人格。"})
	if !strings.HasPrefix(p, "自定义人格。") || !strings.Contains(p, `"handoff_to"`) {
		t.Fatalf("system prompt 组装不符: %q", p)
	}
	// 无 SystemPrompt：名字/角色兜底
	p2 := buildSystemPrompt(model.Agent{Name: "甲", Role: "工程师"})
	if !strings.Contains(p2, "甲") || !strings.Contains(p2, "工程师") {
		t.Fatalf("缺省人格不符: %q", p2)
	}
}
