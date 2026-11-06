package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lyonwang/agentfleet/internal/model"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := pool.Exec(ctx, SchemaSQL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *PostgresStore) Close() error                   { p.pool.Close(); return nil }

func jsonb(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func unjsonb(b []byte, dst any) {
	if len(b) > 0 {
		_ = json.Unmarshal(b, dst)
	}
}

// ---- agents ----

func (p *PostgresStore) CreateAgent(ctx context.Context, a *model.Agent) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO agents(name, role, system_prompt, model, tools, runner, max_concurrency, token_budget_24h, enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at, updated_at`,
		a.Name, a.Role, a.SystemPrompt, a.Model, jsonb(a.Tools), a.Runner, a.MaxConcurrency, a.TokenBudget24h, a.Enabled,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	return a.ID, err
}

const agentCols = `id, name, role, system_prompt, model, tools, runner, max_concurrency, token_budget_24h, enabled, created_at, updated_at`

func scanAgent(row pgx.Row) (*model.Agent, error) {
	a := &model.Agent{}
	var tools []byte
	if err := row.Scan(&a.ID, &a.Name, &a.Role, &a.SystemPrompt, &a.Model, &tools, &a.Runner,
		&a.MaxConcurrency, &a.TokenBudget24h, &a.Enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.Tools = []string{}
	unjsonb(tools, &a.Tools)
	return a, nil
}

// AgentTokensSince 24h 窗口内某 agent 的 token 合计（预算熔断判定）。
func (p *PostgresStore) AgentTokensSince(ctx context.Context, agentID int64, since time.Time) (int64, error) {
	var sum int64
	err := p.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(prompt_tokens + completion_tokens),0) FROM usage_events
		 WHERE agent_id=$1 AND created_at >= $2`, agentID, since).Scan(&sum)
	return sum, err
}

func (p *PostgresStore) GetAgent(ctx context.Context, id int64) (*model.Agent, error) {
	a, err := scanAgent(p.pool.QueryRow(ctx, `SELECT `+agentCols+` FROM agents WHERE id=$1`, id))
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	return a, err
}

func (p *PostgresStore) ListAgents(ctx context.Context, f AgentFilter) ([]model.Agent, error) {
	sql := `SELECT ` + agentCols + ` FROM agents`
	if f.EnabledOnly {
		sql += ` WHERE enabled`
	}
	sql += ` ORDER BY id`
	rows, err := p.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpdateAgent(ctx context.Context, id int64, patch AgentPatch) error {
	sets, args := []string{}, []any{id}
	add := func(col string, v any) { args = append(args, v); sets = append(sets, fmt.Sprintf("%s=$%d", col, len(args))) }
	if patch.Name != nil { add("name", *patch.Name) }
	if patch.Role != nil { add("role", *patch.Role) }
	if patch.SystemPrompt != nil { add("system_prompt", *patch.SystemPrompt) }
	if patch.Model != nil { add("model", *patch.Model) }
	if patch.Tools != nil { add("tools", jsonb(*patch.Tools)) }
	if patch.Runner != nil { add("runner", *patch.Runner) }
	if patch.MaxConcurrency != nil { add("max_concurrency", *patch.MaxConcurrency) }
	if patch.TokenBudget24h != nil { add("token_budget_24h", *patch.TokenBudget24h) }
	if patch.Enabled != nil { add("enabled", *patch.Enabled) }
	if len(sets) == 0 {
		return nil
	}
	args = append(args, time.Now())
	sets = append(sets, fmt.Sprintf("updated_at=$%d", len(args)))
	ct, err := p.pool.Exec(ctx, `UPDATE agents SET `+join(sets)+` WHERE id=$1`, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	return nil
}

func join(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += ", "
		}
		s += p
	}
	return s
}

// ---- tasks ----

