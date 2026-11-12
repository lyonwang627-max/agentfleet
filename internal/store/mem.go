package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// MemStore 进程内实现：演示模式（--mem）与单元测试共用，语义与 Postgres 版对齐。
type MemStore struct {
	claimStrategy ClaimStrategy
	mu            sync.RWMutex
	agents        map[int64]*model.Agent
	tasks         map[int64]*model.Task
	webhooks      map[int64]*model.Webhook
	runs          map[int64]*model.Run
	steps         map[int64][]model.RunStep // run_id -> steps
	convos        map[int64]*model.Conversation
	msgs          map[int64][]model.Message // conversation_id -> messages
	usage         []model.UsageEvent
	datasets      map[int64]*model.EvalDataset
	cases         map[int64][]model.EvalCase
	evalRuns      map[int64]*model.EvalRun
	automations   map[int64]*model.Automation
	agentVersions map[int64]*model.AgentVersion
	artifacts     map[int64]*model.Artifact
	workspaces    map[int64]*model.Workspace
	skills        map[int64]*model.Skill
	apiKeys       map[int64]*model.APIKey
	squads        map[int64]*model.Squad
	squadMembers  map[int64]*model.SquadMember
	projects      map[int64]*model.Project
	taskComments  map[int64]*model.TaskComment
	events        []model.Event
	nextID        int64
	lastEventSeq  int64
}

func NewMemStore() *MemStore {
	return NewMemStoreWithStrategy("priority")
}

// NewMemStoreWithStrategy 指定认领策略构建（I10）。
func NewMemStoreWithStrategy(name string) *MemStore {
	m := &MemStore{
		agents: map[int64]*model.Agent{}, tasks: map[int64]*model.Task{}, webhooks: map[int64]*model.Webhook{},
		claimStrategy: ClaimStrategyByName(name),
		runs: map[int64]*model.Run{}, steps: map[int64][]model.RunStep{},
		convos: map[int64]*model.Conversation{}, msgs: map[int64][]model.Message{},
		datasets: map[int64]*model.EvalDataset{}, cases: map[int64][]model.EvalCase{},
		evalRuns: map[int64]*model.EvalRun{}, automations: map[int64]*model.Automation{},
		apiKeys:       map[int64]*model.APIKey{},
		agentVersions: map[int64]*model.AgentVersion{},
		artifacts:     map[int64]*model.Artifact{},
		workspaces:    map[int64]*model.Workspace{},
		skills:        map[int64]*model.Skill{},
		squads:        map[int64]*model.Squad{},
		squadMembers:  map[int64]*model.SquadMember{},
		projects:      map[int64]*model.Project{},
		taskComments:  map[int64]*model.TaskComment{},
	}
	return m
}

func (m *MemStore) id() int64 { m.nextID++; return m.nextID }

func (m *MemStore) Ping(_ context.Context) error { return nil }
func (m *MemStore) Close() error                 { return nil }

// ---- agents ----

func (m *MemStore) CreateAgent(_ context.Context, a *model.Agent) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID = m.id()
	a.CreatedAt = time.Now()
	a.UpdatedAt = a.CreatedAt
	cp := *a
	m.agents[a.ID] = &cp
	return a.ID, nil
}

func (m *MemStore) GetAgent(_ context.Context, id int64) (*model.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	cp := *a
	return &cp, nil
}

func (m *MemStore) ListAgents(_ context.Context, f AgentFilter) ([]model.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Agent, 0, len(m.agents))
	for _, a := range m.agents {
		if f.EnabledOnly && !a.Enabled {
			continue
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemStore) UpdateAgent(_ context.Context, id int64, p AgentPatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	return m.applyAgentPatch(a, p)
}

// applyAgentPatch 无锁应用（调用方须持 m.mu；PublishAgentDraft 落线复用）。
func (m *MemStore) applyAgentPatch(a *model.Agent, p AgentPatch) error {
	if p.Name != nil {
		a.Name = *p.Name
	}
	if p.Role != nil {
		a.Role = *p.Role
	}
	if p.SystemPrompt != nil {
		a.SystemPrompt = *p.SystemPrompt
	}
	if p.Model != nil {
		a.Model = *p.Model
	}
	if p.Tools != nil {
		a.Tools = *p.Tools
	}
	if p.Runner != nil {
		a.Runner = *p.Runner
	}
	if p.MaxConcurrency != nil {
		a.MaxConcurrency = *p.MaxConcurrency
	}
	if p.TokenBudget24h != nil {
		a.TokenBudget24h = *p.TokenBudget24h
	}
	if p.Enabled != nil {
		a.Enabled = *p.Enabled
	}
	a.UpdatedAt = time.Now()
	return nil
}

// AgentTokensSince 内存版：线性扫 usage（演示规模足够；生产走 PG 聚合）。
func (m *MemStore) AgentTokensSince(_ context.Context, agentID int64, since time.Time) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var sum int64
	for _, u := range m.usage {
		if u.AgentID == agentID && !u.CreatedAt.Before(since) {
			sum += int64(u.PromptTokens + u.CompletionTokens)
		}
	}
	return sum, nil
}

