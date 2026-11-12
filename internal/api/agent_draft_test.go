package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 草稿/发布双态（I14 第一轮）：PATCH 只进草稿，publish 落线+版本快照，rollback 清草稿。

func newDraftServer(t *testing.T) (*Server, *store.MemStore, int64) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	aid, err := st.CreateAgent(ctx, &model.Agent{Name: "worker", Role: "执行", Model: "mock", Runner: "mock",
		SystemPrompt: "原始提示词", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return s, st, aid
}

func patchJSON(s *Server, id int64, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PATCH", "/api/v1/agents/"+itoaInt64(id), strings.NewReader(body))
	r.SetPathValue("id", itoaInt64(id)) // 直调 handler 绕过 mux，PathValue 需手动注入
	w := httptest.NewRecorder()
	s.patchAgent(w, r)
	return w
}

func getAgentJSON(s *Server, id int64) map[string]any {
	r := httptest.NewRequest("GET", "/api/v1/agents/"+itoaInt64(id), nil)
	r.SetPathValue("id", itoaInt64(id))
	w := httptest.NewRecorder()
	s.getAgent(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return m
}

func TestPatchOnlyTouchesDraft(t *testing.T) {
	s, st, aid := newDraftServer(t)
	ctx := context.Background()

	w := patchJSON(s, aid, `{"system_prompt":"草稿新提示词","model":"other-model"}`)
	if w.Code != 200 {
		t.Fatalf("PATCH 应 200: %d %s", w.Code, w.Body.String())
	}
	// 线上配置不变（编辑不落线——在途 run 免疫）
	a, _ := st.GetAgent(ctx, aid)
	if a.SystemPrompt != "原始提示词" || a.Model != "mock" {
		t.Fatalf("PATCH 不应直接改线上: %q %q", a.SystemPrompt, a.Model)
	}
	// 草稿持有新值 + dirty 标
	if a.DraftConfig == nil || a.DraftConfig["system_prompt"] != "草稿新提示词" {
		t.Fatalf("草稿未合并: %+v", a.DraftConfig)
	}
	out := getAgentJSON(s, aid)
	if out["has_unpublished_changes"] != true {
		t.Fatalf("dirty 标缺失: %v", out["has_unpublished_changes"])
	}
}

func TestPublishDraftGoesLive(t *testing.T) {
	s, st, aid := newDraftServer(t)
	ctx := context.Background()
	_, _ = st.CreateEvalRun(ctx, &model.EvalRun{}) // 无关调用，保持与既有测试一致的 store 状态

	_ = patchJSON(s, aid, `{"system_prompt":"发布版提示词"}`)

	r := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish", strings.NewReader(`{"note":"v2"}`))
	r.SetPathValue("id", itoaInt64(aid))
	w := httptest.NewRecorder()
	s.publishAgent(w, r)
	if w.Code != 201 {
		t.Fatalf("publish 应 201: %d %s", w.Code, w.Body.String())
	}

	a, _ := st.GetAgent(ctx, aid)
	if a.SystemPrompt != "发布版提示词" {
		t.Fatalf("publish 未落线: %q", a.SystemPrompt)
	}
	if a.DraftConfig != nil {
		t.Fatalf("publish 后草稿应清空: %+v", a.DraftConfig)
	}
	out := getAgentJSON(s, aid)
	if out["has_unpublished_changes"] != false {
		t.Fatal("落线后应无未发布变更")
	}
	// 版本链新增一条（F2 语义保留）
	vers, _ := st.ListAgentVersions(ctx, aid)
	if len(vers) != 1 {
		t.Fatalf("版本链应 1 条: %d", len(vers))
	}
}

func TestPublishWithoutDraftSnapshotsOnly(t *testing.T) {
	// 无草稿：publish=纯版本快照（F2 原语义回归锁）
	s, st, aid := newDraftServer(t)
	ctx := context.Background()
	r := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish", nil)
	r.SetPathValue("id", itoaInt64(aid))
	w := httptest.NewRecorder()
	s.publishAgent(w, r)
	if w.Code != 201 {
		t.Fatalf("应 201: %d %s", w.Code, w.Body.String())
	}
	a, _ := st.GetAgent(ctx, aid)
	if a.SystemPrompt != "原始提示词" {
		t.Fatalf("无草稿不应改线上: %q", a.SystemPrompt)
	}
	vers, _ := st.ListAgentVersions(ctx, aid)
	if len(vers) != 1 {
		t.Fatalf("版本快照应 1 条: %d", len(vers))
	}
}

func TestRollbackClearsDraft(t *testing.T) {
	s, st, aid := newDraftServer(t)
	ctx := context.Background()
	_ = patchJSON(s, aid, `{"system_prompt":"悬空草稿"}`)
	_ = patchJSON(s, aid, `{"system_prompt":"发布版提示词"}`)

	// publish（落线 v1 快照）→ 再改草稿 → rollback 到 v1：live 回退且草稿清空
	pub := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish", nil)
	pub.SetPathValue("id", itoaInt64(aid))
	wr := httptest.NewRecorder()
	s.publishAgent(wr, pub)

	_ = patchJSON(s, aid, `{"system_prompt":"悬空草稿"}`)
	rb := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/versions/1/rollback", nil)
	rb.SetPathValue("id", itoaInt64(aid))
	rb.SetPathValue("version", "1")
	wrb := httptest.NewRecorder()
	s.rollbackAgent(wrb, rb)
	if wrb.Code != 200 {
		t.Fatalf("rollback 应 200: %d %s", wrb.Code, wrb.Body.String())
	}
	a, _ := st.GetAgent(ctx, aid)
	if a.SystemPrompt != "发布版提示词" {
		t.Fatalf("回退未生效: %q", a.SystemPrompt)
	}
	if a.DraftConfig != nil {
		t.Fatalf("回退后草稿应清空: %+v", a.DraftConfig)
	}
	out := getAgentJSON(s, aid)
	if out["has_unpublished_changes"] != false {
		t.Fatal("回退后应无未发布变更")
	}
}

// 灰度发布（I38 第 2 轮）：publish 带 ramp_pct → ramping 落库；promote 收敛 → live 更新+清标记。
func TestRampPublishAndPromote(t *testing.T) {
	s, st, aid := newDraftServer(t)
	ctx := context.Background()

	// 发布带 ramp_pct=50 → 版本 v1 + ramping 标记（live 配置不动）
	pr := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish",
		strings.NewReader(`{"note":"灰度发布","ramp_pct":50}`))
	pr.SetPathValue("id", itoaInt64(aid))
	wr := httptest.NewRecorder()
	s.publishAgent(wr, pr)
	if wr.Code != 201 {
		t.Fatalf("灰度发布应 201: %d %s", wr.Code, wr.Body.String())
	}
	a, _ := st.GetAgent(ctx, aid)
	if a.RampingVersion != 1 || a.RampPct != 50 {
		t.Fatalf("ramping 标记未落库: %+v", a)
	}
	// 灰度阶段 live 配置仍是旧值（I14 语义：草稿落线即全量的例外——灰度下 live 不动）
	// 注意：I14 publish 会落线草稿——ramp 发布时草稿为空，live 保持原配置 ✓

	// promote → live 配置更新为 ramping 快照 + 清标记（版本链不膨胀）
	pm := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish",
		strings.NewReader(`{"promote_ramp":true}`))
	pm.SetPathValue("id", itoaInt64(aid))
	wm := httptest.NewRecorder()
	s.publishAgent(wm, pm)
	if wm.Code != 200 {
		t.Fatalf("promote 应 200: %d %s", wm.Code, wm.Body.String())
	}
	a2, _ := st.GetAgent(ctx, aid)
	if a2.RampingVersion != 0 || a2.RampPct != 0 {
		t.Fatalf("promote 未清灰度标记: %+v", a2)
	}
	vers, _ := st.ListAgentVersions(ctx, aid)
	if len(vers) != 1 {
		t.Fatalf("promote 不应产生新版本: %d", len(vers))
	}

	// 无灰度 promote → 409
	wm2 := httptest.NewRecorder()
	pm2 := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish",
		strings.NewReader(`{"promote_ramp":true}`))
	pm2.SetPathValue("id", itoaInt64(aid))
	s.publishAgent(wm2, pm2)
	if wm2.Code != 409 {
		t.Fatalf("无灰度 promote 应 409: %d", wm2.Code)
	}
}

