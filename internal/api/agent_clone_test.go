package api

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Agent 克隆（I3）：配置复制 + 名称去重 + 全新版本链 + 独立性（改克隆不影响源）。
func TestCloneAgent(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: timeNowTest()}
	ctx := context.Background()

	srcID, _ := st.CreateAgent(ctx, &model.Agent{
		Name: "coder", Role: "开发", SystemPrompt: "源提示词", Model: "qwen3.5",
		Runner: "mock", MaxConcurrency: 2, TokenBudget24h: 5000, Enabled: true})

	req := httptest.NewRequest("POST", "/api/v1/agents/"+strconv.FormatInt(srcID, 10)+"/clone", strings.NewReader(`{}`))
	req.SetPathValue("id", strconv.FormatInt(srcID, 10))
	w := httptest.NewRecorder()
	s.cloneAgent(w, req)
	if w.Code != 201 {
		t.Fatalf("克隆应 201: %d %s", w.Code, w.Body.String())
	}

	agents, _ := st.ListAgents(ctx, store.AgentFilter{})
	var clone *model.Agent
	for i := range agents {
		if agents[i].ID != srcID {
			clone = &agents[i]
		}
	}
	if clone == nil || clone.Name != "coder-clone" {
		t.Fatalf("克隆体缺失或命名错误: %+v", clone)
	}
	if clone.SystemPrompt != "源提示词" || clone.TokenBudget24h != 5000 {
		t.Fatalf("配置未复制: %+v", clone)
	}
	if !clone.Enabled {
		t.Fatal("克隆体应默认启用")
	}

	// 改克隆不影响源（独立性）
	newPrompt := "改过的提示词"
	_ = st.UpdateAgent(ctx, clone.ID, store.AgentPatch{SystemPrompt: &newPrompt})
	src, _ := st.GetAgent(ctx, srcID)
	if src.SystemPrompt != "源提示词" {
		t.Fatal("源配置被克隆体污染")
	}

	// 名称去重：再克隆一次自动加序号
	req2 := httptest.NewRequest("POST", "/api/v1/agents/"+strconv.FormatInt(srcID, 10)+"/clone", strings.NewReader(`{}`))
	req2.SetPathValue("id", strconv.FormatInt(srcID, 10))
	w2 := httptest.NewRecorder()
	s.cloneAgent(w2, req2)
	if !strings.Contains(w2.Body.String(), "coder-clone-2") {
		t.Fatalf("名称去重未生效: %s", w2.Body.String())
	}

	// 克隆体是全新版本链
	versions, _ := st.ListAgentVersions(ctx, clone.ID)
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("克隆体版本链应为 1 条 v1: %+v", versions)
	}
}

func timeNowTest() time.Time { return time.Now() }
