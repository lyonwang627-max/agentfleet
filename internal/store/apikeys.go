package store

import (
	"context"
	"fmt"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- MemStore: api keys ----

func (m *MemStore) CreateAPIKey(_ context.Context, name, keyHash, keyPrefix string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	m.apiKeys[id] = &model.APIKey{ID: id, Name: name, KeyHash: keyHash, KeyPrefix: keyPrefix,
		Scopes: "all", CreatedAt: time.Now()}
	return id, nil
}

func (m *MemStore) ListAPIKeys(_ context.Context) ([]model.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.APIKey, 0, len(m.apiKeys))
	for _, k := range m.apiKeys {
		out = append(out, *k)
	}
	return out, nil
}

func (m *MemStore) FindAPIKeyByHash(_ context.Context, keyHash string) (*model.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, k := range m.apiKeys {
		if k.KeyHash == keyHash {
			cp := *k
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("api key: %w", ErrNotFound)
}

func (m *MemStore) DeleteAPIKey(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.apiKeys[id]; !ok {
		return fmt.Errorf("api key %d: %w", id, ErrNotFound)
	}
	delete(m.apiKeys, id)
	return nil
}

func (m *MemStore) TouchAPIKey(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.apiKeys[id]; ok {
		now := time.Now()
		k.LastUsedAt = &now
	}
	return nil
}

// ---- PostgresStore: api keys ----

func (p *PostgresStore) CreateAPIKey(ctx context.Context, name, keyHash, keyPrefix string) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx,
		`INSERT INTO api_keys(name, key_hash, key_prefix) VALUES ($1,$2,$3) RETURNING id`,
		name, keyHash, keyPrefix).Scan(&id)
	return id, err
}

func (p *PostgresStore) ListAPIKeys(ctx context.Context) ([]model.APIKey, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, name, key_prefix, scopes, last_used_at, created_at FROM api_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.APIKey{}
	for rows.Next() {
		k := model.APIKey{}
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.Scopes, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (p *PostgresStore) FindAPIKeyByHash(ctx context.Context, keyHash string) (*model.APIKey, error) {
	k := &model.APIKey{}
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, key_prefix, scopes, last_used_at, created_at FROM api_keys WHERE key_hash=$1`, keyHash).
		Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.Scopes, &k.LastUsedAt, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (p *PostgresStore) DeleteAPIKey(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM api_keys WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("api key %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) TouchAPIKey(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, id)
	return err
}

func (m *MemStore) HasAnyAPIKey(_ context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.apiKeys) > 0, nil
}

func (p *PostgresStore) HasAnyAPIKey(ctx context.Context) (bool, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&n)
	return n > 0, err
}
