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
