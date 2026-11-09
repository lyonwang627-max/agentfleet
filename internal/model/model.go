// Package model 定义领域对象。字段与 docs/api-contract.md 一一对应。
package model

import "time"

type Agent struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Role           string    `json:"role"`
	SystemPrompt   string    `json:"system_prompt"`
	Model          string    `json:"model"`
	Tools          []string  `json:"tools"`
	Runner         string    `json:"runner"` // llm | mock
	MaxConcurrency int       `json:"max_concurrency"`
	TokenBudget24h int       `json:"token_budget_24h"` // 24h token 预算；0=不限
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// 任务状态机：queued -> running -> (review -> queued | done | failed -> queued)
type TaskStatus string

const (
	TaskQueued   TaskStatus = "queued"
	TaskRunning  TaskStatus = "running"
	TaskReview   TaskStatus = "review"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
	TaskCanceled TaskStatus = "canceled"
)

type Task struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	Prompt       string     `json:"prompt"`
	AgentID      int64      `json:"agent_id"`
	AgentName    string     `json:"agent_name,omitempty"`
	Priority     int        `json:"priority"` // 1-9，9 最高
	Status       TaskStatus `json:"status"`
	DependsOn    []int64    `json:"depends_on"`
	WorkspaceID  int64      `json:"workspace_id,omitempty"` // 绑定执行环境（0=不绑定）
	SquadID      int64      `json:"squad_id,omitempty"`    // 分给小队（I30：执行时唤醒 leader）
	SkillIDs     []int64    `json:"skill_ids,omitempty"`    // 引用的经验（创建时注入 prompt）
	RiskLevel    string     `json:"risk_level,omitempty"`   // I29: low|medium|high（审批分级）
	HandoffCount int        `json:"-"`
	MaxHandoffs  int        `json:"-"`
	RetryCount   int        `json:"-"`
	MaxRetries   int        `json:"-"`
	VisibleAfter time.Time  `json:"-"`
	Result       string     `json:"result,omitempty"`
	Error        string     `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

func (t *Task) BlockedBy() []int64 { return t.DependsOn }

type StepKind string

const (
	StepLLM     StepKind = "llm"
	StepTool    StepKind = "tool"
	StepHandoff StepKind = "handoff"
	StepGate    StepKind = "gate"
)

type RunStep struct {
	Seq       int64          `json:"seq"`
	Kind      StepKind       `json:"kind"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	Note      string         `json:"note,omitempty"`
	LatencyMS int64          `json:"latency_ms"`
	Tokens    int            `json:"tokens"`
	CreatedAt time.Time      `json:"created_at"`
}

type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunDone    RunStatus = "done"
	RunFailed  RunStatus = "failed"
)

