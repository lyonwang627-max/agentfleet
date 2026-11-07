package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/lyonwang/agentfleet/internal/store"
)

// Prometheus 文本格式指标端点（I2）：零 client 依赖，手写 exposition。
// 口径与 /api/v1/usage、/health 对齐——运维抓取与看板共用同一数据源。

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type metricLine struct {
		name, labels, value, help, typ string
	}
	lines := []metricLine{}

	// 任务状态分布
	tasks, total, _ := s.Store.ListTasks(ctx, nil, 0, 1, 100)
	byStatus := map[string]int{}
	for _, t := range tasks {
		byStatus[string(t.Status)]++
	}
	lines = append(lines, metricLine{name: "agentfleet_tasks_by_status", help: "任务按状态分布", typ: "gauge"})
	for _, st := range []string{"queued", "running", "review", "done", "failed", "canceled"} {
		lines = append(lines, metricLine{name: "agentfleet_tasks_by_status",
			labels: fmt.Sprintf("{status=%q}", st), value: strconv.Itoa(byStatus[st])})
	}
	lines = append(lines, metricLine{name: "agentfleet_tasks_total", value: strconv.Itoa(total), typ: "counter"})

	// agents
	agents, _ := s.Store.ListAgents(ctx, store.AgentFilter{})
	enabled := 0
	for _, a := range agents {
		if a.Enabled {
			enabled++
		}
	}
	lines = append(lines, metricLine{name: "agentfleet_agents_enabled", value: strconv.Itoa(enabled), typ: "gauge"})
	lines = append(lines, metricLine{name: "agentfleet_agents_total", value: strconv.Itoa(len(agents)), typ: "gauge"})

	// 用量按用途（token 计量；聚合失败静默降级——metrics 不应 500）
	if byPurpose, err := s.Store.UsageByPurpose(ctx, 24*time.Hour); err == nil {
		lines = append(lines, metricLine{name: "agentfleet_tokens_total", help: "token 用量按用途与模型", typ: "counter"})
		for _, u := range byPurpose {
			lines = append(lines, metricLine{name: "agentfleet_tokens_total",
				labels: fmt.Sprintf("{purpose=%q,model=%q}", u.Purpose, u.Model),
				value:  strconv.Itoa(u.PromptTokens + u.CompletionTokens)})
		}
	}

	// 限流拒绝
	if s.limiter != nil {
		_, _, rejected := s.limiter.Stats()
		lines = append(lines, metricLine{name: "agentfleet_rate_limit_rejected_total", help: "限流拒绝累计", typ: "counter",
			value: strconv.FormatInt(rejected, 10)})
	}

	// 评测与运行时长
	if evals, err := s.Store.ListEvalRuns(ctx); err == nil {
		lines = append(lines, metricLine{name: "agentfleet_eval_runs_total", value: strconv.Itoa(len(evals)), typ: "counter"})
	}
	lines = append(lines, metricLine{name: "agentfleet_uptime_seconds",
		value: strconv.Itoa(int(time.Since(s.StartedAt).Seconds())), typ: "gauge"})

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	seenHelp := map[string]bool{}
	for _, l := range lines {
		if l.help != "" && !seenHelp[l.name] {
			fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", l.name, l.help, l.name, l.typ)
			seenHelp[l.name] = true
		}
		fmt.Fprintf(w, "%s%s %s\n", l.name, l.labels, l.value)
	}
}
