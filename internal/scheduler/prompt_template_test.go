package scheduler

import (
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
)

// Prompt 模板变量（I21）：渲染/未知变量保留/无模板快路径/nil 安全。
func TestRenderTemplate(t *testing.T) {
	task := &model.Task{ID: 42, Title: "P2 告警处置"}
	ws := &model.Workspace{Name: "hotfix", RootPath: "/tmp/hf", Branch: "hotfix/p99"}

	// 全变量渲染
	got := renderTemplate("处理 {{task.title}}（#{{task.id}}），环境 {{workspace.name}}@{{workspace.branch}}",
		task, ws, "intake")
	for _, want := range []string{"P2 告警处置", "#42", "hotfix@hotfix/p99"} {
		if !strings.Contains(got, want) {
			t.Fatalf("渲染缺 %q: %s", want, got)
		}
	}

	// 未知变量原样保留（写错变量名应被看见）
	got = renderTemplate("保留 {{unknown.var}} 与 {{task.title}}", task, nil, "a")
	if !strings.Contains(got, "{{unknown.var}}") {
		t.Fatal("未知变量被静默清空")
	}
	if strings.Contains(got, "{{task.title}}") {
		t.Fatal("已知变量未渲染")
	}

	// 无模板快路径：原样返回
	plain := "没有任何变量的普通提示词"
	if renderTemplate(plain, task, nil, "a") != plain {
		t.Fatal("快路径应原样返回")
	}

	// nil task/ws 不 panic
	got = renderTemplate("{{task.title}}|{{workspace.path}}", nil, nil, "a")
	if strings.Contains(got, "<nil>") {
		t.Fatalf("nil 引用渲染异常: %q", got)
	}
}

// agent.name 变量。
func TestRenderTemplateAgentName(t *testing.T) {
	got := renderTemplate("由 {{agent.name}} 处理", &model.Task{Title: "t"}, nil, "intake")
	if !strings.Contains(got, "由 intake 处理") {
		t.Fatalf("agent.name 渲染失败: %q", got)
	}
}