// ---- tasks ----

func (m *MemStore) CreateTask(_ context.Context, t *model.Task) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.ID = m.id()
	if t.Status == "" {
		t.Status = model.TaskQueued
	}
	if t.MaxHandoffs == 0 {
		t.MaxHandoffs = 3
	}
	if t.MaxRetries == 0 {
		t.MaxRetries = 2
	}
	t.CreatedAt = time.Now()
	cp := *t
	m.tasks[t.ID] = &cp
	return t.ID, nil
}

func (m *MemStore) GetTask(_ context.Context, id int64) (*model.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %d: %w", id, ErrNotFound)
	}
	cp := *t
	return &cp, nil
}

func (m *MemStore) ListTasks(_ context.Context, status *model.TaskStatus, agentID int64, page, pageSize int) ([]model.Task, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]model.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if status != nil && t.Status != *status {
			continue
		}
		if agentID > 0 && t.AgentID != agentID {
			continue
		}
		all = append(all, *t)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Priority != all[j].Priority {
			return all[i].Priority > all[j].Priority
		}
		return all[i].ID > all[j].ID
	})
	total := len(all)
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	lo := (page - 1) * pageSize
	if lo > total {
		lo = total
	}
	hi := lo + pageSize
	if hi > total {
		hi = total
	}
	return all[lo:hi], total, nil
}

func (m *MemStore) UpdateTask(_ context.Context, id int64, p TaskPatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return fmt.Errorf("task %d: %w", id, ErrNotFound)
	}
	if p.Title != nil {
		t.Title = *p.Title
	}
	if p.Prompt != nil {
		t.Prompt = *p.Prompt
	}
	if p.AgentID != nil {
		t.AgentID = *p.AgentID
	}
	if p.Priority != nil {
		t.Priority = *p.Priority
	}
	if p.Status != nil {
		t.Status = *p.Status
	}
	if p.Result != nil {
		t.Result = *p.Result
	}
	if p.Error != nil {
		t.Error = *p.Error
	}
	if p.WorkspaceID != nil {
		t.WorkspaceID = *p.WorkspaceID
	}
	if p.SkillIDs != nil {
		t.SkillIDs = *p.SkillIDs
	}
	if p.SteerNote != nil {
		t.SteerNote = *p.SteerNote
	}
	if p.DependsOn != nil {
		t.DependsOn = *p.DependsOn
	}
	if p.HandoffCount != nil {
		t.HandoffCount = *p.HandoffCount
	}
	if p.RetryCount != nil {
		t.RetryCount = *p.RetryCount
	}
	if p.VisibleAfter != nil {
		t.VisibleAfter = *p.VisibleAfter
	}
	if p.StartedAt != nil {
		t.StartedAt = p.StartedAt
	}
	if p.FinishedAt != nil {
		t.FinishedAt = p.FinishedAt
	}
	return nil
}

// ClaimNextTask 内存版认领：锁内选优先级最高的可执行任务并置 running。
func (m *MemStore) ClaimNextTask(_ context.Context) (*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	// I10：先收集全部可行候选，策略只负责「候选集中选谁」——闸门仍由存储统一保证
	var candidates []*model.Task
	for _, t := range m.tasks {
		if t.Status != model.TaskQueued {
			continue
		}
		if t.VisibleAfter.After(now) {
			continue
		}
		a, ok := m.agents[t.AgentID]
		if !ok || !a.Enabled {
			continue
		}
		blocked := false
		for _, dep := range t.DependsOn {
			if d, ok := m.tasks[dep]; !ok || d.Status != model.TaskDone {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		if running := m.runningFor(t.AgentID); running >= a.MaxConcurrency {
			continue
		}
		candidates = append(candidates, t)
	}
	best := m.claimStrategy(candidates)
	if best == nil {
		return nil, nil
	}
	best.Status = model.TaskRunning
	now2 := now
	best.StartedAt = &now2
	cp := *best
	return &cp, nil
}

func (m *MemStore) runningFor(agentID int64) int {
	n := 0
	for _, t := range m.tasks {
		if t.AgentID == agentID && t.Status == model.TaskRunning {
			n++
		}
	}
	return n
}

// ---- runs & steps ----

func (m *MemStore) CreateRun(_ context.Context, r *model.Run) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ID = m.id()
	r.StartedAt = time.Now()
	r.Status = model.RunRunning
	cp := *r
	m.runs[r.ID] = &cp
	return r.ID, nil
}

func (m *MemStore) UpdateRun(_ context.Context, r *model.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[r.ID]; !ok {
		return fmt.Errorf("run %d: %w", r.ID, ErrNotFound)
	}
	cp := *r
	m.runs[r.ID] = &cp
	return nil
}

func (m *MemStore) GetRun(_ context.Context, id int64) (*model.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, fmt.Errorf("run %d: %w", id, ErrNotFound)
	}
	cp := *r
	cp.Steps = append([]model.RunStep(nil), m.steps[id]...)
	return &cp, nil
}

