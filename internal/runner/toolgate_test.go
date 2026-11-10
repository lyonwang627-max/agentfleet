package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
)

// 工具级审批闸（I33 第 1 轮）：高风险工具执行前暂停进 review 门；
// 审批记录（F3 注入 task prompt）提及工具名才放行。

func gateReq(llm *gateway.LLM, prompt string, steps *[]*model.RunStep) ExecRequest {
	return ExecRequest{
		Agent:    model.Agent{Name: "tester", Model: "mock", SystemPrompt: "你是测试执行者。"},
		Task:     model.Task{Title: "工具闸任务", Prompt: prompt},
		Recorder: stepCollector(steps),
	}
}

func TestToolGateHighRiskBlocks(t *testing.T) {
	// 第 1 轮 decide 请求高风险工具 → 闸拦截 → NeedsApproval（不是失败）
	// 注入 seam：模拟 ghost server 已注册且含 publish_report（闸在幻觉判定之后，需工具真实存在）
	orig := toolKnownFn
	toolKnownFn = func(ref string) bool { return ref == "mcp:ghost/publish_report" }
	defer func() { toolKnownFn = orig }()
	llm := newTestLLM([]string{`{"tool":"mcp:ghost/publish_report","tool_args":{"title":"周报"},"reply":""}`}, nil)
	var steps []*model.RunStep
	res, err := NewLLM(llm).Execute(context.Background(), gateReq(llm, "对外发布周报", &steps))
	if err != nil {
		t.Fatalf("闸拦截不是失败: %v", err)
	}
	if !res.NeedsApproval || res.RiskLevel != "high" {
		t.Fatalf("应进审批门: %+v", res)
	}
	if !strings.Contains(res.Note, "mcp:ghost/publish_report") {
		t.Fatalf("Note 应带工具快照: %q", res.Note)
	}
	// gate 步留痕（工具+参数快照）
	found := false
	for _, s := range steps {
		if s.Kind == model.StepGate && s.Name == "tool-approval-gate" {
			found = true
			if s.Input["tool"] != "mcp:ghost/publish_report" {
				t.Fatalf("gate 步缺工具快照: %v", s.Input)
			}
		}
	}
	if !found {
		t.Fatal("缺 tool-approval-gate 步")
	}
}

func TestToolGateLowRiskPasses(t *testing.T) {
	// 低风险工具不走闸（回归锁：正常工具循环不受影响）
	var seenUsers []string
	llm := newTestLLM([]string{
		`{"tool":"mcp:ghost/echo","tool_args":{"msg":"hi"},"reply":""}`,
		`{"reply":"完成","handoff_to":""}`,
	}, &seenUsers)
	var steps []*model.RunStep
	res, err := NewLLM(llm).Execute(context.Background(), gateReq(llm, "查一下状态", &steps))
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsApproval {
		t.Fatalf("低风险工具不应进闸: %+v", res)
	}
	if res.Reply != "完成" {
		t.Fatalf("循环应正常收尾: %q", res.Reply)
	}
}

func TestToolGateApprovedSecondPass(t *testing.T) {
	// F3 审批记录注入后重跑：意见提及工具名 → 闸放行 → 工具真实执行
	orig := toolKnownFn
	toolKnownFn = func(ref string) bool { return ref == "mcp:ghost/publish_report" }
	defer func() { toolKnownFn = orig }()
	var seenApprovalUsers []string
	llm := newTestLLM([]string{`{"tool":"mcp:ghost/publish_report","tool_args":{"title":"合规标题"},"reply":""}`}, &seenApprovalUsers)
	var steps []*model.RunStep
	prompt := "对外发布周报\n\n[审批记录] 2026-11-10 人工放行\n审批意见: 批准调用 publish_report，参数改为合规标题"
	res, err := NewLLM(llm).Execute(context.Background(), gateReq(llm, prompt, &steps))
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsApproval {
		t.Fatalf("已批准不应再进闸: %+v", res)
	}
	// 工具真实执行（ghost 未注册 → 失败观察回灌，decide 第二轮收尾）
	if len(seenApprovalUsers) < 2 {
		t.Fatal("放行后应继续工具循环（观察回灌下一轮）")
	}
	toolEmitted := false
	for _, s := range steps {
		if s.Kind == model.StepTool {
			toolEmitted = true
		}
	}
	if !toolEmitted {
		t.Fatal("放行后应落 tool 步")
	}
}

func TestHighRiskToolTable(t *testing.T) {
	cases := map[string]bool{
		"mcp:demo/publish_report": true,
		"mcp:demo/delete_user":    true,
		"mcp:demo/PAYMENT_refund": true,
		"mcp:demo/echo":           false,
		"mcp:demo/search":         false,
		"mcp:demo/read_file":      false,
	}
	for ref, want := range cases {
		if got := highRiskTool(ref); got != want {
			t.Fatalf("highRiskTool(%q)=%v want %v", ref, got, want)
		}
	}
	if !toolApproved("xx [审批记录] 放行 publish_report", "mcp:demo/publish_report") {
		t.Fatal("审批记录提及工具名应放行")
	}
	if toolApproved("xx [审批记录] 放行", "mcp:demo/publish_report") {
		t.Fatal("审批记录未提及工具名不应放行（防一次审批放行全部工具）")
	}
	if toolApproved("普通 prompt", "mcp:demo/publish_report") {
		t.Fatal("无审批记录不放行")
	}
}
