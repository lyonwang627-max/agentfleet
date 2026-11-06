package store

import (
	"context"
	"errors"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 版本链语义（F2）：publish 单调递增；rollback=旧快照覆盖配置+前进新版本（链上可追溯）。
func TestAgentVersionChain(t *testing.T) {
	st := NewMemStore()
	ctx := context.Background()

	aid, err := st.CreateAgent(ctx, &model.Agent{
		Name: "coder", SystemPrompt: "v0 系统提示词", Model: "qwen3.5", Runner: "mock",
		MaxConcurrency: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]any {
		a, _ := st.GetAgent(ctx, aid)
		return map[string]any{"name": a.Name, "role": a.Role, "system_prompt": a.SystemPrompt,
			"model": a.Model, "runner": a.Runner, "max_concurrency": a.MaxConcurrency}
	}
	publish := func(note string) int {
		v := &model.AgentVersion{AgentID: aid, Config: snapshot(), Note: note}
		_, ver, err := st.SaveAgentVersion(ctx, v)
		if err != nil {
			t.Fatalf("publish %q: %v", note, err)
		}
		return ver
	}

	if v := publish("首次发布"); v != 1 {
		t.Fatalf("首次 publish 应为 v1，得到 v%d", v)
	}

	// 修改配置后发布 v2
	newPrompt := "v2 系统提示词"
	if err := st.UpdateAgent(ctx, aid, AgentPatch{SystemPrompt: &newPrompt}); err != nil {
		t.Fatal(err)
	}
	if v := publish("升级提示词"); v != 2 {
		t.Fatalf("第二次 publish 应为 v2，得到 v%d", v)
	}

	// 回滚到 v1：快照覆盖配置
	old, err := st.GetAgentVersion(ctx, aid, 1)
	if err != nil {
		t.Fatal(err)
	}
	prompt := old.Config["system_prompt"].(string)
	if err := st.UpdateAgent(ctx, aid, AgentPatch{SystemPrompt: &prompt}); err != nil {
		t.Fatal(err)
	}
	a3, _ := st.GetAgent(ctx, aid)
	if a3.SystemPrompt != "v0 系统提示词" {
		t.Fatalf("回滚后 system_prompt 应为 v1 快照内容，得到 %q", a3.SystemPrompt)
	}

	// 回滚本身也是新版本 v3（版本链只前进，可追溯）
	if v := publish("rollback from v1"); v != 3 {
		t.Fatalf("rollback publish 应为 v3，得到 v%d", v)
	}
	versions, _ := st.ListAgentVersions(ctx, aid)
	if len(versions) != 3 || versions[0].Version != 3 {
		t.Fatalf("版本链异常: %d 条, 最新 v%d", len(versions), versions[0].Version)
	}

	// 不存在的版本 → ErrNotFound
	if _, err := st.GetAgentVersion(ctx, aid, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("预期 ErrNotFound，得到 %v", err)
	}
}