func (m *MemStore) ListRuns(_ context.Context, taskID int64) ([]model.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Run{}
	for _, r := range m.runs {
		if r.TaskID == taskID {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (m *MemStore) AppendStep(_ context.Context, runID int64, s *model.RunStep) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	s.Seq = int64(len(m.steps[runID]) + 1)
	s.CreatedAt = time.Now()
	m.steps[runID] = append(m.steps[runID], *s)
	if r, ok := m.runs[runID]; ok {
		r.TotalTokens += s.Tokens
	}
	return s.Seq, nil
}

func (m *MemStore) ListSteps(_ context.Context, runID int64) ([]model.RunStep, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]model.RunStep(nil), m.steps[runID]...), nil
}

// ---- conversations ----

func (m *MemStore) CreateConversation(_ context.Context, c *model.Conversation) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.ID = m.id()
	c.CreatedAt = time.Now()
	cp := *c
	m.convos[c.ID] = &cp
	return c.ID, nil
}

func (m *MemStore) GetConversation(_ context.Context, id int64) (*model.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.convos[id]
	if !ok {
		return nil, fmt.Errorf("conversation %d: %w", id, ErrNotFound)
	}
	cp := *c
	return &cp, nil
}

func (m *MemStore) AppendMessage(_ context.Context, msg *model.Message) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg.ID = m.id()
	msg.CreatedAt = time.Now()
	m.msgs[msg.ConversationID] = append(m.msgs[msg.ConversationID], *msg)
	return msg.ID, nil
}

func (m *MemStore) ListConversations(_ context.Context, limit int) ([]model.ConversationSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.ConversationSummary{}
	for _, c := range m.convos {
		agentName := ""
		if a, ok := m.agents[c.AgentID]; ok {
			agentName = a.Name
		}
		runCount := 0
		for _, r := range m.runs {
			if r.Conversation == c.ID {
				runCount++
			}
		}
		out = append(out, model.ConversationSummary{
			ID: c.ID, AgentID: c.AgentID, AgentName: agentName, Title: c.Title,
			MessageCount: len(m.msgs[c.ID]), RunCount: runCount, CreatedAt: c.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemStore) ListMessages(_ context.Context, conversationID int64) ([]model.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]model.Message(nil), m.msgs[conversationID]...), nil
}

// ---- usage ----

func (m *MemStore) AppendUsage(_ context.Context, u *model.UsageEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	u.ID = m.nextID
	u.CreatedAt = time.Now()
	m.usage = append(m.usage, *u)
	return nil
}

func (m *MemStore) UsageSummary(_ context.Context, window time.Duration) (model.UsageEvent, []model.UsageEvent, []model.UsageEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cutoff := time.Now().Add(-window)
	byModel := map[string]*model.UsageEvent{}
	byAgent := map[int64]*model.UsageEvent{}
	var total model.UsageEvent
	for _, u := range m.usage {
		if u.CreatedAt.Before(cutoff) {
			continue
		}
		total.Requests += u.Requests
		total.PromptTokens += u.PromptTokens
		total.CompletionTokens += u.CompletionTokens
		if bm, ok := byModel[u.Model]; ok {
			bm.Requests += u.Requests
			bm.PromptTokens += u.PromptTokens
			bm.CompletionTokens += u.CompletionTokens
		} else {
			cp := u
			byModel[u.Model] = &cp
		}
		if ba, ok := byAgent[u.AgentID]; ok {
			ba.Requests += u.Requests
			ba.PromptTokens += u.PromptTokens
			ba.CompletionTokens += u.CompletionTokens
		} else {
			cp := u
			byAgent[u.AgentID] = &cp
		}
	}
	return total, usageToSlice(byModel), usageToSlice(byAgent), nil
}

func usageToSlice[M comparable](mm map[M]*model.UsageEvent) []model.UsageEvent {
	out := make([]model.UsageEvent, 0, len(mm))
	for _, v := range mm {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	return out
}

// ---- evals ----

func (m *MemStore) CreateDataset(_ context.Context, d *model.EvalDataset) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d.ID = m.id()
	d.CreatedAt = time.Now()
	cp := *d
	m.datasets[d.ID] = &cp
	return d.ID, nil
}

func (m *MemStore) ListDatasets(_ context.Context) ([]model.EvalDataset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.EvalDataset, 0, len(m.datasets))
	for _, d := range m.datasets {
		cp := *d
		cp.CaseCount = len(m.cases[d.ID])
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemStore) SaveCases(_ context.Context, datasetID int64, cases []model.EvalCase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range cases {
		m.nextID++
		cases[i].ID = m.nextID
		cases[i].DatasetID = datasetID
	}
	m.cases[datasetID] = cases
	return nil
}

func (m *MemStore) ListCases(_ context.Context, datasetID int64) ([]model.EvalCase, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]model.EvalCase(nil), m.cases[datasetID]...), nil
}

