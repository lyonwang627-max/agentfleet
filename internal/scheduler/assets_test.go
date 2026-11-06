package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 执行环境维度三模块（workspace 绑定 / skill 注入 / artifact 产出）端到端：
// 任务绑定 workspace + 引用 skill → 调度执行 → 注入发生在 runner 侧（任务本体 prompt 不变）→ 产物落库。
func TestWorkspaceSkillArtifactFlow(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	runner.Register("mock", runner.NewMock())
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	wsID, err := st.CreateWorkspace(ctx, &model.Workspace{Name: "wenv", RootPath: "/tmp/wenv", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	skillID, err := st.SaveSkill(ctx, &model.Skill{Name: "SOP", Content: "先止血再定位", Tags: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}
	aid := mustAgent(t, st, "worker", "mock")
	taskID, err := st.CreateTask(ctx, &model.Task{
		Title: "生成数据周报", Prompt: "汇总本周数据的报告任务。", AgentID: aid, Priority: 6,
		WorkspaceID: wsID, SkillIDs: []int64{skillID}})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tsk, _ := st.GetTask(ctx, taskID)
		if tsk != nil && tsk.Status == model.TaskDone {
			break
		}
		time.Sleep(120 * time.Millisecond)
	}
	tsk, _ := st.GetTask(ctx, taskID)
	if tsk == nil || tsk.Status != model.TaskDone {
		t.Fatalf("任务未完成: %+v", tsk)
	}

	// 1) 任务本体 prompt 不被 skill/workspace 污染（指令通道纯净）
	if strings.Contains(tsk.Prompt, "[经验参考") || strings.Contains(tsk.Prompt, "[执行环境]") {
		t.Fatalf("注入内容污染了任务本体 prompt: %q", tsk.Prompt)
	}
	// 2) 产物已落库：周报 report + workspace 执行 log
	arts, _ := st.ListArtifacts(ctx, taskID, false)
	kinds := map[string]bool{}
	for _, a := range arts {
		kinds[a.Kind] = true
	}
	if !kinds["report"] || !kinds["log"] {
		t.Fatalf("期望 report+log 产物，得到: %+v", arts)
	}
	// 3) skill 使用计数已增加
	sk, _ := st.GetSkill(ctx, skillID)
	if sk.UsageCount != 1 {
		t.Fatalf("skill 使用计数应为 1，得到 %d", sk.UsageCount)
	}
}
