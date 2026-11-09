package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- Projects + TaskComments：MemStore ----

func (m *MemStore) CreateProject(_ context.Context, pr *model.Project) (int64, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.nextID++
	pr.ID = m.nextID
	pr.CreatedAt = time.Now()
	if pr.Repos == nil {
		pr.Repos = []string{}
	}
	cp := *pr
	m.projects[pr.ID] = &cp
	return pr.ID, nil
}

func (m *MemStore) GetProject(_ context.Context, id int64) (*model.Project, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	pr, ok := m.projects[id]
	if !ok {
		return nil, fmt.Errorf("project %d: %w", id, ErrNotFound)
	}
	cp := *pr
	return &cp, nil
}

func (m *MemStore) ListProjects(_ context.Context) ([]model.Project, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	out := make([]model.Project, 0, len(m.projects))
	for _, pr := range m.projects {
		out = append(out, *pr)
	}
	return out, nil
}

func (m *MemStore) DeleteProject(_ context.Context, id int64) error {
	m.mu.Lock(); defer m.mu.Unlock()
	if _, ok := m.projects[id]; !ok {
		return fmt.Errorf("project %d: %w", id, ErrNotFound)
	}
	delete(m.projects, id)
	return nil
}

func (m *MemStore) AddTaskComment(_ context.Context, c *model.TaskComment) (int64, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.nextID++
	c.ID = m.nextID
	c.CreatedAt = time.Now()
	cp := *c
	m.taskComments[c.ID] = &cp
	return c.ID, nil
}

func (m *MemStore) ListTaskComments(_ context.Context, taskID int64) ([]model.TaskComment, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	out := []model.TaskComment{}
	for _, c := range m.taskComments {
		if c.TaskID == taskID {
			out = append(out, *c)
		}
	}
	return out, nil
}

// ---- Projects + TaskComments：PostgresStore ----

func (p *PostgresStore) CreateProject(ctx context.Context, pr *model.Project) (int64, error) {
	if pr.Repos == nil {
		pr.Repos = []string{}
	}
	err := p.pool.QueryRow(ctx,
		`INSERT INTO projects(name, description, repos) VALUES ($1,$2,$3) RETURNING id, created_at`,
		pr.Name, pr.Description, jsonb(pr.Repos)).Scan(&pr.ID, &pr.CreatedAt)
	return pr.ID, err
}

func (p *PostgresStore) GetProject(ctx context.Context, id int64) (*model.Project, error) {
	pr := &model.Project{}
	var repos []byte
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, description, repos, created_at FROM projects WHERE id=$1`, id).
		Scan(&pr.ID, &pr.Name, &pr.Description, &repos, &pr.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("project %d: %w", id, ErrNotFound)
	}
	pr.Repos = []string{}
	unjsonb(repos, &pr.Repos)
	return pr, err
}

func (p *PostgresStore) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, name, description, repos, created_at FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		pr := model.Project{}
		var repos []byte
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Description, &repos, &pr.CreatedAt); err != nil {
			return nil, err
		}
		pr.Repos = []string{}
		unjsonb(repos, &pr.Repos)
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (p *PostgresStore) DeleteProject(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("project %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) AddTaskComment(ctx context.Context, c *model.TaskComment) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO task_comments(task_id, author_type, author_name, content) VALUES ($1,$2,$3,$4)
		 RETURNING id, created_at`, c.TaskID, c.AuthorType, c.AuthorName, c.Content).
		Scan(&c.ID, &c.CreatedAt)
	return c.ID, err
}

func (p *PostgresStore) ListTaskComments(ctx context.Context, taskID int64) ([]model.TaskComment, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, task_id, author_type, author_name, content, created_at
		 FROM task_comments WHERE task_id=$1 ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TaskComment{}
	for rows.Next() {
		c := model.TaskComment{}
		if err := rows.Scan(&c.ID, &c.TaskID, &c.AuthorType, &c.AuthorName, &c.Content, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
