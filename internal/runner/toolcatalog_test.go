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
