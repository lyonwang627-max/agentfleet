package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runtime"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Workspace Runtime（I16 第 1 轮）：Local 探测（目录/分支）+ health 端点 + 创建即探测。

func TestLocalRuntimeProbe(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// 缺目录 → degraded
	st, note := runtime.Local{}.Probe(ctx, &model.Workspace{RootPath: filepath.Join(dir, "nope")})
	if st != runtime.StatusDegraded || !strings.Contains(note, "不存在") {
		t.Fatalf("缺目录应 degraded: %v %q", st, note)
	}

	// 目录在、无 .git → ready（分支校验跳过）
	st, _ = runtime.Local{}.Probe(ctx, &model.Workspace{RootPath: dir})
	if st != runtime.StatusReady {
		t.Fatalf("纯目录应 ready: %v", st)
	}

	// .git 存在 + 分支不匹配 → degraded
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, note = runtime.Local{}.Probe(ctx, &model.Workspace{RootPath: dir, Branch: "release-hotfix"})
	if st != runtime.StatusDegraded || !strings.Contains(note, "分支") {
		t.Fatalf("分支不匹配应 degraded: %v %q", st, note)
	}
}

func TestWorkspaceHealthEndpoint(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	ctx := context.Background()

	// 真实临时目录 + .git（分支匹配由 git 探测——临时目录非 git 仓库，用无分支场景）
	dir := t.TempDir()
	id, _ := st.CreateWorkspace(ctx, &model.Workspace{Name: "ws", RootPath: dir, Status: "active"})

	r := httptest.NewRequest("GET", "/api/v1/workspaces/"+itoaInt64(id)+"/health", nil)
	r.SetPathValue("id", itoaInt64(id))
	w := httptest.NewRecorder()
	s.workspaceHealth(w, r)
	if w.Code != 200 {
		t.Fatalf("health 应 200: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		RuntimeStatus string `json:"runtime_status"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.RuntimeStatus != string(runtime.StatusReady) {
		t.Fatalf("纯目录应 ready: %q", out.RuntimeStatus)
	}
	// 探测结果落库
	ws2, _ := st.GetWorkspace(ctx, id)
	if ws2.RuntimeStatus != string(runtime.StatusReady) {
		t.Fatalf("状态未落库: %q", ws2.RuntimeStatus)
	}

	// 404
	r2 := httptest.NewRequest("GET", "/api/v1/workspaces/99999/health", nil)
	r2.SetPathValue("id", "99999")
	w2 := httptest.NewRecorder()
	s.workspaceHealth(w2, r2)
	if w2.Code != 404 {
		t.Fatalf("不存在应 404: %d", w2.Code)
	}
}

func TestCreateWorkspaceProbesImmediately(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: nil, StartedAt: time.Now()}
	// 缺失目录 → 创建响应即带 degraded
	r := httptest.NewRequest("POST", "/api/v1/workspaces",
		strings.NewReader(`{"name":"ws","root_path":"/nonexistent/path/xyz"}`))
	w := httptest.NewRecorder()
	s.createWorkspace(w, r)
	if w.Code != 201 {
		t.Fatalf("创建应 201: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "degraded") {
		t.Fatalf("创建响应应带探测结果: %s", w.Body.String())
	}
	wsList, _ := st.ListWorkspaces(context.Background(), false)
	if len(wsList) != 1 || wsList[0].RuntimeStatus != string(runtime.StatusDegraded) {
		t.Fatalf("探测状态未落库: %+v", wsList)
	}
}

func TestMockRuntimeAlwaysReady(t *testing.T) {
	st, _ := runtime.Mock{}.Probe(context.Background(), &model.Workspace{})
	if st != runtime.StatusReady {
		t.Fatalf("mock 应恒 ready: %v", st)
	}
}

// ---- 第 2 轮：Docker 运行时 + EnsureReady 拒绝 ----

func TestDockerRuntimeProbe(t *testing.T) {
	ctx := context.Background()
	// 空 root_path → degraded
	st, note := runtime.Docker{}.Probe(ctx, &model.Workspace{})
	if st != runtime.StatusDegraded || !strings.Contains(note, "容器名") {
		t.Fatalf("空容器名应 degraded: %v %q", st, note)
	}
	// 本机 docker 行为不确定（daemon 可能不可用）——只断言状态 ∈ 合法集且 note 非空
	st2, note2 := runtime.Docker{}.Probe(ctx, &model.Workspace{RootPath: "definitely-not-a-container-xyz"})
	switch st2 {
	case runtime.StatusReady, runtime.StatusDegraded:
	default:
		t.Fatalf("状态越界: %v", st2)
	}
	if note2 == "" {
		t.Fatal("note 应非空")
	}
}
