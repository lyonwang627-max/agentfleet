package store

import (
	"context"
	"fmt"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- memories（ADR-0003 M1）：agent 长期记忆 CRUD ----
// 写入铁律：抽取路径走 draft（人审 approve 后 active）；手动 API 直写 active。
// 注入铁律：确定性注入（execute 读 active，预算上限由调用方控制）。

func (m *MemStore) CreateMemory(_ context.Context, mem *model.Memory) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	mem.ID = m.nextID
	if mem.Status == "" {
		mem.Status = "draft"
	}
	mem.CreatedAt = time.Now()
	cp := *mem
	m.memories[mem.ID] = &cp
	return mem.ID, nil
}

func (m *MemStore) ListMemories(_ context.Context, agentID int64, statuses []string) ([]model.Memory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Memory{}
	want := map[string]bool{}
	for _, s := range statuses {
		want[s] = true
	}
	for _, mem := range m.memories {
		if mem.AgentID != agentID {
			continue
		}
		if len(want) > 0 && !want[mem.Status] {
			continue
		}
		out = append(out, *mem)
	}
	// ID 升序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func (m *MemStore) ApproveMemory(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem, ok := m.memories[id]
	if !ok {
		return fmt.Errorf("memory %d: %w", id, ErrNotFound)
	}
	if mem.Status != "draft" {
		return fmt.Errorf("memory %d: 仅 draft 可 approve", id)
	}
	mem.Status = "active"
	now := time.Now()
	mem.ApprovedAt = &now
	return nil
}

func (m *MemStore) ExpireMemory(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem, ok := m.memories[id]
	if !ok {
		return fmt.Errorf("memory %d: %w", id, ErrNotFound)
	}
	mem.Status = "expired"
	return nil
}

func (m *MemStore) DeleteMemory(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.memories[id]; !ok {
		return fmt.Errorf("memory %d: %w", id, ErrNotFound)
	}
	delete(m.memories, id)
	return nil
}

// ---- PostgresStore ----

func (p *PostgresStore) CreateMemory(ctx context.Context, mem *model.Memory) (int64, error) {
	if mem.Status == "" {
		mem.Status = "draft"
	}
	err := p.pool.QueryRow(ctx,
		`INSERT INTO memories(agent_id, scope, kind, content, source_run_id, status, approved_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`,
		mem.AgentID, mem.Scope, mem.Kind, mem.Content, mem.SourceRunID, mem.Status, mem.ApprovedAt).
		Scan(&mem.ID, &mem.CreatedAt)
	return mem.ID, err
}

func (p *PostgresStore) ListMemories(ctx context.Context, agentID int64, statuses []string) ([]model.Memory, error) {
	sql := `SELECT id, agent_id, scope, kind, content, COALESCE(source_run_id,0), status, created_at, approved_at FROM memories WHERE agent_id=$1`
	args := []any{agentID}
	if len(statuses) > 0 {
		sql += ` AND status = ANY($2)`
		args = append(args, statuses)
	}
	sql += ` ORDER BY id`
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Memory{}
	for rows.Next() {
		mem := model.Memory{}
		var approved *time.Time
		if err := rows.Scan(&mem.ID, &mem.AgentID, &mem.Scope, &mem.Kind, &mem.Content,
			&mem.SourceRunID, &mem.Status, &mem.CreatedAt, &approved); err != nil {
			return nil, err
		}
		mem.ApprovedAt = approved
		out = append(out, mem)
	}
	return out, rows.Err()
}

func (p *PostgresStore) ApproveMemory(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx,
		`UPDATE memories SET status='active', approved_at=now() WHERE id=$1 AND status='draft'`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("memory %d: 不存在或非 draft", id)
	}
	return nil
}

func (p *PostgresStore) ExpireMemory(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE memories SET status='expired' WHERE id=$1`, id)
	return err
}

func (p *PostgresStore) DeleteMemory(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM memories WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("memory %d: %w", id, ErrNotFound)
	}
	return nil
}