func (m *MemStore) CreateEvalRun(_ context.Context, e *model.EvalRun) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.ID = m.id()
	e.CreatedAt = time.Now()
	e.Status = "running"
	cp := *e
	m.evalRuns[e.ID] = &cp
	return e.ID, nil
}

func (m *MemStore) UpdateEvalRun(_ context.Context, e *model.EvalRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.evalRuns[e.ID]; !ok {
		return fmt.Errorf("eval run %d: %w", e.ID, ErrNotFound)
	}
	cp := *e
	m.evalRuns[e.ID] = &cp
	return nil
}

func (m *MemStore) GetEvalRun(_ context.Context, id int64) (*model.EvalRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.evalRuns[id]
	if !ok {
		return nil, fmt.Errorf("eval run %d: %w", id, ErrNotFound)
	}
	cp := *e
	return &cp, nil
}

func (m *MemStore) ListEvalRuns(_ context.Context) ([]model.EvalRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.EvalRun, 0, len(m.evalRuns))
	for _, e := range m.evalRuns {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// ---- events ----

func (m *MemStore) AppendEvent(_ context.Context, e *model.Event) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastEventSeq++
	e.Seq = m.lastEventSeq
	e.CreatedAt = time.Now()
	m.events = append(m.events, *e)
	if len(m.events) > 5000 {
		m.events = m.events[len(m.events)-5000:]
	}
	return e.Seq, nil
}

func (m *MemStore) EventsSince(_ context.Context, since int64, limit int) ([]model.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Event{}
	for _, e := range m.events {
		if e.Seq > since {
			out = append(out, e)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// SetRunSession 外部 agent 会话 ID 落库（I34：CC --resume 续跑依据）。
func (m *MemStore) SetRunSession(_ context.Context, runID int64, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok {
		return fmt.Errorf("run %d: %w", runID, ErrNotFound)
	}
	r.SessionID = sessionID
	return nil
}

// ---- webhooks（I7）----

func (m *MemStore) CreateWebhook(_ context.Context, w *model.Webhook) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	w.ID = m.nextID
	if w.Events == nil {
		w.Events = []string{"*"}
	}
	w.CreatedAt = time.Now()
	cp := *w
	m.webhooks[w.ID] = &cp
	return w.ID, nil
}

func (m *MemStore) ListWebhooks(_ context.Context) ([]model.Webhook, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Webhook{}
	for _, w := range m.webhooks {
		out = append(out, *w)
	}
	// ID 升序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// SetWebhookStatus 投递观测落库（I7 第 2 轮：last_status/last_delivery_at）。
func (m *MemStore) SetWebhookStatus(_ context.Context, id int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.webhooks[id]
	if !ok {
		return fmt.Errorf("webhook %d: %w", id, ErrNotFound)
	}
	now := time.Now()
	w.LastStatus = status
	w.LastDeliveryAt = &now
	return nil
}

func (m *MemStore) DeleteWebhook(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.webhooks[id]; !ok {
		return fmt.Errorf("webhook %d: %w", id, ErrNotFound)
	}
	delete(m.webhooks, id)
	return nil
}
