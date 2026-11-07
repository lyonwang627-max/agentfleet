package store

import (
	"context"
	"sort"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- MemStore: usage by purpose ----

func (m *MemStore) UsageByPurpose(_ context.Context, window time.Duration) ([]model.UsageEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cutoff := time.Now().Add(-window)
	byPurpose := map[string]*model.UsageEvent{}
	for _, u := range m.usage {
		if u.CreatedAt.Before(cutoff) {
			continue
		}
		if bp, ok := byPurpose[u.Purpose]; ok {
			bp.Requests += u.Requests
			bp.PromptTokens += u.PromptTokens
			bp.CompletionTokens += u.CompletionTokens
		} else {
			cp := u
			byPurpose[u.Purpose] = &cp
		}
	}
	out := make([]model.UsageEvent, 0, len(byPurpose))
	for _, v := range byPurpose {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	return out, nil
}

// ---- PostgresStore: usage by purpose ----

func (p *PostgresStore) UsageByPurpose(ctx context.Context, window time.Duration) ([]model.UsageEvent, error) {
	cutoff := time.Now().Add(-window)
	rows, err := p.pool.Query(ctx,
		`SELECT purpose, COALESCE(SUM(requests),0), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
		 FROM usage_events WHERE created_at >= $1 GROUP BY purpose ORDER BY 2 DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.UsageEvent{}
	for rows.Next() {
		u := model.UsageEvent{}
		if err := rows.Scan(&u.Purpose, &u.Requests, &u.PromptTokens, &u.CompletionTokens); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
