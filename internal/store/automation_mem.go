package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- MemStore: automations ----

func (m *MemStore) CreateAutomation(_ context.Context, a *model.Automation) (int64, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	a.ID = m.id(); a.CreatedAt = time.Now()
	cp := *a; m.automations[a.ID] = &cp
	return a.ID, nil
}

func (m *MemStore) GetAutomation(_ context.Context, id int64) (*model.Automation, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	a, ok := m.automations[id]
	if !ok {
		return nil, fmt.Errorf("automation %d: %w", id, ErrNotFound)
	}
	cp := *a
	return &cp, nil
}

func (m *MemStore) ListAutomations(_ context.Context, enabledOnly bool) ([]model.Automation, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	out := []model.Automation{}
	for _, a := range m.automations {
		if enabledOnly && !a.Enabled {
			continue
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemStore) UpdateAutomation(_ context.Context, id int64, p AutomationPatch) error {
	m.mu.Lock(); defer m.mu.Unlock()
	a, ok := m.automations[id]
	if !ok {
		return fmt.Errorf("automation %d: %w", id, ErrNotFound)
	}
	if p.AgentID != nil { a.AgentID = *p.AgentID }
	if p.Name != nil { a.Name = *p.Name }
	if p.CronExpr != nil { a.CronExpr = *p.CronExpr }
	if p.Title != nil { a.Title = *p.Title }
	if p.Prompt != nil { a.Prompt = *p.Prompt }
	if p.Priority != nil { a.Priority = *p.Priority }
	if p.Enabled != nil { a.Enabled = *p.Enabled }
	return nil
}

func (m *MemStore) DeleteAutomation(_ context.Context, id int64) error {
	m.mu.Lock(); defer m.mu.Unlock()
	if _, ok := m.automations[id]; !ok {
		return fmt.Errorf("automation %d: %w", id, ErrNotFound)
	}
	delete(m.automations, id)
	return nil
}

func (m *MemStore) MarkAutomationRan(_ context.Context, id int64, lastRun, nextRun time.Time) error {
	m.mu.Lock(); defer m.mu.Unlock()
	a, ok := m.automations[id]
	if !ok {
		return fmt.Errorf("automation %d: %w", id, ErrNotFound)
	}
	a.LastRun, a.NextRun = &lastRun, &nextRun
	return nil
}
