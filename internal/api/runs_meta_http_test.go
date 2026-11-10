package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// runs/artifacts/models/mcp-servers 观测端点直测（此前 handler 0%）。

func TestRunTraceArtifactsMetaHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "obs", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "观测任务", Prompt: "p", AgentID: aid})
	rid, _ := st.CreateRun(ctx, &model.Run{TaskID: tid, AgentID: aid, Status: model.RunDone, TotalTokens: 42})
	_, _ = st.AppendStep(ctx, rid, &model.RunStep{Kind: model.StepLLM, Name: "decide", LatencyMS: 88, Tokens: 42})
	artID, _ := st.SaveArtifact(ctx, &model.Artifact{TaskID: tid, RunID: rid, Kind: "report",
		Name: "报告.md", MimeType: "text/markdown", Content: "# 报告正文"})

	get := func(path string, setID bool) (int, string) {
		r := httptest.NewRequest("GET", path, nil)
		if setID {
			r.SetPathValue("id", pathIDFrom(path))
		}
		w := httptest.NewRecorder()
		switch {
		case strings.HasPrefix(path, "/api/v1/runs/"):
			s.getRun(w, r)
		case strings.HasPrefix(path, "/api/v1/artifacts/"):
			s.getArtifact(w, r)
		case strings.HasPrefix(path, "/api/v1/artifacts"):
			s.listArtifacts(w, r)
		case path == "/api/v1/models":
			s.models(w, r)
		case path == "/api/v1/mcp/servers":
			s.mcpServers(w, r)
		}
		return w.Code, w.Body.String()
	}

	// run trace：run 字段 + steps 内嵌
	code, body := get("/api/v1/runs/"+itoaInt64(rid), true)
	if code != 200 {
		t.Fatalf("getRun 应 200: %d", code)
	}
	var run model.Run
	_ = json.Unmarshal([]byte(body), &run)
	// AppendStep 会把步 tokens 累计进 run 总量（42 步级 + 42 初始 = 84）
	if run.ID != rid || run.TotalTokens != 84 || len(run.Steps) != 1 || run.Steps[0].Name != "decide" {
		t.Fatalf("run trace 形状不符: id=%d tokens=%d steps=%d", run.ID, run.TotalTokens, len(run.Steps))
	}
	// 不存在的 run → 404
	if code, _ = get("/api/v1/runs/99999", true); code != 404 {
		t.Fatalf("run 404: %d", code)
	}

	// artifacts：列表（默认不带 content）→ 详情（带 content）→ 404
	code, body = get("/api/v1/artifacts?task_id="+itoaInt64(tid), false)
	if code != 200 || !strings.Contains(body, `"total":1`) || strings.Contains(body, "报告正文") {
		t.Fatalf("artifacts 列表形状不符: %d %s", code, body[:min(len(body), 120)])
	}
	code, body = get("/api/v1/artifacts/"+itoaInt64(artID), true)
	if code != 200 || !strings.Contains(body, "报告正文") {
		t.Fatalf("artifact 详情应带 content: %d", code)
	}
	if code, _ = get("/api/v1/artifacts/99999", true); code != 404 {
		t.Fatalf("artifact 404: %d", code)
	}

	// models：动态发现含内置 mock
	code, body = get("/api/v1/models", false)
	if code != 200 || !strings.Contains(body, "mock") {
		t.Fatalf("models 应含 mock: %d", code)
	}
	// mcp/servers：数组形状（无注册 server 时为空）
	code, body = get("/api/v1/mcp/servers", false)
	if code != 200 || !strings.Contains(body, "servers") {
		t.Fatalf("mcp servers 形状不符: %d", code)
	}
}

func pathIDFrom(path string) string {
	i := strings.LastIndex(path, "/")
	return path[i+1:]
}
