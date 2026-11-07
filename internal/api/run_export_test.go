package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// run 报告 Markdown 导出（I23）：头部元数据/步骤表/最终产出/截断。
func TestExportRunMarkdown(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()

	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "worker", Model: "mock", Runner: "mock", Enabled: true})
	taskID, _ := st.CreateTask(ctx, &model.Task{Title: "导出测试任务", Prompt: "普通任务，直接完成。", AgentID: aid})
	runID, _ := st.CreateRun(ctx, &model.Run{TaskID: taskID, AgentID: aid})
	_, _ = st.AppendStep(ctx, runID, &model.RunStep{Kind: model.StepLLM, Name: "plan", LatencyMS: 180, Tokens: 120})
	long := strings.Repeat("产出内容。", 500) // > 2000 rune，验证截断
	longPtr := long
	_ = st.UpdateTask(ctx, taskID, store.TaskPatch{Result: &longPtr})
	fin := time.Now()
	_ = st.UpdateRun(ctx, &model.Run{ID: runID, TaskID: taskID, AgentID: aid, Status: model.RunDone,
		TotalTokens: 300, FinishedAt: &fin})

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/runs/%d/export.md", runID), nil)
	req.SetPathValue("id", fmt.Sprint(runID))
	w := httptest.NewRecorder()
	s.exportRunMarkdown(w, req)
	if w.Code != 200 {
		t.Fatalf("导出应 200: %d", w.Code)
	}
	out := w.Body.String()
	for _, want := range []string{"# Run #", "导出测试任务", "worker", "| 1 | llm | plan |", "## 最终产出", "…"} {
		if !strings.Contains(out, want) {
			t.Fatalf("报告缺 %q", want)
		}
	}
}
