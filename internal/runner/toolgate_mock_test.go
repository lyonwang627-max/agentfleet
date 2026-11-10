package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
)

// I33 第 2 轮：mock 剧本工具路径同闸 + 拒绝留痕（cancel-on-review 的 gate-reject 步在 api 层测）。

func TestMockToolGateBlocks(t *testing.T) {
	// 剧本含高风险工具 → 闸拦截（ NeedsApproval，不真实调用）
	var steps []*model.RunStep
	req := ExecRequest{
		Agent:    model.Agent{Name: "m", Model: "mock", Runner: "mock"},
		Task:     model.Task{Title: "发布任务", Prompt: "MCP:ghost/publish_x msg=周报"},
		Recorder: stepCollector(&steps),
	}
	res, err := NewMock().Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("闸拦截不是失败: %v", err)
	}
	if !res.NeedsApproval || res.RiskLevel != "high" || !strings.Contains(res.Note, "mcp:ghost/publish_x") {
		t.Fatalf("应进审批门且带工具快照: %+v", res)
	}
	gateFound := false
	for _, s := range steps {
		if s.Kind == model.StepGate && s.Name == "tool-approval-gate" {
			gateFound = true
		}
	}
	if !gateFound {
		t.Fatal("缺 tool-approval-gate 步")
	}
}

func TestMockToolGateApprovedPasses(t *testing.T) {
	// 审批记录提及工具名 → 闸放行 → 真实调用（ghost 未注册 → 下游 mcp 错误，与闸无关）
	var steps []*model.RunStep
	prompt := "MCP:ghost/publish_x msg=周报\n\n[审批记录] 人工放行\n审批意见: 批准 publish_x"
	req := ExecRequest{
		Agent:    model.Agent{Name: "m", Model: "mock", Runner: "mock"},
		Task:     model.Task{Title: "发布任务", Prompt: prompt},
		Recorder: stepCollector(&steps),
	}
	_, err := NewMock().Execute(context.Background(), req)
	if err == nil {
		t.Fatal("ghost 未注册应报 mcp 错误")
	}
	if !strings.Contains(err.Error(), "mcp") {
		t.Fatalf("放行后错误应是 mcp 层（非闸）: %v", err)
	}
	for _, s := range steps {
		if s.Kind == model.StepGate && s.Name == "tool-approval-gate" {
			t.Fatal("已放行不应再触发闸")
		}
	}
}

func TestMockToolGateLowRiskUnaffected(t *testing.T) {
	// 低风险剧本工具照常（回归锁）
	var steps []*model.RunStep
	req := ExecRequest{
		Agent:    model.Agent{Name: "m", Model: "mock", Runner: "mock"},
		Task:     model.Task{Title: "回显任务", Prompt: "MCP:ghost/echo msg=hello"},
		Recorder: stepCollector(&steps),
	}
	_, err := NewMock().Execute(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "mcp") {
		t.Fatalf("低风险工具应直接执行（未注册报 mcp 错）: %v", err)
	}
	for _, s := range steps {
		if s.Kind == model.StepGate {
			t.Fatal("低风险不应有 gate 步")
		}
	}
}
