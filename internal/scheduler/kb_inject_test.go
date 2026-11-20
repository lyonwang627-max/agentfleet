package scheduler

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// K2 任务检索增强：绑定 KB 的任务 execute 时检索注入「参考资料」段——确定性注入不跑 LLM。
func TestKBInjection(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "kbinj", Model: "mock", Runner: "mock", Enabled: true})
	kid, _ := st.CreateKB(ctx, &model.KnowledgeBase{Name: "运维手册"})
	st.SaveKBDoc(ctx, &model.KBDoc{KBID: kid, Name: "发布流程", Content: "发布前必须检查 staging 数据库只读副本状态，确认无慢查询堆积。"})

	tid, _ := st.CreateTask(ctx, &model.Task{Title: "发布检查", Prompt: "帮我检查 staging 数据库状态并发布", AgentID: aid, KBIDs: []int64{kid}})
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})
	task, _ := st.GetTask(ctx, tid)

	capR := &captureRunner{}
	runner.Register("mock", capR)
	t.Cleanup(func() { runner.Register("mock", runner.NewMock()) })
	s.execute(ctx, *task)

	if capR.captured == nil {
		t.Fatal("execute 未运行")
	}
	prompt := capR.captured.Task.Prompt
	if !strings.Contains(prompt, "[参考资料（知识库检索）]") {
		t.Fatal("缺知识库注入段")
	}
	for _, want := range []string{"运维手册", "发布流程", "staging 数据库只读副本"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("注入段缺 %q", want)
		}
	}
	// 任务本体 prompt 不被改写（注入只发生在给 runner 的副本）
	orig, _ := st.GetTask(ctx, tid)
	if strings.Contains(orig.Prompt, "[参考资料") {
		t.Fatal("任务本体 prompt 不应被改写")
	}
}

// 预算与 fail-open：检索段 ≤8 条 + ≤3000 字符；KB 已删跳过不阻塞执行。
func TestKBInjectionBudgetAndFailOpen(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "kbbud", Model: "mock", Runner: "mock", Enabled: true})
	kid, _ := st.CreateKB(ctx, &model.KnowledgeBase{Name: "大库"})
	// 12 个命中分段（超出 8 条预算）——每段都含检索词"集群"
	for i := 0; i < 12; i++ {
		st.SaveKBDoc(ctx, &model.KBDoc{KBID: kid, Name: "d" + string(rune('A'+i)), Content: "集群规范第" + string(rune('A'+i)) + "条"})
	}
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "集群巡检", Prompt: "集群", AgentID: aid, KBIDs: []int64{kid, 99999}}) // 99999 不存在
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})
	task, _ := st.GetTask(ctx, tid)

	capR := &captureRunner{}
	runner.Register("mock", capR)
	t.Cleanup(func() { runner.Register("mock", runner.NewMock()) })
	s.execute(ctx, *task)

	if capR.captured == nil {
		t.Fatal("execute 未运行")
	}
	prompt := capR.captured.Task.Prompt
	start := strings.Index(prompt, "[参考资料（知识库检索）]")
	if start < 0 {
		t.Fatal("缺知识库注入段")
	}
	end := len(prompt)
	if i := strings.Index(prompt[start:], "\n\n["); i > 0 {
		end = start + i
	}
	seg := prompt[start:end]
	if n := len([]rune(seg)); n > 3100 {
		t.Fatalf("注入段超预算: %d rune", n)
	}
	if strings.Count(seg, "\n- [") > 8 {
		t.Fatalf("超 8 条预算: %d", strings.Count(seg, "\n- ["))
	}
}
