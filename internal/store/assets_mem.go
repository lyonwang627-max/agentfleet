package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- MemStore: artifacts / workspaces / skills ----

func (m *MemStore) SaveArtifact(_ context.Context, a *model.Artifact) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID = m.id()
	a.CreatedAt = time.Now()
	a.Size = len(a.Content)
	cp := *a
	m.artifacts[a.ID] = &cp
	return a.ID, nil
}

func (m *MemStore) ListArtifacts(_ context.Context, taskID int64, withContent bool) ([]model.Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Artifact{}
	for _, a := range m.artifacts {
		if taskID == 0 || a.TaskID == taskID {
			cp := *a
			if !withContent {
				cp.Content = ""
			}
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (m *MemStore) GetArtifact(_ context.Context, id int64) (*model.Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.artifacts[id]
	if !ok {
		return nil, fmt.Errorf("artifact %d: %w", id, ErrNotFound)
	}
	cp := *a
	return &cp, nil
}

func (m *MemStore) CreateWorkspace(_ context.Context, w *model.Workspace) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w.Status == "" {
		w.Status = "active"
	}
	w.ID = m.id()
	w.CreatedAt = time.Now()
	cp := *w
	m.workspaces[w.ID] = &cp
	return w.ID, nil
}

func (m *MemStore) GetWorkspace(_ context.Context, id int64) (*model.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workspaces[id]
	if !ok {
		return nil, fmt.Errorf("workspace %d: %w", id, ErrNotFound)
	}
	cp := *w
	return &cp, nil
}

func (m *MemStore) ListWorkspaces(_ context.Context, activeOnly bool) ([]model.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Workspace{}
	for _, w := range m.workspaces {
		if activeOnly && w.Status != "active" {
			continue
		}
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemStore) UpdateWorkspace(_ context.Context, id int64, p WorkspacePatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workspaces[id]
	if !ok {
		return fmt.Errorf("workspace %d: %w", id, ErrNotFound)
	}
	if p.Name != nil {
		w.Name = *p.Name
	}
	if p.Branch != nil {
		w.Branch = *p.Branch
	}
	if p.Note != nil {
		w.Note = *p.Note
	}
	if p.Status != nil {
		w.Status = *p.Status
	}
	return nil
}

func (m *MemStore) DeleteWorkspace(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[id]; !ok {
		return fmt.Errorf("workspace %d: %w", id, ErrNotFound)
	}
	delete(m.workspaces, id)
	return nil
}

func (m *MemStore) SaveSkill(_ context.Context, s *model.Skill) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == 0 {
		s.ID = m.id()
		s.CreatedAt = time.Now()
		s.UsageCount = 0
	}
	s.UpdatedAt = time.Now()
	if s.Tags == nil {
		s.Tags = []string{}
	}
	cp := *s
	m.skills[s.ID] = &cp
	return s.ID, nil
}

func (m *MemStore) GetSkill(_ context.Context, id int64) (*model.Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill %d: %w", id, ErrNotFound)
	}
	cp := *s
	return &cp, nil
}

func (m *MemStore) ListSkills(_ context.Context, tag string) ([]model.Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.Skill{}
	for _, s := range m.skills {
		if tag == "" || hasTag(s.Tags, tag) {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UsageCount > out[j].UsageCount })
	return out, nil
}

func (m *MemStore) UpdateSkill(_ context.Context, s *model.Skill) error {
	return m.saveSkillLocked(s)
}

func (m *MemStore) saveSkillLocked(s *model.Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[s.ID]; !ok {
		return fmt.Errorf("skill %d: %w", s.ID, ErrNotFound)
	}
	s.UpdatedAt = time.Now()
	cp := *s
	m.skills[s.ID] = &cp
	return nil
}

func (m *MemStore) DeleteSkill(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[id]; !ok {
		return fmt.Errorf("skill %d: %w", id, ErrNotFound)
	}
	delete(m.skills, id)
	return nil
}

func (m *MemStore) IncrSkillUsage(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.skills[id]
	if !ok {
		return fmt.Errorf("skill %d: %w", id, ErrNotFound)
	}
	s.UsageCount++
	return nil
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}
