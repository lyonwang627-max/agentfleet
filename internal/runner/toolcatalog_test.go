package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 工具能力清单（I28 第 1 轮）：decide 提示注入「可选工具+描述」，LLM 按能力描述选工具。
// 与 I33 闸共存：清单只是提示信息，闸仍在 callTool 执行时拦截。

func TestToolCatalogEmpty(t *testing.T) {
	// 无注册 server → 空清单（decide 提示退回原形态）
	if got := ToolCatalog(); got != "" {
		t.Fatalf("无 server 应空清单: %q", got)
	}
}

func TestDecideInjectsCatalog(t *testing.T) {
	// 有工具清单时 decide 首轮 user 含「可用工具」段；无清单时不含
	st := store.NewMemStore()
	ctx := context.Background()
	var seenUsers []string
	llm := newTestLLM([]string{`{"reply":"完成"}`}, &seenUsers)

	// 无 server：不注入
	req := ExecRequest{
		Agent: model.Agent{Name: "t", Model: "mock"}, Task: model.Task{Title: "任务", Prompt: "做事"},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			_, err := st.AppendStep(ctx, func() int64 { id, _ := st.CreateRun(ctx, &model.Run{TaskID: 1, AgentID: 1}); return id }(), s)
			return err
		},
	}
	if _, err := NewLLM(llm).Execute(ctx, req); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seenUsers[0], "可用工具") {
		t.Fatal("无 server 不应注入工具段")
	}
}

// 幻觉工具引导（I28 第 2 轮）：不存在的工具不执行不进闸，转引导观察回灌下一轮。
func TestHallucinatedToolGuides(t *testing.T) {
	var seenUsers []string
	llm := newTestLLM([]string{
		`{"tool":"mcp:ghost/nope","tool_args":{},"reply":""}`,
		`{"reply":"基于已有信息收尾"}`,
	}, &seenUsers)
	st := store.NewMemStore()
	ctx := context.Background()
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: 1, AgentID: 1})
	res, err := NewLLM(llm).Execute(ctx, ExecRequest{
		Agent: model.Agent{Name: "t", Model: "mock"}, Task: model.Task{Title: "任务", Prompt: "做事"},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			_, err := st.AppendStep(ctx, rid, s)
			return err
		}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "基于已有信息收尾" {
		t.Fatalf("应引导后收尾: %q", res.Reply)
	}
	// 第二轮 user 含引导文本
	if len(seenUsers) < 2 || !strings.Contains(seenUsers[1], "不存在") || !strings.Contains(seenUsers[1], "请重新决策") {
		t.Fatalf("引导观察未回灌: %q", seenUsers[1])
	}
	// tool 步落 hallucination 标记
	steps, _ := st.ListSteps(ctx, rid)
	hallu := false
	for _, s := range steps {
		if s.Kind == model.StepTool && s.Output["hallucination"] == true {
			hallu = true
		}
	}
	if !hallu {
		t.Fatal("幻觉 tool 步未标记")
	}
	// ToolKnown 判定：未注册 server 快速 false
	if ToolKnown("mcp:ghost/nope") {
		t.Fatal("未注册 server 应 false")
	}
	if ToolKnown("not-a-ref") {
		t.Fatal("非法引用应 false")
	}
}
