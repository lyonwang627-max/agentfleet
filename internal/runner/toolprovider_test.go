package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 工具池统一（I8 第 1 轮）：单一入口按 scheme 分发；内置/MCP 双源；旧引用兼容。

func TestUnifiedBuiltinTool(t *testing.T) {
	u := UnifiedTools()
	out, err := u.Invoke("builtin:text_search", map[string]any{"query": "连接池"})
	if err != nil || !strings.Contains(out, "seed-knowledge") {
		t.Fatalf("内置工具调用失败: %q %v", out, err)
	}
	// 未注册内置工具 → 报错（不静默）
	if _, err := u.Invoke("builtin:ghost_tool", nil); err == nil || !strings.Contains(err.Error(), "未注册") {
		t.Fatalf("未注册内置工具应报错: %v", err)
	}
}

func TestUnifiedMCPCompat(t *testing.T) {
	// 旧引用（无 scheme）兼容：ghost 未注册 → 幻觉引导语义的下游错误
	u := UnifiedTools()
	_, err := u.Invoke("mcp:ghost/echo", map[string]any{"msg": "x"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "mcp") {
		t.Fatalf("旧引用兼容失败: %v", err)
	}
}

func TestUnifiedCatalog(t *testing.T) {
	// 无 MCP server：清单含 builtin 项
	u := UnifiedTools()
	cat := u.Catalog()
	if !strings.Contains(cat, "builtin:text_search") {
		t.Fatalf("清单应含内置项: %q", cat)
	}
}

// 决策协议接 UnifiedTools：llm runner 经 seam 换工具源（F11 决策的 tool 引用直通）。
func TestLLMToolLoopViaUnified(t *testing.T) {
	st := store.NewMemStore()
	ctx := context.Background()
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: 1, AgentID: 1})

	var seen []string
	llm := newTestLLM([]string{
		`{"tool":"builtin:text_search","tool_args":{"query":"缓存"},"reply":""}`,
		`{"reply":"基于检索完成"}`,
	}, &seen)
	res, err := NewLLM(llm).Execute(ctx, ExecRequest{
		Agent: model.Agent{Name: "u", Model: "mock"},
		Task:  model.Task{Title: "统一工具任务", Prompt: "检索缓存要点"},
		Recorder: func(_ context.Context, s *model.RunStep) error {
			_, err := st.AppendStep(ctx, rid, s)
			return err
		}})
	if err != nil {
		t.Fatal(err)
	}
	// toolKnownFn seam：builtin 引用视为已注册（I8 与 I28 幻觉判定的接缝——生产侧 ToolKnown
	// 只认 mcp 引用；此处验证循环本身对 builtin 引用透明）
	_ = res
	if len(seen) < 2 {
		t.Fatalf("应 2 轮决策: %d", len(seen))
	}
}
