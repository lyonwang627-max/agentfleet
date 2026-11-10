package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- PostgresStore: artifacts / workspaces / skills ----

func (p *PostgresStore) SaveArtifact(ctx context.Context, a *model.Artifact) (int64, error) {
	a.Size = len(a.Content)
	err := p.pool.QueryRow(ctx,
		`INSERT INTO artifacts(task_id, run_id, kind, name, mime_type, size, content)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`,
		a.TaskID, a.RunID, a.Kind, a.Name, a.MimeType, a.Size, a.Content,
	).Scan(&a.ID, &a.CreatedAt)
	return a.ID, err
}

func (p *PostgresStore) ListArtifacts(ctx context.Context, taskID int64, withContent bool) ([]model.Artifact, error) {
	sql := `SELECT id, task_id, run_id, kind, name, mime_type, size`
	if withContent {
		sql += `, content`
	} else {
		sql += `, ''`
	}
	sql += `, created_at FROM artifacts`
	args := []any{}
	if taskID > 0 {
		sql += ` WHERE task_id=$1`
		args = append(args, taskID)
	}
	sql += ` ORDER BY id DESC LIMIT 200`
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Artifact{}
	for rows.Next() {
		a := model.Artifact{}
		if err := rows.Scan(&a.ID, &a.TaskID, &a.RunID, &a.Kind, &a.Name, &a.MimeType, &a.Size, &a.Content, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *PostgresStore) GetArtifact(ctx context.Context, id int64) (*model.Artifact, error) {
	a := &model.Artifact{}
	err := p.pool.QueryRow(ctx,
		`SELECT id, task_id, run_id, kind, name, mime_type, size, content, created_at FROM artifacts WHERE id=$1`, id).
		Scan(&a.ID, &a.TaskID, &a.RunID, &a.Kind, &a.Name, &a.MimeType, &a.Size, &a.Content, &a.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("artifact %d: %w", id, ErrNotFound)
	}
	return a, err
}

func (p *PostgresStore) CreateWorkspace(ctx context.Context, w *model.Workspace) (int64, error) {
	if w.Status == "" {
		w.Status = "active"
	}
	err := p.pool.QueryRow(ctx,
		`INSERT INTO workspaces(name, root_path, branch, note, status) VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, created_at`, w.Name, w.RootPath, w.Branch, w.Note, w.Status).
		Scan(&w.ID, &w.CreatedAt)
	return w.ID, err
}

func (p *PostgresStore) GetWorkspace(ctx context.Context, id int64) (*model.Workspace, error) {
	w := &model.Workspace{}
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, root_path, branch, note, status, created_at, COALESCE(runtime_status,''), COALESCE(runtime_note,'') FROM workspaces WHERE id=$1`, id).
		Scan(&w.ID, &w.Name, &w.RootPath, &w.Branch, &w.Note, &w.Status, &w.CreatedAt, &w.RuntimeStatus, &w.RuntimeNote)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("workspace %d: %w", id, ErrNotFound)
	}
	return w, err
}

func (p *PostgresStore) ListWorkspaces(ctx context.Context, activeOnly bool) ([]model.Workspace, error) {
	sql := `SELECT id, name, root_path, branch, note, status, created_at, COALESCE(runtime_status,''), COALESCE(runtime_note,'') FROM workspaces`
	if activeOnly {
		sql += ` WHERE status='active'`
	}
	sql += ` ORDER BY id`
	rows, err := p.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Workspace{}
	for rows.Next() {
		w := model.Workspace{}
		if err := rows.Scan(&w.ID, &w.Name, &w.RootPath, &w.Branch, &w.Note, &w.Status, &w.CreatedAt, &w.RuntimeStatus, &w.RuntimeNote); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpdateWorkspace(ctx context.Context, id int64, patch WorkspacePatch) error {
	sets, args := []string{}, []any{id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s=$%d", col, len(args)))
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Branch != nil {
		add("branch", *patch.Branch)
	}
	if patch.Note != nil {
		add("note", *patch.Note)
	}
	if patch.Status != nil {
		add("status", *patch.Status)
	}
	if patch.RuntimeStatus != nil {
		add("runtime_status", *patch.RuntimeStatus)
	}
	if patch.RuntimeNote != nil {
		add("runtime_note", *patch.RuntimeNote)
	}
	if len(sets) == 0 {
		return nil
	}
	ct, err := p.pool.Exec(ctx, `UPDATE workspaces SET `+join(sets)+` WHERE id=$1`, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("workspace %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) DeleteWorkspace(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("workspace %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) SaveSkill(ctx context.Context, s *model.Skill) (int64, error) {
	if s.Tags == nil {
		s.Tags = []string{}
	}
	if s.ID == 0 {
		err := p.pool.QueryRow(ctx,
			`INSERT INTO skills(name, content, tags, source_task) VALUES ($1,$2,$3,$4)
			 RETURNING id, created_at, updated_at`, s.Name, s.Content, jsonb(s.Tags), s.SourceTask).
			Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
		return s.ID, err
	}
	_, err := p.pool.Exec(ctx,
		`UPDATE skills SET name=$2, content=$3, tags=$4, updated_at=now() WHERE id=$1`,
		s.ID, s.Name, s.Content, jsonb(s.Tags))
	return s.ID, err
}

func scanSkillRow(row pgx.Row) (*model.Skill, error) {
	s := &model.Skill{}
	var tags []byte
	if err := row.Scan(&s.ID, &s.Name, &s.Content, &tags, &s.UsageCount, &s.SourceTask, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.Tags = []string{}
	unjsonb(tags, &s.Tags)
	return s, nil
}

const skillCols = `id, name, content, tags, usage_count, source_task, created_at, updated_at`

func (p *PostgresStore) GetSkill(ctx context.Context, id int64) (*model.Skill, error) {
	s, err := scanSkillRow(p.pool.QueryRow(ctx, `SELECT `+skillCols+` FROM skills WHERE id=$1`, id))
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("skill %d: %w", id, ErrNotFound)
	}
	return s, err
}

func (p *PostgresStore) ListSkills(ctx context.Context, tag string) ([]model.Skill, error) {
	sql := `SELECT ` + skillCols + ` FROM skills`
	args := []any{}
	if tag != "" {
		sql += ` WHERE tags @> $1::jsonb`
		args = append(args, jsonb([]string{tag}))
	}
	sql += ` ORDER BY usage_count DESC, id DESC LIMIT 200`
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Skill{}
	for rows.Next() {
		s, err := scanSkillRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpdateSkill(ctx context.Context, s *model.Skill) error {
	return p.SaveSkillCtxUpdate(ctx, s)
}

func (p *PostgresStore) SaveSkillCtxUpdate(ctx context.Context, s *model.Skill) error {
	if s.Tags == nil {
		s.Tags = []string{}
	}
	ct, err := p.pool.Exec(ctx,
		`UPDATE skills SET name=$2, content=$3, tags=$4, updated_at=now() WHERE id=$1`,
		s.ID, s.Name, s.Content, jsonb(s.Tags))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("skill %d: %w", s.ID, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) DeleteSkill(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM skills WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("skill %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) IncrSkillUsage(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE skills SET usage_count = usage_count + 1 WHERE id=$1`, id)
	return err
}
