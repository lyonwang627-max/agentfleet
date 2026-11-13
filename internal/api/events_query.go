package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 事件流过滤查询（I22）：events 表已有持久化与全局 seq，本端点提供服务端过滤——
// 前端事件流按类型/任务聚焦的数据源。与 SSE 流（/api/v1/events）互补：SSE 管实时，
// 本端点管历史查询。

func (s *Server) queryEvents(w http.ResponseWriter, r *http.Request) {
	since := int64(0)
	var sinceTime *time.Time // I39：相对时间模式（NOW()-<dur>）→ 按 created_at 过滤
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = n
		} else if cutoff, ok := parseRelativeTime(v, timeNow2()); ok {
			sinceTime = &cutoff
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
		if sinceTime != nil && e.CreatedAt.Before(*sinceTime) {
			continue // I39：相对时间过滤（早于 cutoff 的丢弃）
		}
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

// parseRelativeTime 解析相对时间（I39）："NOW()-<dur>" → cutoff 时间点。
// dur 语法：Go ParseDuration（s/m/h）+ d（=24h，Go 无 d 单位故预替换）。
// 非 NOW()- 前缀或解析失败 → ok=false（调用方回退 seq 语义）。
func parseRelativeTime(v string, now time.Time) (time.Time, bool) {
	const prefix = "NOW()-"
	if !strings.HasPrefix(v, prefix) {
		return time.Time{}, false
	}
	dur := strings.TrimPrefix(v, prefix)
	dur = strings.ReplaceAll(dur, "d", "h") // 简化：d 视作 h（24 倍差异由调用方用 h 表达规避）
	// 更精确：独立解析数字+d
	if strings.HasSuffix(v, "d") && !strings.HasSuffix(v, "hd") {
		// 形如 24d：重新解析
		dur = strings.TrimSuffix(strings.TrimPrefix(v, prefix), "d") + "d"
		if n, err := strconv.Atoi(strings.TrimSuffix(dur, "d")); err == nil {
			return now.Add(-time.Duration(n) * 24 * time.Hour), true
		}
		return time.Time{}, false
	}
	d, err := time.ParseDuration(dur)
	if err != nil || d <= 0 {
		return time.Time{}, false
	}
	return now.Add(-d), true
}

// timeNow2 可替换时钟（测试）。
var timeNow2 = time.Now
