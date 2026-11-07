package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
)

// mock 剧本分支直测（回归锁）：此前实弹修过的空格提取 bug 与各分支语义。

func TestExtractAfterSpaceRegression(t *testing.T) {
	// 回归锁：marker 后的空格曾被吞掉导致目标名提取为空 → 默认 reviewer 连环误交接
	if got := extractAfter("完成后交接给 fixer 执行", "交接给"); got != "fixer" {
		t.Fatalf("空格后目标名应正确提取: %q", got)
	}
	if got := extractAfter("handoff:fixer 执行修复", "handoff:"); got != "fixer" {
		t.Fatalf("handoff: 提取失败: %q", got)
	}
	if got := extractAfter("交接给：reviewer", "交接给"); got != "reviewer" {
		t.Fatalf("全角冒号应被清理: %q", got)
	}
}

func collectSteps(t *testing.T) *[]model.RunStep {
	t.Helper()
	steps := &[]model.RunStep{}
	return steps
}

func execMock(t *testing.T, prompt string) (Result, []model.RunStep) {
	t.Helper()
	var steps []model.RunStep
	res, err := NewMock().Execute(context.Background(), ExecRequest{
		Agent: model.Agent{Name: "w", Model: "mock"},
		Task:  model.Task{Title: "t", Prompt: prompt},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			steps = append(steps, *s)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return res, steps
}

func TestMockBranchApprovalRecord(t *testing.T) {
	res, steps := execMock(t, "发布任务\n\n[审批记录] 2026-11-07 人工放行")
	if !strings.Contains(res.Reply, "已获人工审批") {
		t.Fatalf("放行分支语义错误: %q", res.Reply)
	}
	found := false
	for _, s := range steps {
		if s.Name == "approval-passed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 approval-passed step: %+v", steps)
	}
}

func TestMockBranchApprovalGate(t *testing.T) {
	res, _ := execMock(t, "涉及对外发布，需要审批。")
	if !res.NeedsApproval {
		t.Fatal("审批关键词应进审批门")
	}
}

func TestMockBranchHandoffBeforeApproval(t *testing.T) {
	// handoff 判定先于审批：带 handoff 指令的任务先移交（接收方再决定审批）
	res, _ := execMock(t, "定位问题后 handoff:fixer，方案涉及变更需审批。")
	if res.HandoffTo != "fixer" {
		t.Fatalf("handoff 应先于审批分支: %+v", res)
	}
	if res.NeedsApproval {
		t.Fatal("intake 阶段不应进审批门")
	}
}

func TestMockSearchToolStepNotExclusive(t *testing.T) {
	_, steps := execMock(t, "先搜索 runbook，然后普通完成。")
	toolFound := false
	for _, s := range steps {
		if s.Kind == model.StepTool && s.Name == "text_search" {
			toolFound = true
		}
	}
	if !toolFound {
		t.Fatalf("搜索指令应产生 tool 步（与决策解耦）: %+v", steps)
	}
}

func TestMockFailBranch(t *testing.T) {
	_, err := NewMock().Execute(context.Background(), ExecRequest{
		Agent: model.Agent{Name: "w"}, Task: model.Task{Title: "t", Prompt: "这个任务要 失败 两次。"}})
	if err == nil || !strings.Contains(err.Error(), "mock") {
		t.Fatalf("失败分支应返回错误（触发重试）: %v", err)
	}
}
