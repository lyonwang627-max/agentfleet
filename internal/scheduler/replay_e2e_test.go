package scheduler

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 步级续跑 e2e（I11 第 2 轮，调度器级）：重试时重放通道携带 / 版本变化拒绝。

// captureRunner 捕获 ExecRequest 供断言。
type captureRunner struct{ captured *runner.ExecRequest }

func (c *captureRunner) Execute(ctx context.Context, req runner.ExecRequest) (runner.Result, error) {
	cp := req
	c.captured = &cp
	return runner.Result{Reply: "完成"}, nil
}

func replayFixture(t *testing.T) (*Scheduler, *store.MemStore, context.Context, *model.Agent, *model.Task, int64, *captureRunner) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	capR := &captureRunner{}
	runner.Register("mock", capR) // 全局注册表：mock 槽位换捕获型（测试后清理）
	t.Cleanup(func() { runner.Register("mock", runner.NewMock()) })
	aid, err := st.CreateAgent(ctx, &model.Agent{Name: "rp", Model: "mock", Runner: "mock", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := st.CreateTask(ctx, &model.Task{Title: "重放任务", Prompt: "做事", AgentID: aid})
	if err != nil {
		t.Fatal(err)
	}
	// 上次失败的 run：确定性 tool 步（含全量 result）+ 版本 v1
	rid, err := st.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid, Status: model.RunFailed, AgentVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.AppendStep(ctx, rid, &model.RunStep{Kind: model.StepTool, Name: "mcp:demo/echo",
		Input: map[string]any{"msg": "hi"}, Deterministic: true,
		Output: map[string]any{"result": "echo: 上次观察", "result_head": "echo: 上次观察"}})
	if err != nil {
		t.Fatal(err)
	}
	// 任务重试中（RetryCount=1）
	task, _ := st.GetTask(ctx, tid)
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning), RetryCount: ptrInt(1)})
	task, _ = st.GetTask(ctx, tid)
	agent, _ := st.GetAgent(ctx, aid)
	return s, st, ctx, agent, task, rid, capR
}

func TestReplayCarriedWhenVersionSame(t *testing.T) {
	s, st, ctx, agent, task, _, capR := replayFixture(t)
	// 版本链 v1（与 run.AgentVersion 一致）
	_, _, _ = st.SaveAgentVersion(ctx, &model.AgentVersion{AgentID: agent.ID, Version: 1,
		Config: map[string]any{"name": "rp"}})

	s.execute(ctx, *task)

	if capR.captured == nil || len(capR.captured.ReplayTools) != 1 {
		t.Fatalf("重放通道应携带 1 条: %+v", capR.captured)
	}
	rt := capR.captured.ReplayTools[0]
	if rt.Ref != "mcp:demo/echo" || !strings.Contains(rt.Result, "上次观察") {
		t.Fatalf("重放内容不符: %+v", rt)
	}
}

func TestReplayRejectedOnVersionChange(t *testing.T) {
	s, st, ctx, agent, task, rid, capR := replayFixture(t)
	// 版本链 v1 → v2（配置在失败 run 之后变更）
	_, _, _ = st.SaveAgentVersion(ctx, &model.AgentVersion{AgentID: agent.ID, Version: 1,
		Config: map[string]any{"name": "rp"}})
	_, _, _ = st.SaveAgentVersion(ctx, &model.AgentVersion{AgentID: agent.ID, Version: 2,
		Config: map[string]any{"name": "rp2"}})

	s.execute(ctx, *task)

	if capR.captured == nil {
		t.Fatal("execute 未运行")
	}
	if len(capR.captured.ReplayTools) != 0 {
		t.Fatalf("版本变化应拒绝重放: %+v", capR.captured.ReplayTools)
	}
	// 拒绝事件留痕
	evs, _ := st.EventsSince(ctx, 0, 100)
	found := false
	for _, e := range evs {
		if e.Kind == "task.replay_rejected" {
			found = true
		}
	}
	if !found {
		t.Fatal("未发出 task.replay_rejected 事件")
	}
	_ = rid
}

func TestReplayNoChannelWhenRetryZero(t *testing.T) {
	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "z", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "首跑", Prompt: "p", AgentID: aid})
	// 无历史失败 run、RetryCount=0
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})

	// 补一个 done run（非失败）：RetryCount=0 时整段重放逻辑不进
	_, _ = st.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid, Status: model.RunDone, AgentVersion: 1})
	capR2 := &captureRunner{}
	runner.Register("mock", capR2)
	t.Cleanup(func() { runner.Register("mock", runner.NewMock()) })
	task, _ := st.GetTask(ctx, tid)

	s.execute(ctx, *task)

	if capR2.captured == nil {
		t.Fatal("execute 未运行")
	}
	if len(capR2.captured.ReplayTools) != 0 {
		t.Fatalf("首跑不应带重放: %+v", capR2.captured.ReplayTools)
	}
}

// I34 会话续跑（调度器级）：重试时 ResumeSession 携带上次 CC session；版本变化一并拒绝。
func TestResumeSessionCarriedAndRejected(t *testing.T) {
	// 携带：fixture 的失败 run 带 SessionID，版本链 v1 一致
	s, st, ctx, agent, task, _, capR := replayFixture(t)
	if runs, _ := st.ListRuns(ctx, task.ID); len(runs) > 0 {
		_ = st.SetRunSession(ctx, runs[0].ID, "sess-cc-1")
	}
	_, _, _ = st.SaveAgentVersion(ctx, &model.AgentVersion{AgentID: agent.ID, Version: 1,
		Config: map[string]any{"name": "rp"}})
	s.execute(ctx, *task)
	if capR.captured == nil || capR.captured.ResumeSession != "sess-cc-1" {
		t.Fatalf("ResumeSession 应携带: %+v", capR.captured)
	}

	// 拒绝：版本升 v2 → ResumeSession 与重放一并清空
	s, st, ctx, agent, task, _, capR = replayFixture(t)
	if runs, _ := st.ListRuns(ctx, task.ID); len(runs) > 0 {
		_ = st.SetRunSession(ctx, runs[0].ID, "sess-cc-1")
	}
	_, _, _ = st.SaveAgentVersion(ctx, &model.AgentVersion{AgentID: agent.ID, Version: 1,
		Config: map[string]any{"name": "rp"}})
	_, _, _ = st.SaveAgentVersion(ctx, &model.AgentVersion{AgentID: agent.ID, Version: 2,
		Config: map[string]any{"name": "rp2"}})
	s.execute(ctx, *task)
	if capR.captured == nil {
		t.Fatal("execute 未运行")
	}
	if capR.captured.ResumeSession != "" {
		t.Fatalf("版本变化应拒绝会话续跑: %q", capR.captured.ResumeSession)
	}
}