func (p *PostgresStore) CreateTask(ctx context.Context, t *model.Task) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO tasks(title, prompt, agent_id, priority, status, depends_on, max_handoffs, max_retries, visible_after)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`,
		t.Title, t.Prompt, t.AgentID, t.Priority, taskStatusOr(t.Status), jsonb(t.DependsOn),
		t.MaxHandoffs, t.MaxRetries, t.VisibleAfter,
	).Scan(&t.ID, &t.CreatedAt)
	return t.ID, err
}

func taskStatusOr(s model.TaskStatus) model.TaskStatus {
	if s == "" {
		return model.TaskQueued
	}
	return s
}

const taskCols = `id, title, prompt, agent_id, priority, status, depends_on, handoff_count, max_handoffs,
	retry_count, max_retries, visible_after, result, error, created_at, started_at, finished_at`

func scanTask(row pgx.Row) (*model.Task, error) {
	t := &model.Task{}
	var deps []byte
	if err := row.Scan(&t.ID, &t.Title, &t.Prompt, &t.AgentID, &t.Priority, &t.Status, &deps,
		&t.HandoffCount, &t.MaxHandoffs, &t.RetryCount, &t.MaxRetries, &t.VisibleAfter,
		&t.Result, &t.Error, &t.CreatedAt, &t.StartedAt, &t.FinishedAt); err != nil {
		return nil, err
	}
	t.DependsOn = []int64{}
	unjsonb(deps, &t.DependsOn)
	return t, nil
}

// 注意：BIGINT[] 走 pq.Array 风格不可用（未引 lib/pq），这里用 JSONB 存 depends_on。
// schema 里 depends_on 是 BIGINT[]，为统一改用 JSONB：见 schema.sql。

func (p *PostgresStore) GetTask(ctx context.Context, id int64) (*model.Task, error) {
	t, err := scanTask(p.pool.QueryRow(ctx, `SELECT `+taskCols+` FROM tasks WHERE id=$1`, id))
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("task %d: %w", id, ErrNotFound)
	}
	return t, err
}

func (p *PostgresStore) ListTasks(ctx context.Context, status *model.TaskStatus, agentID int64, page, pageSize int) ([]model.Task, int, error) {
	where, args := " WHERE 1=1", []any{}
	if status != nil {
		args = append(args, string(*status))
		where += fmt.Sprintf(" AND status=$%d", len(args))
	}
	if agentID > 0 {
		args = append(args, agentID)
		where += fmt.Sprintf(" AND agent_id=$%d", len(args))
	}
	var total int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	args = append(args, pageSize, (page-1)*pageSize)
	sql := `SELECT ` + taskCols + ` FROM tasks` + where +
		fmt.Sprintf(" ORDER BY priority DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

func (p *PostgresStore) UpdateTask(ctx context.Context, id int64, patch TaskPatch) error {
	sets, args := []string{}, []any{id}
	add := func(col string, v any) { args = append(args, v); sets = append(sets, fmt.Sprintf("%s=$%d", col, len(args))) }
	if patch.Title != nil { add("title", *patch.Title) }
	if patch.Prompt != nil { add("prompt", *patch.Prompt) }
	if patch.AgentID != nil { add("agent_id", *patch.AgentID) }
	if patch.Priority != nil { add("priority", *patch.Priority) }
	if patch.Status != nil { add("status", string(*patch.Status)) }
	if patch.Result != nil { add("result", *patch.Result) }
	if patch.Error != nil { add("error", *patch.Error) }
	if patch.HandoffCount != nil { add("handoff_count", *patch.HandoffCount) }
	if patch.RetryCount != nil { add("retry_count", *patch.RetryCount) }
	if patch.VisibleAfter != nil { add("visible_after", *patch.VisibleAfter) }
	if patch.StartedAt != nil { add("started_at", *patch.StartedAt) }
	if patch.FinishedAt != nil { add("finished_at", *patch.FinishedAt) }
	if len(sets) == 0 {
		return nil
	}
	ct, err := p.pool.Exec(ctx, `UPDATE tasks SET `+join(sets)+` WHERE id=$1`, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("task %d: %w", id, ErrNotFound)
	}
	return nil
}

// ClaimNextTask 生产级认领：单条事务内 FOR UPDATE SKIP LOCKED 抢占，天然支持多实例横向扩展。
// 依赖满足、agent 启用、并发配额、visible_after 全部在 SQL 内判定。
func (p *PostgresStore) ClaimNextTask(ctx context.Context) (*model.Task, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT t.id FROM tasks t
			JOIN agents a ON a.id = t.agent_id
			WHERE t.status = 'queued'
			  AND t.visible_after <= now()
			  AND a.enabled
			  AND NOT EXISTS (
				SELECT 1 FROM tasks d
				WHERE d.id = ANY(t.depends_on) AND d.status <> 'done'
			  )
			  AND (SELECT COUNT(*) FROM tasks r
				   WHERE r.agent_id = t.agent_id AND r.status = 'running') < a.max_concurrency
			ORDER BY t.priority DESC, t.id
			FOR UPDATE OF t SKIP LOCKED
			LIMIT 1
		)
		UPDATE tasks SET status='running', started_at=now()
		WHERE id = (SELECT id FROM candidate)
		RETURNING `+taskCols)
	t, err := scanTask(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, tx.Commit(ctx)
}

