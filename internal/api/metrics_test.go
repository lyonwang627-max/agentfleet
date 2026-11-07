package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// /metrics exposition（I2）：格式合法 + 关键指标存在 + 值非负。
func TestMetricsExposition(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "m", Model: "mock", Runner: "mock", Enabled: true})
	_, _ = st.CreateTask(ctx, &model.Task{Title: "t", Prompt: "普通任务，直接完成。", AgentID: aid})
	_ = st.AppendUsage(ctx, &model.UsageEvent{AgentID: aid, Model: "mock", Purpose: "chat", PromptTokens: 5, CompletionTokens: 7, Requests: 1})

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	s.metrics(w, req)
	out := w.Body.String()

	for _, want := range []string{
		"# HELP agentfleet_tasks_by_status",
		`agentfleet_tasks_total 1`,
		"agentfleet_agents_enabled 1",
		`agentfleet_tokens_total{purpose="chat",model="mock"} 12`,
		"agentfleet_uptime_seconds",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics 缺少 %q", want)
		}
	}
}
