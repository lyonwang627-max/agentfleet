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

// 列表/CRUD 尾巴端点回归锁（此前 handler 0%）：agents/workspaces 三件/auth keys/
// automations 列表/squad 成员移除。

func TestRemainingListEndpoints(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "la", Model: "mock", Runner: "mock", Enabled: true})
	wid, _ := st.CreateWorkspace(ctx, &model.Workspace{Name: "lw", RootPath: "/tmp", Status: "active"})
	kid, _ := st.CreateAPIKey(ctx, "key-1", "hash-abc", "af_")
	_ = kid
	sq, _ := st.CreateSquad(ctx, &model.Squad{Name: "小队", LeaderID: aid})
	mem := &model.SquadMember{SquadID: sq, MemberType: "agent", MemberID: aid}
	_ = st.AddSquadMember(ctx, mem)
	smid := mem.ID

	// listAgents：items/total 形状
	wa := httptest.NewRecorder()
	s.listAgents(wa, httptest.NewRequest("GET", "/api/v1/agents", nil))
	if wa.Code != 200 || !strings.Contains(wa.Body.String(), `"total":1`) {
		t.Fatalf("listAgents 形状不符: %d %s", wa.Code, wa.Body.String()[:min(len(wa.Body.String()), 120)])
	}

	// workspaces：list / get / patch（branch 修改可见）
	wl := httptest.NewRecorder()
	s.listWorkspaces(wl, httptest.NewRequest("GET", "/api/v1/workspaces", nil))
	if wl.Code != 200 || !strings.Contains(wl.Body.String(), "lw") {
		t.Fatalf("listWorkspaces 不符: %d", wl.Code)
	}
	rg := httptest.NewRequest("GET", "/api/v1/workspaces/"+itoaInt64(wid), nil)
	rg.SetPathValue("id", itoaInt64(wid))
	wg := httptest.NewRecorder()
	s.getWorkspace(wg, rg)
	if wg.Code != 200 || !strings.Contains(wg.Body.String(), "lw") {
		t.Fatalf("getWorkspace 不符: %d", wg.Code)
	}
	rp := httptest.NewRequest("PATCH", "/api/v1/workspaces/"+itoaInt64(wid),
		strings.NewReader(`{"branch":"feature-x","note":"n1"}`))
	rp.SetPathValue("id", itoaInt64(wid))
	wp := httptest.NewRecorder()
	s.patchWorkspace(wp, rp)
	if wp.Code != 200 {
		t.Fatalf("patchWorkspace 应 200: %d %s", wp.Code, wp.Body.String())
	}
	ws2, _ := st.GetWorkspace(ctx, wid)
	if ws2.Branch != "feature-x" || ws2.Note != "n1" {
		t.Fatalf("patch 未生效: %+v", ws2)
	}

	// listAuthKeys
	wk := httptest.NewRecorder()
	s.listAuthKeys(wk, httptest.NewRequest("GET", "/api/v1/auth/keys", nil))
	if wk.Code != 200 || !strings.Contains(wk.Body.String(), "key-1") {
		t.Fatalf("listAuthKeys 不符: %d %s", wk.Code, wk.Body.String()[:min(len(wk.Body.String()), 120)])
	}

	// listAutomations
	wau := httptest.NewRecorder()
	s.listAutomations(wau, httptest.NewRequest("GET", "/api/v1/automations", nil))
	if wau.Code != 200 || !strings.Contains(wau.Body.String(), `"items"`) {
		t.Fatalf("listAutomations 形状不符: %d", wau.Code)
	}

	// squad 成员移除：存在 → 200；不存在 → 404
	rm := httptest.NewRequest("DELETE", "/api/v1/squads/members/"+itoaInt64(smid), nil)
	rm.SetPathValue("id", itoaInt64(smid))
	wr := httptest.NewRecorder()
	s.removeSquadMember(wr, rm)
	if wr.Code != 200 {
		t.Fatalf("移除成员应 200: %d %s", wr.Code, wr.Body.String())
	}
	rm2 := httptest.NewRequest("DELETE", "/api/v1/squads/members/99999", nil)
	rm2.SetPathValue("id", "99999")
	wr2 := httptest.NewRecorder()
	s.removeSquadMember(wr2, rm2)
	if wr2.Code != 404 {
		t.Fatalf("不存在成员应 404: %d", wr2.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(wr.Body.Bytes(), &resp)
	if resp["removed"] != true {
		t.Fatalf("removed 标缺失: %v", resp)
	}
}

// hasLabelLocal 纯函数三分支（标签过滤底层）。
func TestHasLabelLocal(t *testing.T) {
	labels := []string{"sev1", "ops"}
	if !hasLabelLocal(labels, "sev1") {
		t.Fatal("含标签应 true")
	}
	if hasLabelLocal(labels, "missing") {
		t.Fatal("不含应 false")
	}
	if hasLabelLocal(nil, "sev1") {
		t.Fatal("空列表应 false")
	}
}