// ---- runs & steps ----

func (p *PostgresStore) CreateRun(ctx context.Context, r *model.Run) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO runs(task_id, agent_id, conversation_id) VALUES ($1,$2,$3) RETURNING id, started_at`,
		r.TaskID, r.AgentID, r.Conversation,
	).Scan(&r.ID, &r.StartedAt)
	r.Status = model.RunRunning
	return r.ID, err
}

func (p *PostgresStore) UpdateRun(ctx context.Context, r *model.Run) error {
	ct, err := p.pool.Exec(ctx,
		`UPDATE runs SET status=$2, total_tokens=$3, cost_usd=$4, error=$5, finished_at=$6 WHERE id=$1`,
		r.ID, string(r.Status), r.TotalTokens, r.CostUSD, r.Error, r.FinishedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("run %d: %w", r.ID, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) GetRun(ctx context.Context, id int64) (*model.Run, error) {
	r := &model.Run{}
	var fin *time.Time
	err := p.pool.QueryRow(ctx,
		`SELECT id, task_id, agent_id, COALESCE(conversation_id,0), status, total_tokens, cost_usd, error, started_at, finished_at
		 FROM runs WHERE id=$1`, id,
	).Scan(&r.ID, &r.TaskID, &r.AgentID, &r.Conversation, &r.Status, &r.TotalTokens, &r.CostUSD, &r.Error, &r.StartedAt, &fin)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("run %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	r.FinishedAt = fin
	r.Steps, err = p.ListSteps(ctx, id)
	return r, err
}

func (p *PostgresStore) ListRuns(ctx context.Context, taskID int64) ([]model.Run, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, task_id, agent_id, COALESCE(conversation_id,0), status, total_tokens, cost_usd, error, started_at, finished_at
		 FROM runs WHERE task_id=$1 ORDER BY id DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		r := model.Run{}
		var fin *time.Time
		if err := rows.Scan(&r.ID, &r.TaskID, &r.AgentID, &r.Conversation, &r.Status,
			&r.TotalTokens, &r.CostUSD, &r.Error, &r.StartedAt, &fin); err != nil {
			return nil, err
		}
		r.FinishedAt = fin
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresStore) AppendStep(ctx context.Context, runID int64, s *model.RunStep) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO run_steps(run_id, kind, name, input, output, note, latency_ms, tokens)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING seq, created_at`,
		runID, string(s.Kind), s.Name, jsonb(s.Input), jsonb(s.Output), s.Note, s.LatencyMS, s.Tokens,
	).Scan(&s.Seq, &s.CreatedAt)
	if err == nil {
		_, _ = p.pool.Exec(ctx, `UPDATE runs SET total_tokens = total_tokens + $2 WHERE id=$1`, runID, s.Tokens)
	}
	return s.Seq, err
}

