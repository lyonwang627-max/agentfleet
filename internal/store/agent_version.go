package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- MemStore: agent versions ----

func (m *MemStore) SaveAgentVersion(_ context.Context, v *model.AgentVersion) (int64, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	max := 0
	for _, av := range m.agentVersions {
		if av.AgentID == v.AgentID && av.Version > max {
			max = av.Version
		}
	}
	v.Version = max + 1
	m.nextID++
	v.ID = m.nextID
	v.CreatedAt = time.Now()
	cp := *v
	m.agentVersions[v.ID] = &cp
	return v.ID, v.Version, nil
}

func (m *MemStore) ListAgentVersions(_ context.Context, agentID int64) ([]model.AgentVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.AgentVersion{}
	for _, av := range m.agentVersions {
		if av.AgentID == agentID {
			out = append(out, *av)
		}
	}
	// 降序（最新在前）
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Version > out[i].Version {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (m *MemStore) GetAgentVersion(_ context.Context, agentID int64, version int) (*model.AgentVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, av := range m.agentVersions {
		if av.AgentID == agentID && av.Version == version {
			cp := *av
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("agent %d version %d: %w", agentID, version, ErrNotFound)
}

// ---- PostgresStore: agent versions ----

func (p *PostgresStore) SaveAgentVersion(ctx context.Context, v *model.AgentVersion) (int64, int, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO agent_versions(agent_id, version, config, note)
		 VALUES ($1, (SELECT COALESCE(MAX(version),0)+1 FROM agent_versions WHERE agent_id=$2), $3, $4)
		 RETURNING id, version`,
		v.AgentID, v.AgentID, jsonb(v.Config), v.Note).Scan(&v.ID, &v.Version)
	return v.ID, v.Version, err
}

func (p *PostgresStore) ListAgentVersions(ctx context.Context, agentID int64) ([]model.AgentVersion, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, agent_id, version, config, note, created_at FROM agent_versions
		 WHERE agent_id=$1 ORDER BY version DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AgentVersion{}
	for rows.Next() {
		av := model.AgentVersion{}
		var cfg []byte
		if err := rows.Scan(&av.ID, &av.AgentID, &av.Version, &cfg, &av.Note, &av.CreatedAt); err != nil {
			return nil, err
		}
		av.Config = map[string]any{}
		unjsonb(cfg, &av.Config)
		out = append(out, av)
	}
	return out, rows.Err()
}

func (p *PostgresStore) GetAgentVersion(ctx context.Context, agentID int64, version int) (*model.AgentVersion, error) {
	av := &model.AgentVersion{}
	var cfg []byte
	err := p.pool.QueryRow(ctx,
		`SELECT id, agent_id, version, config, note, created_at FROM agent_versions
		 WHERE agent_id=$1 AND version=$2`, agentID, version).
		Scan(&av.ID, &av.AgentID, &av.Version, &cfg, &av.Note, &av.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("agent %d version %d: %w", agentID, version, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	av.Config = map[string]any{}
	unjsonb(cfg, &av.Config)
	return av, nil
}
