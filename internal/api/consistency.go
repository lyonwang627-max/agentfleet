package api

import (
	"encoding/json"
	"net/http"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 任务状态漂移检测器（I13，借鉴 OpenHands event sourcing）：
// tasks.status 与 events 流是双写（v0 模式妥协）。本端点从 events 表重放某任务的
// task.update 事件派生「应有状态」，与实际状态比对——双写漂移即报警。
// 有护栏的妥协 > 无护栏的双写。

// replayTaskStatus 纯函数：按序折叠 task.update 事件，派生最终状态。
// 事件 data 需含 task_id 与 status；canceled/failed/done 为终态（其后事件忽略——状态机不变量）。
func replayTaskStatus(events []model.Event, taskID int64) (model.TaskStatus, bool) {
	final := model.TaskQueued
	seen := false
	terminal := func(s model.TaskStatus) bool {
		return s == model.TaskDone || s == model.TaskFailed || s == model.TaskCanceled
	}
	for _, e := range events {
		if e.Kind != "task.update" {
			continue
		}
		data, _ := json.Marshal(e.Data)
		var d struct {
			TaskID int64  `json:"task_id"`
			Status string `json:"status"`
		}
		if json.Unmarshal(data, &d) != nil || d.TaskID != taskID {
			continue
		}
		s := model.TaskStatus(d.Status)
		if s == "" {
			continue
		}
		seen = true
		// 状态机合法迁移：review 后 approve 回 queued 是合法的（非终态）
		if terminal(final) {
			break // 终态后不再接受事件（若流里还有，属于异常——由漂移判定暴露）
		}
		final = s
	}
	return final, seen
}

func (s *Server) taskConsistency(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	t, err := s.Store.GetTask(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	events, err := s.Store.EventsSince(r.Context(), 0, 5000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	replayed, seen := replayTaskStatus(events, id)
	consistent := !seen || replayed == t.Status || t.Status == model.TaskCanceled
	// canceled 任务的事件流可停在中间态（取消本身可不经事件标注），不算漂移
	out := map[string]any{
		"task_id":        id,
		"actual_status":  string(t.Status),
		"replayed_status": string(replayed),
		"events_seen":    seen,
		"consistent":     consistent,
	}
	if !consistent {
		out["hint"] = "双写漂移：以 events 流重放结果为准排查（或确认是否为合法的审批回退路径）"
	}
	writeJSON(w, http.StatusOK, out)
}