func (p *PostgresStore) ListSteps(ctx context.Context, runID int64) ([]model.RunStep, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT seq, kind, name, COALESCE(input,'{}'::jsonb), COALESCE(output,'{}'::jsonb), note, latency_ms, tokens, created_at
		 FROM run_steps WHERE run_id=$1 ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RunStep{}
	for rows.Next() {
		s := model.RunStep{}
		var input, output []byte
		if err := rows.Scan(&s.Seq, &s.Kind, &s.Name, &input, &output, &s.Note, &s.LatencyMS, &s.Tokens, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.Input = map[string]any{}; unjsonb(input, &s.Input)
		s.Output = map[string]any{}; unjsonb(output, &s.Output)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- conversations ----

func (p *PostgresStore) CreateConversation(ctx context.Context, c *model.Conversation) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO conversations(agent_id, title) VALUES ($1,$2) RETURNING id, created_at`,
		c.AgentID, c.Title).Scan(&c.ID, &c.CreatedAt)
	return c.ID, err
}

func (p *PostgresStore) GetConversation(ctx context.Context, id int64) (*model.Conversation, error) {
	c := &model.Conversation{}
	err := p.pool.QueryRow(ctx, `SELECT id, agent_id, title, created_at FROM conversations WHERE id=$1`, id).
		Scan(&c.ID, &c.AgentID, &c.Title, &c.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("conversation %d: %w", id, ErrNotFound)
	}
	return c, err
}

func (p *PostgresStore) AppendMessage(ctx context.Context, m *model.Message) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO messages(conversation_id, role, content) VALUES ($1,$2,$3) RETURNING id, created_at`,
		m.ConversationID, m.Role, m.Content).Scan(&m.ID, &m.CreatedAt)
	return m.ID, err
}

func (p *PostgresStore) ListMessages(ctx context.Context, conversationID int64) ([]model.Message, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, conversation_id, role, content, created_at FROM messages WHERE conversation_id=$1 ORDER BY id`,
		conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Message{}
	for rows.Next() {
		m := model.Message{}
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---- usage ----

func (p *PostgresStore) AppendUsage(ctx context.Context, u *model.UsageEvent) error {
	return p.pool.QueryRow(ctx,
		`INSERT INTO usage_events(agent_id, agent_name, model, prompt_tokens, completion_tokens, requests)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
		u.AgentID, u.AgentName, u.Model, u.PromptTokens, u.CompletionTokens, u.Requests).Scan(&u.ID, &u.CreatedAt)
}