type Run struct {
	ID           int64      `json:"id"`
	TaskID       int64      `json:"task_id"`
	AgentID      int64      `json:"agent_id"`
	Conversation int64      `json:"conversation_id,omitempty"`
	Status       RunStatus  `json:"status"`
	TotalTokens  int        `json:"total_tokens"`
	CostUSD      float64    `json:"cost_usd"`
	Error        string     `json:"error,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	Steps        []RunStep  `json:"steps,omitempty"`
}

type Conversation struct {
	ID        int64     `json:"id"`
	AgentID   int64     `json:"agent_id"`
	AgentName string    `json:"agent_name,omitempty"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// ConversationSummary 会话列表项（F6 回放数据层）：含消息数与关联 run 数。
type ConversationSummary struct {
	ID           int64     `json:"id"`
	AgentID      int64     `json:"agent_id"`
	AgentName    string    `json:"agent_name,omitempty"`
	Title        string    `json:"title"`
	MessageCount int       `json:"message_count"`
	RunCount     int       `json:"run_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type Message struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Role           string    `json:"role"` // user | agent
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

type UsageEvent struct {
	ID               int64     `json:"id"`
	AgentID          int64     `json:"agent_id"`
	AgentName        string    `json:"agent_name,omitempty"`
	Model            string    `json:"model"`
	Purpose          string    `json:"purpose,omitempty"` // F18-a：execute | chat | judge | review（路由可观测维度）
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	Requests         int       `json:"requests"`
	CreatedAt        time.Time `json:"created_at"`
}

// ---- 评测 ----

type EvalDataset struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CaseCount   int       `json:"cases"`
	CreatedAt   time.Time `json:"created_at"`
}

type EvalCase struct {
	ID        int64          `json:"id"`
	DatasetID int64          `json:"dataset_id"`
	Input     string         `json:"input"`
	Expected  map[string]any `json:"expected"` // 评分要点 points []string、threshold int
}

type EvalRun struct {
	ID        int64       `json:"id"`
	DatasetID int64       `json:"dataset_id"`
	AgentID   int64       `json:"agent_id"`
	Status    string      `json:"status"` // running | done | failed
	ScoreAvg  float64     `json:"score_avg"`
	PassRate  float64     `json:"pass_rate"`
	Report    *EvalReport `json:"report,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

// Prescription RSI 处方（F17）：评测驱动的 system_prompt 改进建议（应用走版本管理可回滚）。
type Prescription struct {
	Diagnosis            string `json:"diagnosis"`
	SystemPromptAddition string `json:"system_prompt_addition"`
	Rationale            string `json:"rationale"`
	BasedOnEval          int64  `json:"based_on_eval"`
	FailureCases         int    `json:"failure_cases"`
}

type EvalReport struct {
	Cases        []EvalCaseResult `json:"cases"`
	ScoreAvg     float64          `json:"score_avg"`
	PassRate     float64          `json:"pass_rate"`
	Prescription *Prescription    `json:"prescription,omitempty"` // F17 RSI 处方（additive）
}

type EvalCaseResult struct {
	CaseID       int64  `json:"case_id"`
	Score        int    `json:"score"`
	Pass         bool   `json:"pass"`
	JudgeComment string `json:"judge_comment"`
}

// AgentVersion Agent 配置的不可变版本快照（F2）。版本链只前进：回滚=用旧快照覆盖配置+发布新版本。
type AgentVersion struct {
	ID        int64          `json:"id"`
	AgentID   int64          `json:"agent_id"`
	Version   int            `json:"version"`
	Config    map[string]any `json:"config"`
	Note      string         `json:"note"`
	CreatedAt time.Time      `json:"created_at"`
}

// Automation 自动化规则：按 cron 表达式周期性创建任务（对齐 Multica/Orca 的自动化心智）。
type Automation struct {
	ID        int64      `json:"id"`
	AgentID   int64      `json:"agent_id"`
	AgentName string     `json:"agent_name,omitempty"`
	Name      string     `json:"name"`
	CronExpr  string     `json:"cron_expr"`
	Title     string     `json:"title"`
	Prompt    string     `json:"prompt"`
	Priority  int        `json:"priority"`
	Enabled   bool       `json:"enabled"`
	LastRun   *time.Time `json:"last_run"`
	NextRun   *time.Time `json:"next_run"`
	CreatedAt time.Time  `json:"created_at"`
}

// Artifact 任务产物：报告/补丁/diff/数据文件等落盘产出（对标 Orca artifacts）。
type Artifact struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	RunID     int64     `json:"run_id"`
	Kind      string    `json:"kind"` // report | patch | diff | data | log
	Name      string    `json:"name"`
	MimeType  string    `json:"mime_type"`
	Size      int       `json:"size"`
	Content   string    `json:"content,omitempty"` // 列表不带，详情带
	CreatedAt time.Time `json:"created_at"`
}

// Workspace 执行环境：任务绑定的独立工作目录（对标 Multica worktree / Orca folder context）。
type Workspace struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"root_path"`
	Branch    string    `json:"branch"`
	Note      string    `json:"note"`
	Status    string    `json:"status"` // active | archived
	CreatedAt time.Time `json:"created_at"`
}

// Skill 经验沉淀：任务完成后固化的可复用 SOP/要点，创建任务时可引用注入（对标 Multica compound skills）。
type Skill struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Content    string    `json:"content"`
	Tags       []string  `json:"tags"`
	UsageCount int       `json:"usage_count"`
	SourceTask int64     `json:"source_task,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// APIKey 认证凭据（I19）：明文只在创建响应出现一次，库存哈希。
type APIKey struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	KeyHash     string     `json:"-"`
	KeyPrefix   string     `json:"key_prefix"`
	Scopes      string     `json:"scopes"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Squad 小队（I30，对标 Multica squads）：leader（必为 agent）+ 混编成员。
// 任务分给 squad 只唤醒 leader；leader 读 Roster 动态决策转派。
type Squad struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	LeaderID     int64     `json:"leader_id"` // 必为 agent
	Instructions string    `json:"instructions"`
	CreatedAt    time.Time `json:"created_at"`
}

// SquadMember 成员（agent 或人——人暂作占位记录不参与执行）。
type SquadMember struct {
	ID         int64     `json:"id"`
	SquadID    int64     `json:"squad_id"`
	MemberType string    `json:"member_type"` // agent | human
	MemberID   int64     `json:"member_id"`
	Role       string    `json:"role"`
}

// SSE 事件
type Event struct {
	Seq       int64     `json:"seq"`
	Kind      string    `json:"kind"` // task.update | agent.update | run.step | eval.update
	Data      any       `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}
