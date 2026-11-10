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
	GetAgentByName(ctx context.Context, name string) (*model.Agent, error)
	ListAgents(ctx context.Context, f AgentFilter) ([]model.Agent, error)
	UpdateAgent(ctx context.Context, id int64, patch AgentPatch) error
	UpdateAgentDraft(ctx context.Context, id int64, patch AgentPatch) error
	PublishAgentDraft(ctx context.Context, id int64) error
	ClearAgentDraft(ctx context.Context, id int64) error
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
	ListConversations(ctx context.Context, limit int) ([]model.ConversationSummary, error)
	AppendMessage(ctx context.Context, m *model.Message) (int64, error)
	ListMessages(ctx context.Context, conversationID int64) ([]model.Message, error)

	// usage
	AppendUsage(ctx context.Context, u *model.UsageEvent) error
	UsageSummary(ctx context.Context, window time.Duration) (total model.UsageEvent, byModel []model.UsageEvent, byAgent []model.UsageEvent, err error)
	UsageByPurpose(ctx context.Context, window time.Duration) ([]model.UsageEvent, error)

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

	// artifacts（任务产物）
	SaveArtifact(ctx context.Context, a *model.Artifact) (int64, error)
	ListArtifacts(ctx context.Context, taskID int64, withContent bool) ([]model.Artifact, error)
	GetArtifact(ctx context.Context, id int64) (*model.Artifact, error)

	// workspaces（执行环境）
	CreateWorkspace(ctx context.Context, w *model.Workspace) (int64, error)
	GetWorkspace(ctx context.Context, id int64) (*model.Workspace, error)
	ListWorkspaces(ctx context.Context, activeOnly bool) ([]model.Workspace, error)
	UpdateWorkspace(ctx context.Context, id int64, patch WorkspacePatch) error
	DeleteWorkspace(ctx context.Context, id int64) error

	// skills（经验沉淀）
	SaveSkill(ctx context.Context, s *model.Skill) (int64, error)
	GetSkill(ctx context.Context, id int64) (*model.Skill, error)
	ListSkills(ctx context.Context, tag string) ([]model.Skill, error)
	UpdateSkill(ctx context.Context, s *model.Skill) error
	DeleteSkill(ctx context.Context, id int64) error
	IncrSkillUsage(ctx context.Context, id int64) error

	// projects + labels + comments（Multica 对标）
	CreateProject(ctx context.Context, pr *model.Project) (int64, error)
	GetProject(ctx context.Context, id int64) (*model.Project, error)
	ListProjects(ctx context.Context) ([]model.Project, error)
	DeleteProject(ctx context.Context, id int64) error
	AddTaskComment(ctx context.Context, c *model.TaskComment) (int64, error)
	ListTaskComments(ctx context.Context, taskID int64) ([]model.TaskComment, error)

	// squads（I30 小队）
	CreateSquad(ctx context.Context, sq *model.Squad) (int64, error)
	GetSquad(ctx context.Context, id int64) (*model.Squad, error)
	ListSquads(ctx context.Context) ([]model.Squad, error)
	UpdateSquad(ctx context.Context, id int64, patch SquadPatch) error
	DeleteSquad(ctx context.Context, id int64) error
	AddSquadMember(ctx context.Context, m *model.SquadMember) error
	RemoveSquadMember(ctx context.Context, id int64) error
	ListSquadMembers(ctx context.Context, squadID int64) ([]model.SquadMember, error)

	// api keys（I19 认证）
	CreateAPIKey(ctx context.Context, name, keyHash, keyPrefix string) (int64, error)
	ListAPIKeys(ctx context.Context) ([]model.APIKey, error)
	FindAPIKeyByHash(ctx context.Context, keyHash string) (*model.APIKey, error)
	DeleteAPIKey(ctx context.Context, id int64) error
	TouchAPIKey(ctx context.Context, id int64) error
	HasAnyAPIKey(ctx context.Context) (bool, error)

	// events（SSE 增量拉取用）
	AppendEvent(ctx context.Context, e *model.Event) (int64, error)
	EventsSince(ctx context.Context, since int64, limit int) ([]model.Event, error)

	Close() error
	Ping(ctx context.Context) error
}

type SquadPatch struct {
	Name         *string
	Description  *string
	LeaderID     *int64
	Instructions *string
}

type WorkspacePatch struct {
	Name          *string
	Branch        *string
	Note          *string
	Status        *string
	RuntimeStatus *string // I16：provisioning | ready | degraded
	RuntimeNote   *string // I16：最近探测原因
}

// I14 草稿/发布双态：PATCH 只进草稿（编辑不影响在途 run），publish 落线 + 版本快照。
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
	ProjectID *int64
	Labels    *[]string
	SteerNote *string
	Title        *string
	Prompt       *string
	AgentID      *int64
	Priority     *int
	Status       *model.TaskStatus
	Result       *string
	Error        *string
	WorkspaceID  *int64
	SkillIDs     *[]int64
	DependsOn    *[]int64
	RiskLevel    *string
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
