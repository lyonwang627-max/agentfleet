package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fergusstrange/embedded-postgres"
	"github.com/lyonwang/agentfleet/internal/model"
)

// PostgresStore 真机冒烟（此前零真机验证）：测试内嵌真 PG 17（不污染系统），
// 跑本周加列密集区与 jsonb 兼容面——mem 双实现下最难暴露的 SQL/scan 缺陷在这里现形。

func newEmbeddedPG(t *testing.T) *PostgresStore {
	t.Helper()
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V17).
		Username("fleet").Password("fleet").Database("fleet").
		Port(5499).
		RuntimePath(t.TempDir()).
		DataPath(t.TempDir()))
	if err := pg.Start(); err != nil {
		t.Skipf("内嵌 PG 启动失败（环境限制，跳过真机冒烟）: %v", err)
	}
	t.Cleanup(func() { _ = pg.Stop() })
	ctx := context.Background()
	p, err := NewPostgresStore(ctx, "postgres://fleet:fleet@127.0.0.1:5499/fleet?sslmode=disable", "priority")
	if err != nil {
		t.Skipf("PostgresStore 连接失败: %v", err)
	}
	return p
}

func TestPostgresStoreSmoke(t *testing.T) {
	p := newEmbeddedPG(t)
	ctx := context.Background()

	// ---- agent + I14 草稿/发布（agentCols 扩列区）----
	aid, err := p.CreateAgent(ctx, &model.Agent{Name: "pg-a", Model: "mock", Runner: "mock", SystemPrompt: "原始", Enabled: true})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := p.UpdateAgentDraft(ctx, aid, AgentPatch{SystemPrompt: strPtr("草稿提示词")}); err != nil {
		t.Fatalf("UpdateAgentDraft: %v", err)
	}
	a, err := p.GetAgent(ctx, aid)
	if err != nil {
		t.Fatal(err)
	}
	if a.SystemPrompt != "原始" || a.DraftConfig == nil || a.DraftConfig["system_prompt"] != "草稿提示词" {
		t.Fatalf("草稿读写不符: %+v", a)
	}
	if err := p.PublishAgentDraft(ctx, aid); err != nil {
		t.Fatalf("PublishAgentDraft: %v", err)
	}
	a, _ = p.GetAgent(ctx, aid)
	if a.SystemPrompt != "草稿提示词" || a.DraftConfig != nil {
		t.Fatalf("落线不符: %+v", a)
	}

	// ---- task：DependsOn/SteerNote patch 往返 ----
	tid, err := p.CreateTask(ctx, &model.Task{Title: "pg任务", Prompt: "p", AgentID: aid, DependsOn: []int64{}})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := p.UpdateTask(ctx, tid, TaskPatch{DependsOn: &[]int64{99}, SteerNote: strPtr("插话")}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	tk, _ := p.GetTask(ctx, tid)
	if len(tk.DependsOn) != 1 || tk.DependsOn[0] != 99 || tk.SteerNote != "插话" {
		t.Fatalf("task patch 往返不符: deps=%v steer=%q", tk.DependsOn, tk.SteerNote)
	}

	// ---- run/steps：I11/I17/I34 扩列区（agent_version/session_id/parent_seq/deterministic）----
	rid, err := p.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid, Status: model.RunDone,
		AgentVersion: 2, SessionID: "sess-pg-1", TotalTokens: 42})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	seq, err := p.AppendStep(ctx, rid, &model.RunStep{Kind: model.StepTool, Name: "mcp:demo/echo",
		Input: map[string]any{"msg": "hi"}, ParentSeq: 0, Deterministic: true, Tokens: 42,
		Output: map[string]any{"result": "echo: pg-观察", "result_head": "echo: pg-观察"}})
	if err != nil {
		t.Fatalf("AppendStep: %v", err)
	}
	if seq != 1 {
		t.Fatalf("seq 应 1: %d", seq)
	}
	run, err := p.GetRun(ctx, rid)
	if err != nil {
		t.Fatal(err)
	}
	if run.AgentVersion != 2 || run.SessionID != "sess-pg-1" || len(run.Steps) != 1 {
		t.Fatalf("run 往返不符: ver=%d sess=%q steps=%d", run.AgentVersion, run.SessionID, len(run.Steps))
	}
	if !run.Steps[0].Deterministic || run.Steps[0].Output["result"] != "echo: pg-观察" {
		t.Fatalf("step 往返不符: %+v", run.Steps[0])
	}

	// ---- I11 重放通道取数（调度器同款查询）----
	if err := p.SetRunSession(ctx, rid, "sess-pg-2"); err != nil {
		t.Fatalf("SetRunSession: %v", err)
	}
	run, _ = p.GetRun(ctx, rid)
	if run.SessionID != "sess-pg-2" {
		t.Fatalf("session 更新失败: %q", run.SessionID)
	}

	// ---- webhook：jsonb events 往返 + 观测列 ----
	whID, err := p.CreateWebhook(ctx, &model.Webhook{Name: "pg-hook", URL: "https://x/y",
		Secret: "s", Events: []string{"task.done", "task.failed"}, Active: true})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	hooks, _ := p.ListWebhooks(ctx)
	if len(hooks) != 1 || len(hooks[0].Events) != 2 || hooks[0].Events[0] != "task.done" {
		t.Fatalf("webhook jsonb 往返不符: %+v", hooks)
	}
	if err := p.SetWebhookStatus(ctx, whID, "ok"); err != nil {
		t.Fatalf("SetWebhookStatus: %v", err)
	}
	hooks, _ = p.ListWebhooks(ctx)
	if hooks[0].LastStatus != "ok" || hooks[0].LastDeliveryAt == nil {
		t.Fatalf("观测列不符: %+v", hooks[0])
	}

	// ---- agent 版本链 ----
	if _, v, err := p.SaveAgentVersion(ctx, &model.AgentVersion{AgentID: aid,
		Config: map[string]any{"name": "pg-a"}}); err != nil || v != 1 {
		t.Fatalf("SaveAgentVersion: v=%d err=%v", v, err)
	}
	vers, _ := p.ListAgentVersions(ctx, aid)
	if len(vers) != 1 || vers[0].Version != 1 {
		t.Fatalf("版本链不符: %+v", vers)
	}
}

