package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 状态漂移检测（I13）：重放与实际一致 → consistent；人为制造漂移 → 检出。
func TestReplayTaskStatusFolding(t *testing.T) {
	mk := func(status string) model.Event {
		return model.Event{Kind: "task.update", Data: map[string]any{"task_id": 7, "status": status}}
	}
	// 正常轨迹：queued→running→done
	st, seen := replayTaskStatus([]model.Event{mk("queued"), mk("running"), mk("done")}, 7)
	if !seen || st != model.TaskDone {
		t.Fatalf("正常轨迹折叠失败: %v %v", st, seen)
	}
	// 审批回退合法：review→queued 继续折叠
	st, _ = replayTaskStatus([]model.Event{mk("queued"), mk("running"), mk("review"), mk("queued"), mk("running")}, 7)
	if st != model.TaskRunning {
		t.Fatalf("审批回退路径折叠失败: %v", st)
	}
	// 终态后事件被忽略（状态机不变量）
	st, _ = replayTaskStatus([]model.Event{mk("done"), mk("running")}, 7)
	if st != model.TaskDone {
		t.Fatalf("终态应屏蔽后续事件: %v", st)
	}
	// 无关任务的事件不影响
	st, seen = replayTaskStatus([]model.Event{mk("done")}, 99)
	if seen {
		t.Fatal("无关 task_id 不应计入")
	}
	_ = st
}

func TestConsistencyEndpoint(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()

	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "w", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "漂移检测目标", Prompt: "普通任务，直接完成。", AgentID: aid})

	// 一致场景：事件流与实际状态同步
	_, _ = st.AppendEvent(ctx, &model.Event{Kind: "task.update", Data: map[string]any{"task_id": tid, "status": "queued"}})
	if tid == 0 {
		t.Fatal("tid 为 0——CreateTask 失败被吞")
	}
	req := httptest.NewRequest("GET", "/api/v1/tasks/x/consistency", nil)
	req.SetPathValue("id", strconv.FormatInt(tid, 10)) // 直调 handler 时手动注入路径参数（绕过 mux 匹配）
	w := httptest.NewRecorder()
	s.taskConsistency(w, req)
	var out map[string]any
	_ = json.NewDecoder(w.Body).Decode(&out)
	if out["consistent"] != true {
		t.Fatalf("一致场景误报漂移: %v", out)
	}

	// 人为制造漂移：实际状态改 review，事件流停在 queued
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: statusPtr(model.TaskReview)})
	w2 := httptest.NewRecorder()
	s.taskConsistency(w2, req)
	_ = json.NewDecoder(w2.Body).Decode(&out)
	if out["consistent"] != false {
		t.Fatalf("漂移未检出: %v", out)
	}
	if out["hint"] == nil {
		t.Fatal("漂移时应带排查提示")
	}
}

func statusPtr(s model.TaskStatus) *model.TaskStatus { return &s }
