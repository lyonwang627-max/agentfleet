package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- PostgresStore: automations ----

func (p *PostgresStore) CreateAutomation(ctx context.Context, a *model.Automation) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO automations(agent_id, name, cron_expr, title, prompt, priority, enabled, next_run)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at`,
		a.AgentID, a.Name, a.CronExpr, a.Title, a.Prompt, a.Priority, a.Enabled, a.NextRun,
	).Scan(&a.ID, &a.CreatedAt)
	return a.ID, err
}

const automationCols = `a.id, a.agent_id, COALESCE(ag.name,''), a.name, a.cron_expr, a.title, a.prompt,
	a.priority, a.enabled, a.last_run, a.next_run, a.created_at`

func scanAutomation(row pgx.Row) (*model.Automation, error) {
	a := &model.Automation{}
	if err := row.Scan(&a.ID, &a.AgentID, &a.AgentName, &a.Name, &a.CronExpr, &a.Title, &a.Prompt,
		&a.Priority, &a.Enabled, &a.LastRun, &a.NextRun, &a.CreatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

func (p *PostgresStore) GetAutomation(ctx context.Context, id int64) (*model.Automation, error) {
	a, err := scanAutomation(p.pool.QueryRow(ctx,
		`SELECT `+automationCols+` FROM automations a LEFT JOIN agents ag ON ag.id=a.agent_id WHERE a.id=$1`, id))
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("automation %d: %w", id, ErrNotFound)
	}
	return a, err
}

func (p *PostgresStore) ListAutomations(ctx context.Context, enabledOnly bool) ([]model.Automation, error) {
	sql := `SELECT ` + automationCols + ` FROM automations a LEFT JOIN agents ag ON ag.id=a.agent_id`
	if enabledOnly {
		sql += ` WHERE a.enabled`
	}
	sql += ` ORDER BY a.id`
	rows, err := p.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Automation{}
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpdateAutomation(ctx context.Context, id int64, patch AutomationPatch) error {
	sets, args := []string{}, []any{id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("a.%s=$%d", col, len(args)))
	}
	if patch.AgentID != nil {
		add("agent_id", *patch.AgentID)
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.CronExpr != nil {
		add("cron_expr", *patch.CronExpr)
	}
	if patch.Title != nil {
		add("title", *patch.Title)
	}
	if patch.Prompt != nil {
		add("prompt", *patch.Prompt)
	}
	if patch.Priority != nil {
		add("priority", *patch.Priority)
	}
	if patch.Enabled != nil {
		add("enabled", *patch.Enabled)
	}
	if len(sets) == 0 {
		return nil
	}
	ct, err := p.pool.Exec(ctx, `UPDATE automations a SET `+join(sets)+` WHERE a.id=$1`, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("automation %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) DeleteAutomation(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM automations WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("automation %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) MarkAutomationRan(ctx context.Context, id int64, lastRun, nextRun time.Time) error {
	_, err := p.pool.Exec(ctx, `UPDATE automations SET last_run=$2, next_run=$3 WHERE id=$1`, id, lastRun, nextRun)
	return err
}