func TestPostgresUsageSummary(t *testing.T) {
	p := newEmbeddedPG(t)
	ctx := context.Background()
	aid, _ := p.CreateAgent(ctx, &model.Agent{Name: "u", Model: "mock", Runner: "mock", Enabled: true})
	if err := p.AppendUsage(ctx, &model.UsageEvent{AgentID: aid, AgentName: "u", Model: "mock",
		Purpose: "execute", PromptTokens: 10, CompletionTokens: 20, Requests: 1}); err != nil {
		t.Fatalf("AppendUsage: %v", err)
	}
	if err := p.AppendUsage(ctx, &model.UsageEvent{AgentID: aid, AgentName: "u", Model: "mock",
		Purpose: "chat", PromptTokens: 5, CompletionTokens: 5, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	agg, byModel, byAgent, err := p.UsageSummary(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if agg.PromptTokens+agg.CompletionTokens == 0 || len(byModel) == 0 || len(byAgent) == 0 {
		t.Fatalf("用量聚合空: %+v %v %v", agg, byModel, byAgent)
	}
	byPurpose, err := p.UsageByPurpose(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(byPurpose) < 2 {
		t.Fatalf("purpose 分桶不符: %+v", byPurpose)
	}
}

// strPtr 测试 helper（pg 冒烟用）。
func strPtr(s string) *string { return &s }

// 编译期守卫：database/sql 由 embedded-postgres 传递依赖使用（保持导入面诚实）。
var _ = sql.ErrNoRows

// ---- 冒烟扩面（I36 第二波）： squads/projects/comments/automations/skills/evals/api_keys ----

func TestPostgresStoreSmokeSecondWave(t *testing.T) {
	p := newEmbeddedPG(t)
	ctx := context.Background()
	aid, _ := p.CreateAgent(ctx, &model.Agent{Name: "w2", Model: "mock", Runner: "mock", Enabled: true})

	// ---- squads：建队/成员/删除（UNIQUE 幂等）----
	sqid, err := p.CreateSquad(ctx, &model.Squad{Name: "pg小队", LeaderID: aid})
	if err != nil {
		t.Fatalf("CreateSquad: %v", err)
	}
	mem := &model.SquadMember{SquadID: sqid, MemberType: "agent", MemberID: aid, Role: "member"}
	if err := p.AddSquadMember(ctx, mem); err != nil {
		t.Fatalf("AddSquadMember: %v", err)
	}
	if err := p.AddSquadMember(ctx, mem); err != nil {
		t.Fatalf("成员重复应幂等: %v", err)
	}
	sq, err := p.GetSquad(ctx, sqid)
	if err != nil || sq.LeaderID != aid {
		t.Fatalf("GetSquad 不符: %+v err=%v", sq, err)
	}

	// ---- projects + task comments（Multica 面）----
	pid, err := p.CreateProject(ctx, &model.Project{Name: "pg项目", Repos: []string{"r1"}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	tid, _ := p.CreateTask(ctx, &model.Task{Title: "评论任务", Prompt: "p", AgentID: aid, ProjectID: pid})
	if _, err := p.AddTaskComment(ctx, &model.TaskComment{TaskID: tid, AuthorType: "member",
		AuthorName: "you", Content: "pg 评论"}); err != nil {
		t.Fatalf("AddTaskComment: %v", err)
	}
	comments, err := p.ListTaskComments(ctx, tid)
	if err != nil || len(comments) != 1 || comments[0].Content != "pg 评论" {
		t.Fatalf("评论往返不符: %+v err=%v", comments, err)
	}
	// task project_id patch 往返（labels 同列区）
	_ = p.UpdateTask(ctx, tid, TaskPatch{Labels: &[]string{"sev1"}})
	tk, _ := p.GetTask(ctx, tid)
	if len(tk.Labels) != 1 || tk.Labels[0] != "sev1" {
		t.Fatalf("labels 往返不符: %v", tk.Labels)
	}

	// ---- automations：cron/next_run/禁用 ----
	autoID, err := p.CreateAutomation(ctx, &model.Automation{AgentID: aid, Name: "晨会",
		CronExpr: "30 9 * * 1-5", Title: "晨会摘要", Priority: 6})
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	aut, err := p.GetAutomation(ctx, autoID)
	if err != nil || aut.CronExpr != "30 9 * * 1-5" {
		t.Fatalf("automation 往返不符: %+v err=%v", aut, err)
	}
	disabled := false
	_ = p.UpdateAutomation(ctx, autoID, AutomationPatch{Enabled: &disabled})
	aut2, _ := p.GetAutomation(ctx, autoID)
	if aut2.Enabled {
		t.Fatal("禁用未生效")
	}

	// ---- skills：创建/引用计数/删除 ----
	skid, err := p.SaveSkill(ctx, &model.Skill{Name: "SOP", Content: "步骤 1/2/3", Tags: []string{"ops"}})
	if err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}
	_ = p.IncrSkillUsage(ctx, skid)
	sk, err := p.GetSkill(ctx, skid)
	if err != nil || sk.UsageCount != 1 {
		t.Fatalf("引用计数不符: %+v err=%v", sk, err)
	}
	if err := p.DeleteSkill(ctx, skid); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}

	// ---- eval datasets/cases ----
	did, err := p.CreateDataset(ctx, &model.EvalDataset{Name: "pg集"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SaveCases(ctx, did, []model.EvalCase{
		{Input: "题1", Expected: map[string]any{"points": []string{"要点A"}}}}); err != nil {
		t.Fatalf("SaveCases: %v", err)
	}
	cases, _ := p.ListCases(ctx, did)
	if len(cases) != 1 || cases[0].Input != "题1" {
		t.Fatalf("cases 往返不符: %+v", cases)
	}

	// ---- api keys：渐进语义数据面 ----
	kid, err := p.CreateAPIKey(ctx, "k1", "hash-1", "fleet_")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	keys, _ := p.ListAPIKeys(ctx)
	if len(keys) != 1 || keys[0].Name != "k1" {
		t.Fatalf("keys 往返不符: %+v", keys)
	}
	found, err := p.FindAPIKeyByHash(ctx, "hash-1")
	if err != nil || found.ID != kid {
		t.Fatalf("hash 查找不符: %+v err=%v", found, err)
	}
	if err := p.DeleteAPIKey(ctx, kid); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}

	// ---- squad/project 删除 ----
	if err := p.DeleteSquad(ctx, sqid); err != nil {
		t.Fatalf("DeleteSquad: %v", err)
	}
	if err := p.DeleteProject(ctx, pid); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
}

// 数据集版本化（I40 第 1 轮）：SaveCases 自增 + eval run 记录评测时版本。
func TestPostgresDatasetVersioning(t *testing.T) {
	p := newEmbeddedPG(t)
	ctx := context.Background()
	did, err := p.CreateDataset(ctx, &model.EvalDataset{Name: "版本集"})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := p.GetDataset(ctx, did)
	if d.Version != 1 {
		t.Fatalf("初始版本应 1: %d", d.Version)
	}
	// 两次 SaveCases → version 1→3
	_ = p.SaveCases(ctx, did, []model.EvalCase{{Input: "题1"}})
	_ = p.SaveCases(ctx, did, []model.EvalCase{{Input: "题2"}})
	d, _ = p.GetDataset(ctx, did)
	if d.Version != 3 {
		t.Fatalf("SaveCases 应自增版本: %d", d.Version)
	}
	// eval run 记录评测时版本
	if _, err := p.CreateEvalRun(ctx, &model.EvalRun{DatasetID: did, AgentID: 1}); err != nil {
		t.Fatal(err)
	}
	run, _ := p.GetEvalRun(ctx, func() int64 { r, _ := p.ListEvalRuns(ctx); return r[0].ID }())
	if run.DatasetVersion != 3 {
		t.Fatalf("eval run 应记录版本 3: %d", run.DatasetVersion)
	}
}

// I40 mem 侧：SaveCases 自增 + eval run 记录版本（双实现对齐验证）。
func TestMemDatasetVersioning(t *testing.T) {
	m := NewMemStore()
	ctx := context.Background()
	did, _ := m.CreateDataset(ctx, &model.EvalDataset{Name: "mem 版本集"})
	_ = m.SaveCases(ctx, did, []model.EvalCase{{Input: "题1"}})
	ds, _ := m.ListDatasets(ctx)
	var d *model.EvalDataset
	for i := range ds {
		if ds[i].ID == did {
			d = &ds[i]
		}
	}
	if d == nil {
		t.Fatal("dataset 缺失")
	}
	if d.Version != 2 {
		t.Fatalf("SaveCases 应自增至 2: %d", d.Version)
	}
	eid, _ := m.CreateEvalRun(ctx, &model.EvalRun{DatasetID: did, AgentID: 1})
	run, _ := m.GetEvalRun(ctx, eid)
	if run.DatasetVersion != 2 {
		t.Fatalf("eval run 应记录版本 2: %d", run.DatasetVersion)
	}
}