func (p *PostgresStore) UsageSummary(ctx context.Context, window time.Duration) (model.UsageEvent, []model.UsageEvent, []model.UsageEvent, error) {
	cutoff := time.Now().Add(-window)
	var total model.UsageEvent
	err := p.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(requests),0), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
		 FROM usage_events WHERE created_at >= $1`, cutoff).
		Scan(&total.Requests, &total.PromptTokens, &total.CompletionTokens)
	if err != nil {
		return total, nil, nil, err
	}
	byModel, err := p.usageGroup(ctx, cutoff, `model`)
	if err != nil {
		return total, nil, nil, err
	}
	byAgent, err := p.usageGroup(ctx, cutoff, `agent_id, agent_name`)
	if err != nil {
		return total, nil, nil, err
	}
	return total, byModel, byAgent, nil
}

func (p *PostgresStore) usageGroup(ctx context.Context, cutoff time.Time, group string) ([]model.UsageEvent, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+group+`, COALESCE(SUM(requests),0), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
		 FROM usage_events WHERE created_at >= $1 GROUP BY `+group+` ORDER BY 2 DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.UsageEvent{}
	for rows.Next() {
		u := model.UsageEvent{}
		var dst []any
		if group == "model" {
			dst = []any{&u.Model}
		} else {
			dst = []any{&u.AgentID, &u.AgentName}
		}
		if err := rows.Scan(append(dst, &u.Requests, &u.PromptTokens, &u.CompletionTokens)...); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---- evals ----

func (p *PostgresStore) CreateDataset(ctx context.Context, d *model.EvalDataset) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO eval_datasets(name, description) VALUES ($1,$2) RETURNING id, created_at`,
		d.Name, d.Description).Scan(&d.ID, &d.CreatedAt)
	return d.ID, err
}

func (p *PostgresStore) ListDatasets(ctx context.Context) ([]model.EvalDataset, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT d.id, d.name, d.description, d.created_at, (SELECT COUNT(*) FROM eval_cases c WHERE c.dataset_id=d.id)
		 FROM eval_datasets d ORDER BY d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvalDataset{}
	for rows.Next() {
		d := model.EvalDataset{}
		if err := rows.Scan(&d.ID, &d.Name, &d.Description, &d.CreatedAt, &d.CaseCount); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (p *PostgresStore) SaveCases(ctx context.Context, datasetID int64, cases []model.EvalCase) error {
	batch := &pgx.Batch{}
	for _, c := range cases {
		batch.Queue(`INSERT INTO eval_cases(dataset_id, input, expected) VALUES ($1,$2,$3)`,
			datasetID, c.Input, jsonb(c.Expected))
	}
	return p.pool.SendBatch(ctx, batch).Close()
}

func (p *PostgresStore) ListCases(ctx context.Context, datasetID int64) ([]model.EvalCase, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, dataset_id, input, expected FROM eval_cases WHERE dataset_id=$1 ORDER BY id`, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvalCase{}
	for rows.Next() {
		c := model.EvalCase{}
		var exp []byte
		if err := rows.Scan(&c.ID, &c.DatasetID, &c.Input, &exp); err != nil {
			return nil, err
		}
		c.Expected = map[string]any{}; unjsonb(exp, &c.Expected)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *PostgresStore) CreateEvalRun(ctx context.Context, e *model.EvalRun) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO eval_runs(dataset_id, agent_id, status) VALUES ($1,$2,$3) RETURNING id, created_at`,
		e.DatasetID, e.AgentID, e.Status).Scan(&e.ID, &e.CreatedAt)
	return e.ID, err
}

func (p *PostgresStore) UpdateEvalRun(ctx context.Context, e *model.EvalRun) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE eval_runs SET status=$2, score_avg=$3, pass_rate=$4, report=$5 WHERE id=$1`,
		e.ID, e.Status, e.ScoreAvg, e.PassRate, jsonb(e.Report))
	return err
}

func (p *PostgresStore) GetEvalRun(ctx context.Context, id int64) (*model.EvalRun, error) {
	e := &model.EvalRun{}
	var report []byte
	err := p.pool.QueryRow(ctx,
		`SELECT id, dataset_id, agent_id, status, score_avg, pass_rate, COALESCE(report,'null'::jsonb), created_at
		 FROM eval_runs WHERE id=$1`, id).
		Scan(&e.ID, &e.DatasetID, &e.AgentID, &e.Status, &e.ScoreAvg, &e.PassRate, &report, &e.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("eval run %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if len(report) > 0 && string(report) != "null" {
		e.Report = &model.EvalReport{}
		unjsonb(report, e.Report)
	}
	return e, nil
}

func (p *PostgresStore) ListEvalRuns(ctx context.Context) ([]model.EvalRun, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, dataset_id, agent_id, status, score_avg, pass_rate, created_at FROM eval_runs ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvalRun{}
	for rows.Next() {
		e := model.EvalRun{}
		if err := rows.Scan(&e.ID, &e.DatasetID, &e.AgentID, &e.Status, &e.ScoreAvg, &e.PassRate, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- events ----

func (p *PostgresStore) AppendEvent(ctx context.Context, e *model.Event) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO events(kind, data) VALUES ($1,$2) RETURNING seq, created_at`,
		e.Kind, jsonb(e.Data)).Scan(&e.Seq, &e.CreatedAt)
	return e.Seq, err
}

func (p *PostgresStore) EventsSince(ctx context.Context, since int64, limit int) ([]model.Event, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT seq, kind, data, created_at FROM events WHERE seq > $1 ORDER BY seq LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Event{}
	for rows.Next() {
		e := model.Event{}
		var data []byte
		if err := rows.Scan(&e.Seq, &e.Kind, &data, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Data = map[string]any{}; unjsonb(data, &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}
