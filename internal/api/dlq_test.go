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

// 死信队列（I18）：failed 任务独立列表（dlq=true）+ replay 清零重试预算重排队。
func TestDeadLetterListAndReplay(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()

	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "w", Model: "mock", Runner: "mock", Enabled: true})
	dlqID, _ := st.CreateTask(ctx, &model.Task{Title: "死信任务", Prompt: "普通任务，直接完成。", AgentID: aid})
	fin := time.Now()
	zero := 0
	// 模拟重试耗尽后的终态：failed 且 retry_count 已达上限
	_ = st.UpdateTask(ctx, dlqID, store.TaskPatch{
		Status: statusPtr(model.TaskFailed), Error: errPtr("重试耗尽"),
		RetryCount: &zero, FinishedAt: &fin}) // retry_count 语义上应为 max，此处仅验证列表过滤
	_ = st.UpdateTask(ctx, dlqID, store.TaskPatch{RetryCount: intPtr(2)})

	// 正常任务（不应进死信）
	_, _ = st.CreateTask(ctx, &model.Task{Title: "正常任务", Prompt: "普通任务，直接完成。", AgentID: aid})

	req := httptest.NewRequest("GET", "/api/v1/tasks?dlq=true", nil)
	w := httptest.NewRecorder()
	s.listTasks(w, req)
	var out struct {
		Items []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&out)
	if len(out.Items) != 1 || out.Items[0].ID != dlqID {
		t.Fatalf("死信列表应只含 failed 任务: %+v", out.Items)
	}
	if out.Items[0].Status != "failed" {
		t.Fatalf("死信任务状态异常: %+v", out.Items[0])
	}

	// replay：清零重试预算重排队
	rr := httptest.NewRequest("POST", "/api/v1/tasks/"+itoa(dlqID)+"/replay", nil)
	rr.SetPathValue("id", itoa(dlqID))
	wr := httptest.NewRecorder()
	s.taskAction(wr, rr)
	if wr.Code != 200 {
		t.Fatalf("replay 应 200: %d %s", wr.Code, wr.Body.String())
	}
	t2, _ := st.GetTask(ctx, dlqID)
	if t2.Status != model.TaskQueued || t2.RetryCount != 0 {
		t.Fatalf("replay 后应 queued 且重试计数清零: %+v", t2)
	}
	// 非 failed 任务 replay → 409
	wr2 := httptest.NewRecorder()
	reqBad := httptest.NewRequest("POST", "/api/v1/tasks/999/replay", nil)
	reqBad.SetPathValue("id", "999")
	s.taskAction(wr2, reqBad)
	if wr2.Code != 404 {
		t.Fatalf("不存在的任务应 404: %d", wr2.Code)
	}
}

func errPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func itoa(n int64) string     { return strings.TrimSpace(fmtInt(n)) }
func fmtInt(n int64) string   { b, _ := json.Marshal(n); return string(b) }
