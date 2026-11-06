// Package store 定义存储接口。两个实现：
//   - postgres.go：生产实现，任务认领用 FOR UPDATE SKIP LOCKED（不需要 Redis 的生产级队列）
//   - mem.go：进程内实现，用于 --mem 演示模式与单元测试（无外部依赖、全量可重放）
package store

import (
	"context"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

type AgentFilter struct {
	EnabledOnly bool
}

type Store interface {
	// agents
	CreateAgent(ctx context.Context, a *model.Agent) (int64, error)
	GetAgent(ctx context.Context, id int64) (*model.Agent, error)
	ListAgents(ctx context.Context, f AgentFilter) ([]model.Agent, error)
	UpdateAgent(ctx context.Context, id int64, patch AgentPatch) error
	// AgentTokensSince 某 agent 自某时刻起的 token 用量合计（预算熔断判定用）
	AgentTokensSince(ctx context.Context, agentID int64, since time.Time) (int64, error)

	// tasks
	CreateTask(ctx context.Context, t *model.Task) (int64, error)
	GetTask(ctx context.Context, id int64) (*model.Task, error)
	ListTasks(ctx context.Context, status *model.TaskStatus, agentID int64, page, pageSize int) ([]model.Task, int, error)
	UpdateTask(ctx context.Context, id int64, patch TaskPatch) error

	// 认领：按优先级取一个可执行任务（依赖满足、agent 启用、visible_after 已到），
	// 并原子地把状态置为 running。两个实现都必须是并发安全的。
	ClaimNextTask(ctx context.Context) (*model.Task, error)

	// runs & steps
	CreateRun(ctx context.Context, r *model.Run) (int64, error)
	UpdateRun(ctx context.Context, r *model.Run) error
	GetRun(ctx context.Context, id int64) (*model.Run, error)
	ListRuns(ctx context.Context, taskID int64) ([]model.Run, error)
	AppendStep(ctx context.Context, runID int64, s *model.RunStep) (int64, error)
	ListSteps(ctx context.Context, runID int64) ([]model.RunStep, error)

	// conversations
	CreateConversation(ctx context.Context, c *model.Conversation) (int64, error)
	GetConversation(ctx context.Context, id int64) (*model.Conversation, error)
	AppendMessage(ctx context.Context, m *model.Message) (int64, error)
	ListMessages(ctx context.Context, conversationID int64) ([]model.Message, error)

	// usage
	AppendUsage(ctx context.Context, u *model.UsageEvent) error
	UsageSummary(ctx context.Context, window time.Duration) (total model.UsageEvent, byModel []model.UsageEvent, byAgent []model.UsageEvent, err error)

	// evals
	CreateDataset(ctx context.Context, d *model.EvalDataset) (int64, error)
	ListDatasets(ctx context.Context) ([]model.EvalDataset, error)
	CreateEvalRun(ctx context.Context, e *model.EvalRun) (int64, error)
	UpdateEvalRun(ctx context.Context, e *model.EvalRun) error
	GetEvalRun(ctx context.Context, id int64) (*model.EvalRun, error)
	ListEvalRuns(ctx context.Context) ([]model.EvalRun, error)
	SaveCases(ctx context.Context, datasetID int64, cases []model.EvalCase) error
	ListCases(ctx context.Context, datasetID int64) ([]model.EvalCase, error)

	// automations（cron 定时建任务）
	CreateAutomation(ctx context.Context, a *model.Automation) (int64, error)
	GetAutomation(ctx context.Context, id int64) (*model.Automation, error)
	ListAutomations(ctx context.Context, enabledOnly bool) ([]model.Automation, error)
	UpdateAutomation(ctx context.Context, id int64, patch AutomationPatch) error
	DeleteAutomation(ctx context.Context, id int64) error
	MarkAutomationRan(ctx context.Context, id int64, lastRun, nextRun time.Time) error

	// agent versions（F2：发布/回滚，版本链只前进）
	SaveAgentVersion(ctx context.Context, v *model.AgentVersion) (int64, int, error) // 返回 (id, version)
	ListAgentVersions(ctx context.Context, agentID int64) ([]model.AgentVersion, error)
	GetAgentVersion(ctx context.Context, agentID int64, version int) (*model.AgentVersion, error)

	// events（SSE 增量拉取用）
	AppendEvent(ctx context.Context, e *model.Event) (int64, error)
	EventsSince(ctx context.Context, since int64, limit int) ([]model.Event, error)

	Close() error
	Ping(ctx context.Context) error
}

type AgentPatch struct {
	Name           *string
	Role           *string
	SystemPrompt   *string
	Model          *string
	Tools          *[]string
	Runner         *string
	MaxConcurrency *int
	TokenBudget24h *int
	Enabled        *bool
}

type TaskPatch struct {
	Title        *string
	Prompt       *string
	AgentID      *int64
	Priority     *int
	Status       *model.TaskStatus
	Result       *string
	Error        *string
	HandoffCount *int
	RetryCount   *int
	VisibleAfter *time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type AutomationPatch struct {
	AgentID  *int64
	Name     *string
	CronExpr *string
	Title    *string
	Prompt   *string
	Priority *int
	Enabled  *bool
}
