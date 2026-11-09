package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lyonwang/agentfleet/internal/model"
)

// I14 草稿/发布双态（store 双实现）：
// - UpdateAgentDraft：PATCH 只进草稿（不存在则从 live 快照初始化后合并）
// - PublishAgentDraft：草稿落线 + 清空草稿（nil=无草稿=与线上一致）
// - ClearAgentDraft：回滚等场景清空草稿（回退覆盖 live 后回到干净态）

// draftFromAgent live 配置快照（键名与 snapshotConfig/api patchAgent 的 JSON 键一致）。
func draftFromAgent(a *model.Agent) map[string]any {
	return map[string]any{
		"name": a.Name, "role": a.Role, "system_prompt": a.SystemPrompt, "model": a.Model,
		"tools": a.Tools, "runner": a.Runner, "max_concurrency": a.MaxConcurrency,
		"token_budget_24h": a.TokenBudget24h,
	}
}

// applyPatchToDraft 把类型化 patch 合并进草稿 map（只写出现的键）。
func applyPatchToDraft(d map[string]any, p AgentPatch) {
	if p.Name != nil {
		d["name"] = *p.Name
	}
	if p.Role != nil {
		d["role"] = *p.Role
	}
	if p.SystemPrompt != nil {
		d["system_prompt"] = *p.SystemPrompt
	}
	if p.Model != nil {
		d["model"] = *p.Model
	}
	if p.Tools != nil {
		d["tools"] = *p.Tools
	}
	if p.Runner != nil {
		d["runner"] = *p.Runner
	}
	if p.MaxConcurrency != nil {
		d["max_concurrency"] = *p.MaxConcurrency
	}
	if p.TokenBudget24h != nil {
		d["token_budget_24h"] = *p.TokenBudget24h
	}
}

// patchFromDraft 草稿 map → 类型化 patch（publish 落线用；缺失键不覆盖）。
func patchFromDraft(d map[string]any) AgentPatch {
	p := AgentPatch{}
	if v, ok := d["name"].(string); ok {
		p.Name = &v
	}
	if v, ok := d["role"].(string); ok {
		p.Role = &v
	}
	if v, ok := d["system_prompt"].(string); ok {
		p.SystemPrompt = &v
	}
	if v, ok := d["model"].(string); ok {
		p.Model = &v
	}
	if v, ok := d["runner"].(string); ok {
		p.Runner = &v
	}
	if v, ok := d["max_concurrency"].(float64); ok {
		n := int(v)
		p.MaxConcurrency = &n
	}
	if v, ok := d["token_budget_24h"].(float64); ok {
		n := int(v)
		p.TokenBudget24h = &n
	}
	if raw, ok := d["tools"].([]any); ok {
		tools := []string{}
		for _, t := range raw {
			if ts, ok := t.(string); ok {
				tools = append(tools, ts)
			}
		}
		p.Tools = &tools
	}
	return p
}

// ---- MemStore ----

func (m *MemStore) UpdateAgentDraft(_ context.Context, id int64, patch AgentPatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	if a.DraftConfig == nil {
		a.DraftConfig = draftFromAgent(a)
	}
	applyPatchToDraft(a.DraftConfig, patch)
	return nil
}

func (m *MemStore) PublishAgentDraft(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	if a.DraftConfig == nil {
		return nil // 无草稿：已同步，无需落线
	}
	if err := m.applyAgentPatch(a, patchFromDraft(a.DraftConfig)); err != nil {
		return err
	}
	a.DraftConfig = nil
	return nil
}

func (m *MemStore) ClearAgentDraft(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	a.DraftConfig = nil
	return nil
}

// ---- PostgresStore ----

func (p *PostgresStore) UpdateAgentDraft(ctx context.Context, id int64, patch AgentPatch) error {
	a, err := p.GetAgent(ctx, id)
	if err != nil {
		return err
	}
	draft := a.DraftConfig
	if draft == nil {
		draft = draftFromAgent(a)
	}
	applyPatchToDraft(draft, patch)
	raw, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `UPDATE agents SET draft_config=$1::jsonb, updated_at=now() WHERE id=$2`, raw, id)
	return err
}

func (p *PostgresStore) PublishAgentDraft(ctx context.Context, id int64) error {
	a, err := p.GetAgent(ctx, id)
	if err != nil {
		return err
	}
	if a.DraftConfig == nil {
		return nil
	}
	if err := p.UpdateAgent(ctx, id, patchFromDraft(a.DraftConfig)); err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `UPDATE agents SET draft_config=NULL, updated_at=now() WHERE id=$1`, id)
	return err
}

func (p *PostgresStore) ClearAgentDraft(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE agents SET draft_config=NULL, updated_at=now() WHERE id=$1`, id)
	return err
}
