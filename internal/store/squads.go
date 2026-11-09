package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- MemStore: squads ----

func (m *MemStore) CreateSquad(_ context.Context, sq *model.Squad) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	sq.ID = id
	sq.CreatedAt = time.Now()
	cp := *sq
	m.squads[id] = &cp
	return id, nil
}

func (m *MemStore) GetSquad(_ context.Context, id int64) (*model.Squad, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sq, ok := m.squads[id]
	if !ok {
		return nil, fmt.Errorf("squad %d: %w", id, ErrNotFound)
	}
	cp := *sq
	return &cp, nil
}

func (m *MemStore) ListSquads(_ context.Context) ([]model.Squad, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Squad, 0, len(m.squads))
	for _, sq := range m.squads {
		out = append(out, *sq)
	}
	return out, nil
}

func (m *MemStore) UpdateSquad(_ context.Context, id int64, p SquadPatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sq, ok := m.squads[id]
	if !ok {
		return fmt.Errorf("squad %d: %w", id, ErrNotFound)
	}
	if p.Name != nil {
		sq.Name = *p.Name
	}
	if p.Description != nil {
		sq.Description = *p.Description
	}
	if p.LeaderID != nil {
		sq.LeaderID = *p.LeaderID
	}
	if p.Instructions != nil {
		sq.Instructions = *p.Instructions
	}
	return nil
}

func (m *MemStore) DeleteSquad(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.squads[id]; !ok {
		return fmt.Errorf("squad %d: %w", id, ErrNotFound)
	}
	delete(m.squads, id)
	for k, mem := range m.squadMembers {
		if mem.SquadID == id {
			delete(m.squadMembers, k)
		}
	}
	return nil
}

func (m *MemStore) AddSquadMember(_ context.Context, mem *model.SquadMember) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.squadMembers {
		if existing.SquadID == mem.SquadID && existing.MemberType == mem.MemberType && existing.MemberID == mem.MemberID {
			return nil // 幂等
		}
	}
	m.nextID++
	mem.ID = m.nextID
	cp := *mem
	m.squadMembers[mem.ID] = &cp
	return nil
}

func (m *MemStore) RemoveSquadMember(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.squadMembers[id]; !ok {
		return fmt.Errorf("squad member %d: %w", id, ErrNotFound)
	}
	delete(m.squadMembers, id)
	return nil
}

func (m *MemStore) ListSquadMembers(_ context.Context, squadID int64) ([]model.SquadMember, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.SquadMember{}
	for _, mem := range m.squadMembers {
		if mem.SquadID == squadID {
			out = append(out, *mem)
		}
	}
	return out, nil
}

// ---- PostgresStore: squads ----

func (p *PostgresStore) CreateSquad(ctx context.Context, sq *model.Squad) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO squads(name, description, leader_id, instructions) VALUES ($1,$2,$3,$4)
		 RETURNING id, created_at`, sq.Name, sq.Description, sq.LeaderID, sq.Instructions).
		Scan(&sq.ID, &sq.CreatedAt)
	return sq.ID, err
}

func (p *PostgresStore) GetSquad(ctx context.Context, id int64) (*model.Squad, error) {
	sq := &model.Squad{}
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, description, leader_id, instructions, created_at FROM squads WHERE id=$1`, id).
		Scan(&sq.ID, &sq.Name, &sq.Description, &sq.LeaderID, &sq.Instructions, &sq.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("squad %d: %w", id, ErrNotFound)
	}
	return sq, err
}

func (p *PostgresStore) ListSquads(ctx context.Context) ([]model.Squad, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, name, description, leader_id, instructions, created_at FROM squads ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Squad{}
	for rows.Next() {
		sq := model.Squad{}
		if err := rows.Scan(&sq.ID, &sq.Name, &sq.Description, &sq.LeaderID, &sq.Instructions, &sq.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sq)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpdateSquad(ctx context.Context, id int64, patch SquadPatch) error {
	sets, args := []string{}, []any{id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s=$%d", col, len(args)))
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Description != nil {
		add("description", *patch.Description)
	}
	if patch.LeaderID != nil {
		add("leader_id", *patch.LeaderID)
	}
	if patch.Instructions != nil {
		add("instructions", *patch.Instructions)
	}
	if len(sets) == 0 {
		return nil
	}
	ct, err := p.pool.Exec(ctx, `UPDATE squads SET `+join(sets)+` WHERE id=$1`, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("squad %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) DeleteSquad(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM squads WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("squad %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) AddSquadMember(ctx context.Context, mem *model.SquadMember) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO squad_members(squad_id, member_type, member_id, role) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (squad_id, member_type, member_id) DO NOTHING`, mem.SquadID, mem.MemberType, mem.MemberID, mem.Role)
	return err
}

func (p *PostgresStore) RemoveSquadMember(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM squad_members WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("squad member %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) ListSquadMembers(ctx context.Context, squadID int64) ([]model.SquadMember, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, squad_id, member_type, member_id, role FROM squad_members WHERE squad_id=$1 ORDER BY id`, squadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SquadMember{}
	for rows.Next() {
		mem := model.SquadMember{}
		if err := rows.Scan(&mem.ID, &mem.SquadID, &mem.MemberType, &mem.MemberID, &mem.Role); err != nil {
			return nil, err
		}
		out = append(out, mem)
	}
	return out, rows.Err()
}