// 灰度语义锁（I38）：ramp 发布时 live 不落线——current 桶保持旧配置（灰度才有意义）。
func TestRampPublishKeepsLive(t *testing.T) {
	s, st, aid := newDraftServer(t)
	ctx := context.Background()
	_ = patchJSON(s, aid, `{"system_prompt":"灰度新词"}`)
	pr := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish",
		strings.NewReader(`{"ramp_pct":100}`))
	pr.SetPathValue("id", itoaInt64(aid))
	wr := httptest.NewRecorder()
	s.publishAgent(wr, pr)
	if wr.Code != 201 {
		t.Fatalf("应 201: %d", wr.Code)
	}
	a, _ := st.GetAgent(ctx, aid)
	if a.SystemPrompt != "原始提示词" {
		t.Fatalf("ramp 发布不应落线 live: %q", a.SystemPrompt)
	}
	if a.RampingVersion != 1 {
		t.Fatalf("ramping 标记缺失: %+v", a)
	}
	// promote → live 落线
	pm := httptest.NewRequest("POST", "/api/v1/agents/"+itoaInt64(aid)+"/publish",
		strings.NewReader(`{"promote_ramp":true}`))
	pm.SetPathValue("id", itoaInt64(aid))
	wm := httptest.NewRecorder()
	s.publishAgent(wm, pm)
	if wm.Code != 200 {
		t.Fatalf("promote 应 200: %d", wm.Code)
	}
	a2, _ := st.GetAgent(ctx, aid)
	if a2.SystemPrompt != "灰度新词" {
		t.Fatalf("promote 后 live 应为新词: %q", a2.SystemPrompt)
	}
}
