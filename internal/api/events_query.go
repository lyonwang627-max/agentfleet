package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// 事件流过滤查询（I22）：events 表已有持久化与全局 seq，本端点提供服务端过滤——
// 前端事件流按类型/任务聚焦的数据源。与 SSE 流（/api/v1/events）互补：SSE 管实时，
// 本端点管历史查询。

func (s *Server) queryEvents(w http.ResponseWriter, r *http.Request) {
	since := int64(0)
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = n
		}
	}
	limit := queryInt(r, "limit", 100)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	kindFilter := r.URL.Query().Get("kind")
	taskFilter := r.URL.Query().Get("task_id")

	events, err := s.Store.EventsSince(r.Context(), since, 5000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := []map[string]any{}
	for _, e := range events {
		if kindFilter != "" && e.Kind != kindFilter {
			continue
		}
		if taskFilter != "" {
			data, _ := json.Marshal(e.Data)
			var d struct {
				TaskID int64 `json:"task_id"`
			}
			_ = json.Unmarshal(data, &d)
			if strconv.FormatInt(d.TaskID, 10) != taskFilter {
				continue
			}
		}
		out = append(out, map[string]any{
			"seq": e.Seq, "kind": e.Kind, "data": e.Data, "created_at": e.CreatedAt,
		})
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}
