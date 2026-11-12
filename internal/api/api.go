// Package api HTTP 层：标准库 ServeMux（Go 1.22+ 方法+路径模式），不引第三方路由。
package api

import (
	"context"
	"sync"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/eval"
	"github.com/lyonwang/agentfleet/internal/webhook"
	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
	"github.com/lyonwang/agentfleet/web"
)

type Server struct {
	Store     store.Store
	Hub       *scheduler.Hub
	LLM       *gateway.LLM
	Eval      *eval.Engine
	Sched     *scheduler.Scheduler
	StartedAt time.Time
	limiter   *RateLimiter
	// I37：入站回调幂等（Idempotency-Key → 首个任务；外部系统重试不重复建任务）
	ingestMu   sync.Mutex
	ingestSeen map[string]ingestRecord
}

// ingestRecord 幂等记录（10 分钟 TTL 内同 key 返回首个任务）。
type ingestRecord struct {
	taskID int64
	seenAt time.Time
}

func (s *Server) ingestDedup(key string) (int64, bool) {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	if s.ingestSeen == nil {
		s.ingestSeen = map[string]ingestRecord{}
	}
	if rec, ok := s.ingestSeen[key]; ok && time.Since(rec.seenAt) < 10*time.Minute {
		return rec.taskID, true
	}
	return 0, false
}

func (s *Server) ingestRemember(key string, taskID int64) {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	if s.ingestSeen == nil {
		s.ingestSeen = map[string]ingestRecord{}
	}
	s.ingestSeen[key] = ingestRecord{taskID: taskID, seenAt: time.Now()}
}

// SetRateLimit 开启 IP 级限流（次/分钟；<=0 关闭）。在 Routes() 前调用。
func (s *Server) SetRateLimit(perMinute int) { s.limiter = NewRateLimiter(perMinute) }

// publish 事件发布（nil 守卫）：测试直调 handler 时 Hub 可能为 nil。
func (s *Server) publish(ctx context.Context, kind string, data map[string]any) {
	if s.Hub != nil {
		s.Hub.Publish(ctx, kind, data)
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.health)

	// agents
	mux.HandleFunc("GET /api/v1/agents", s.listAgents)
	mux.HandleFunc("POST /api/v1/agents", s.createAgent)
	mux.HandleFunc("GET /api/v1/agents/{id}", s.getAgent)
	mux.HandleFunc("PATCH /api/v1/agents/{id}", s.patchAgent)
	mux.HandleFunc("POST /api/v1/agents/{id}/enable", s.setAgent(true))
	mux.HandleFunc("POST /api/v1/agents/{id}/disable", s.setAgent(false))

	// agent versions（F2：发布/回滚）
	mux.HandleFunc("POST /api/v1/agents/{id}/clone", s.cloneAgent) // 克隆派生（I3）
	mux.HandleFunc("POST /api/v1/agents/{id}/publish", s.publishAgent)
	mux.HandleFunc("GET /api/v1/agents/{id}/versions", s.listAgentVersions)
	mux.HandleFunc("POST /api/v1/agents/{id}/versions/{version}/rollback", s.rollbackAgent)

	// tasks
	mux.HandleFunc("GET /api/v1/tasks", s.listTasks)
	mux.HandleFunc("POST /api/v1/tasks", s.createTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.getTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}/consistency", s.taskConsistency) // 状态漂移检测（I13）
	mux.HandleFunc("GET /api/v1/tasks/{id}/dag", s.taskDAG)                 // 依赖子图 + 环标记（I20）
	mux.HandleFunc("POST /api/v1/tasks/{id}/retry", s.taskAction)
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.taskAction)
	mux.HandleFunc("POST /api/v1/tasks/{id}/approve", s.taskAction)
	mux.HandleFunc("POST /api/v1/tasks/{id}/replay", s.taskAction)     // I18 死信重放
	mux.HandleFunc("POST /api/v1/tasks/{id}/precheck", s.precheckTask) // AI 预审（F16）

	// runs & trace
	mux.HandleFunc("GET /api/v1/tasks/{id}/runs", s.listRuns)
	mux.HandleFunc("GET /api/v1/runs/{id}", s.getRun)
	mux.HandleFunc("GET /api/v1/runs/{id}/export.md", s.exportRunMarkdown) // I23 报告导出
	mux.HandleFunc("GET /api/v1/runs/{id}/events", s.runEvents)

	// 全局事件流
	mux.HandleFunc("GET /api/v1/events", s.globalEvents)
	mux.HandleFunc("GET /api/v1/events/query", s.queryEvents) // I22 过滤查询

	// chat
	mux.HandleFunc("POST /api/v1/chat", s.chat)
	mux.HandleFunc("POST /api/v1/chat/stream", s.chatStream)         // F15-a 流式
	mux.HandleFunc("POST /api/v1/chat/room", s.chatRoom)             // I31 群聊（@ 唤醒）
	mux.HandleFunc("GET /api/v1/conversations", s.listConversations) // F6 回放数据层
	mux.HandleFunc("GET /api/v1/conversations/{id}", s.getConversation)

	// gateway 用量
	mux.HandleFunc("GET /api/v1/usage", s.usage)
	mux.HandleFunc("GET /api/v1/models", s.models)
	mux.HandleFunc("GET /api/v1/mcp/servers", s.mcpServers)

	// automations（cron 定时建任务）
	mux.HandleFunc("GET /api/v1/automations", s.listAutomations)
	mux.HandleFunc("POST /api/v1/automations", s.createAutomation)
	mux.HandleFunc("GET /api/v1/automations/{id}", s.getAutomation)
	mux.HandleFunc("PATCH /api/v1/automations/{id}", s.patchAutomation)
	mux.HandleFunc("DELETE /api/v1/automations/{id}", s.deleteAutomation)
	mux.HandleFunc("POST /api/v1/automations/{id}/enable", s.setAutomation(true))
	mux.HandleFunc("POST /api/v1/automations/{id}/disable", s.setAutomation(false))

	// artifacts / workspaces / skills（v1.4：执行环境维度三模块）
	mux.HandleFunc("GET /api/v1/artifacts", s.listArtifacts)
	mux.HandleFunc("GET /api/v1/artifacts/{id}", s.getArtifact)
	mux.HandleFunc("GET /api/v1/workspaces", s.listWorkspaces)
	mux.HandleFunc("POST /api/v1/workspaces", s.createWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{id}", s.getWorkspace)
	mux.HandleFunc("PATCH /api/v1/workspaces/{id}", s.patchWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/health", s.workspaceHealth) // I16 运行时探测
	mux.HandleFunc("POST /api/v1/webhooks", s.createWebhook)                // I7 出站订阅
	mux.HandleFunc("GET /api/v1/webhooks", s.listWebhooks)
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", s.deleteWebhook)
	mux.HandleFunc("POST /api/v1/webhooks/{id}/ingest/{token}", s.ingestWebhook) // I37 入站回调
	mux.HandleFunc("DELETE /api/v1/workspaces/{id}", s.deleteWorkspace)
	mux.HandleFunc("GET /api/v1/skills", s.listSkills)
	mux.HandleFunc("POST /api/v1/skills", s.createSkill)
	mux.HandleFunc("GET /api/v1/skills/{id}", s.getSkill)
	mux.HandleFunc("PATCH /api/v1/skills/{id}", s.patchSkill)
	mux.HandleFunc("DELETE /api/v1/skills/{id}", s.deleteSkill)

	// eval
	mux.HandleFunc("GET /api/v1/eval-datasets", s.listDatasets)
	mux.HandleFunc("GET /api/v1/eval-datasets/{id}/export", s.exportDataset) // I4 导出
	mux.HandleFunc("POST /api/v1/eval-datasets/import", s.importDataset)     // I4 导入
	mux.HandleFunc("GET /api/v1/evals", s.listEvals)
	mux.HandleFunc("POST /api/v1/evals", s.createEval)
	mux.HandleFunc("GET /api/v1/evals/{id}", s.getEval)
	mux.HandleFunc("GET /api/v1/evals/compare", s.compareEvals)                        // 路由 A/B（F18-c）
	mux.HandleFunc("GET /api/v1/routing/stats", s.routingStats)

	// Multica 对标：Projects / TaskComments / Steer
	mux.HandleFunc("POST /api/v1/projects", s.createProject)
	mux.HandleFunc("GET /api/v1/projects", s.listProjects)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/v1/tasks/{id}/comments", s.listTaskComments)
	mux.HandleFunc("POST /api/v1/tasks/{id}/comments", s.postTaskComment)
	mux.HandleFunc("POST /api/v1/tasks/{id}/steer", s.steerTask)                        // 升级率（F18-c）
	mux.HandleFunc("POST /api/v1/evals/{id}/prescription", s.prescribe)                // RSI 处方生成（F17-a）
	mux.HandleFunc("POST /api/v1/agents/{id}/apply-prescription", s.applyPrescription) // 处方一键应用（F17-b）

	// 单二进制交付（-tags release）：根路径服务内嵌前端 SPA
	if h, ok := web.DistHandler(); ok {
		mux.Handle("/", h)
	}

	return logMiddleware(s.authMiddleware(s.rateLimitMiddleware(mux)))
}

// ---- 通用 ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, code int, err error) {
	var b errBody
	b.Error.Code = http.StatusText(code)
	b.Error.Message = err.Error()
	writeJSON(w, code, b)
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func queryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/events" {
			log.Printf("[api] %s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// ---- health ----

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := contextTimeout(3 * time.Second)
	defer cancel()
	dbOK := s.Store.Ping(ctx) == nil
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "db": dbOK, "scheduler": s.Sched.Healthy(),
		"llm":      map[string]any{"mode": s.LLM.Mode, "model": s.LLM.Model},
		"uptime_s": int(time.Since(s.StartedAt).Seconds()),
	})
}

// ---- agents ----

type agentIn struct {
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	SystemPrompt   string   `json:"system_prompt"`
	Model          string   `json:"model"`
	Tools          []string `json:"tools"`
	Runner         string   `json:"runner"`
	MaxConcurrency int      `json:"max_concurrency"`
	TokenBudget24h int      `json:"token_budget_24h"`
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var in agentIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 必填"))
		return
	}
	if in.Runner == "" {
		in.Runner = "llm"
	}
	if in.Model == "" {
		in.Model = s.LLM.Model
	}
	if in.MaxConcurrency <= 0 {
		in.MaxConcurrency = 1
	}
	a := &model.Agent{Name: in.Name, Role: in.Role, SystemPrompt: in.SystemPrompt, Model: in.Model,
		Tools: in.Tools, Runner: in.Runner, MaxConcurrency: in.MaxConcurrency,
		TokenBudget24h: in.TokenBudget24h, Enabled: true}
	id, err := s.Store.CreateAgent(r.Context(), a)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "agent.update", map[string]any{"agent_id": id, "action": "created"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.Store.ListAgents(r.Context(), store.AgentFilter{})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	items := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		items = append(items, s.agentSummary(r.Context(), &a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) agentSummary(ctx contextCtx, a *model.Agent) map[string]any {
	tasks, _, _ := s.Store.ListTasks(ctx, nil, a.ID, 1, 100)
	running, queued := 0, 0
	for _, t := range tasks {
		switch t.Status {
		case model.TaskRunning:
			running++
		case model.TaskQueued:
			queued++
		}
	}
	return map[string]any{"id": a.ID, "name": a.Name, "role": a.Role, "model": a.Model,
		"runner": a.Runner, "enabled": a.Enabled, "max_concurrency": a.MaxConcurrency,
		"running": running, "queued": queued, "created_at": a.CreatedAt}
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAgent(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	out := s.agentSummary(r.Context(), a)
	out["system_prompt"] = a.SystemPrompt
	out["tools"] = a.Tools
	out["draft_config"] = a.DraftConfig // I14：nil=与线上一致
	out["has_unpublished_changes"] = agentDirty(a)
	runs := s.recentRuns(r.Context(), a.ID, 20)
	out["recent_runs"] = runs
	writeJSON(w, http.StatusOK, out)
}

// agentDirty 草稿与线上是否有差异（I14）。
func agentDirty(a *model.Agent) bool {
	return a.DraftConfig != nil
}

func (s *Server) recentRuns(ctx contextCtx, agentID int64, limit int) []map[string]any {
	// v1：按 agent 的任务聚合 runs（简版统计），细节统计见 ROADMAP
	tasks, _, _ := s.Store.ListTasks(ctx, nil, agentID, 1, 50)
	out := []map[string]any{}
	for _, t := range tasks {
		runs, _ := s.Store.ListRuns(ctx, t.ID)
		for _, run := range runs {
			out = append(out, map[string]any{"run_id": run.ID, "task_id": t.ID, "task_title": t.Title,
				"status": string(run.Status), "total_tokens": run.TotalTokens, "started_at": run.StartedAt})
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func (s *Server) patchAgent(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	patch := store.AgentPatch{}
	if v, ok := in["name"].(string); ok {
		patch.Name = &v
	}
	if v, ok := in["role"].(string); ok {
		patch.Role = &v
	}
	if v, ok := in["system_prompt"].(string); ok {
		patch.SystemPrompt = &v
	}
	if v, ok := in["model"].(string); ok {
		patch.Model = &v
	}
	if v, ok := in["runner"].(string); ok {
		patch.Runner = &v
	}
	if v, ok := in["max_concurrency"].(float64); ok {
		n := int(v)
		patch.MaxConcurrency = &n
	}
	if v, ok := in["token_budget_24h"].(float64); ok {
		n := int(v)
		patch.TokenBudget24h = &n
	}
	if v, ok := in["tools"].([]any); ok {
		tools := []string{}
		for _, t := range v {
			if ts, ok := t.(string); ok {
				tools = append(tools, ts)
			}
		}
		patch.Tools = &tools
	}
	// I14 草稿/发布双态：PATCH 只进草稿——编辑不再直接改线上配置（在途 run 不受影响），
	// POST /agents/{id}/publish 显式落线（落线时经 F2 版本链快照可回溯）
	if err := s.Store.UpdateAgentDraft(r.Context(), id, patch); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "agent.update", map[string]any{"agent_id": id, "action": "draft_updated"})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "has_unpublished_changes": true})
}

func (s *Server) setAgent(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r)
		if err := s.Store.UpdateAgent(r.Context(), id, store.AgentPatch{Enabled: &enabled}); err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		s.publish(r.Context(), "agent.update", map[string]any{"agent_id": id, "enabled": enabled})
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
	}
}

// ---- tasks ----

type taskIn struct {
	Title       string  `json:"title"`
	Prompt      string  `json:"prompt"`
	AgentID     int64   `json:"agent_id"`
	Priority    int     `json:"priority"`
	DependsOn   []int64 `json:"depends_on"`
	WorkspaceID int64   `json:"workspace_id"`
	SkillIDs    []int64 `json:"skill_ids"`
	SquadID     int64   `json:"squad_id"` // 分给小队（I30）：AgentID 需为该队 leader
	ProjectID   int64   `json:"project_id"`
	Labels      []string `json:"labels"`
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in taskIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Title) == "" || in.AgentID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("title 与 agent_id 必填"))
		return
	}
	if in.Priority < 1 || in.Priority > 9 {
		in.Priority = 5
	}
	// I20：依赖存在性校验——缺失依赖会让任务永远 blocked（调度器等 deps 全 done），
	// 静默卡死比报错更难排查，创建即拒绝。deps 去重保序。
	if len(in.DependsOn) > 0 {
		seenDep := map[int64]bool{}
		uniq := in.DependsOn[:0]
		for _, dep := range in.DependsOn {
			if seenDep[dep] {
				continue
			}
			seenDep[dep] = true
			if _, err := s.Store.GetTask(r.Context(), dep); err != nil {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("依赖任务 %d 不存在", dep))
				return
			}
			uniq = append(uniq, dep)
		}
		in.DependsOn = uniq
	}
	// I30：分给小队的任务，AgentID 校验为该队 leader（只有 leader 被唤醒）
	if in.SquadID > 0 {
		sq, err := s.Store.GetSquad(r.Context(), in.SquadID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("squad 不存在"))
			return
		}
		if in.AgentID == 0 {
			in.AgentID = sq.LeaderID // 缺省取 leader
		} else if in.AgentID != sq.LeaderID {
			writeErr(w, http.StatusBadRequest, errors.New("小队任务的 agent_id 必须是该队 leader"))
			return
		}
	}
	t := &model.Task{Title: in.Title, Prompt: in.Prompt, AgentID: in.AgentID,
		Priority: in.Priority, DependsOn: in.DependsOn,
		WorkspaceID: in.WorkspaceID, SkillIDs: in.SkillIDs, SquadID: in.SquadID,
		ProjectID: in.ProjectID, Labels: in.Labels}
	id, err := s.Store.CreateTask(r.Context(), t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "task.update", map[string]any{"task_id": id, "status": "queued"})
	if s.Sched != nil {
		s.Sched.Wake() // 事件驱动：建任务即时派发（F13-a）
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": string(model.TaskQueued)})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	label := r.URL.Query().Get("label")
	projectID := int64(queryInt(r, "project_id", 0))
	var status *model.TaskStatus
	if r.URL.Query().Get("dlq") == "true" {
		// I18 死信语义：failed = 重试已耗尽的任务（failTask 保证 failed 必然 retry_count>=max_retries）
		st := model.TaskFailed
		status = &st
	} else if sv := r.URL.Query().Get("status"); sv != "" {
		st := model.TaskStatus(sv)
		status = &st
	}
	items, total, err := s.Store.ListTasks(r.Context(), status, int64(queryInt(r, "agent_id", 0)),
		queryInt(r, "page", 1), queryInt(r, "page_size", 50))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// label / project 过滤（内存过滤，规模内足够）
	if label != "" || projectID > 0 {
		filtered := items[:0]
		for _, t := range items {
			if label != "" && !hasLabelLocal(t.Labels, label) {
				continue
			}
			if projectID > 0 && t.ProjectID != projectID {
				continue
			}
			filtered = append(filtered, t)
		}
		items = filtered
		total = len(items)
	}
	// 附加 blocked_by（依赖中未完成者）
	type row struct {
		model.Task
		AgentName string  `json:"agent_name"`
		BlockedBy []int64 `json:"blocked_by"`
	}
	out := make([]row, 0, len(items))
	for _, t := range items {
		r0 := row{Task: t, BlockedBy: []int64{}}
		if a, err := s.Store.GetAgent(r.Context(), t.AgentID); err == nil {
			r0.AgentName = a.Name
		}
		for _, dep := range t.DependsOn {
			if d, err := s.Store.GetTask(r.Context(), dep); err == nil && d.Status != model.TaskDone {
				r0.BlockedBy = append(r0.BlockedBy, dep)
			}
		}
		out = append(out, r0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.GetTask(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	deps := []map[string]any{}
	for _, dep := range t.DependsOn {
		if d, err := s.Store.GetTask(r.Context(), dep); err == nil {
			deps = append(deps, map[string]any{"id": d.ID, "title": d.Title, "status": string(d.Status)})
		}
	}
	if a, err := s.Store.GetAgent(r.Context(), t.AgentID); err == nil {
		t.AgentName = a.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": t.ID, "title": t.Title, "prompt": t.Prompt, "status": string(t.Status),
		"priority": t.Priority, "agent_id": t.AgentID, "agent_name": t.AgentName,
		"result": t.Result, "error": t.Error, "deps": deps,
		"created_at": t.CreatedAt, "started_at": t.StartedAt, "finished_at": t.FinishedAt})
}

func (s *Server) taskAction(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	t, err := s.Store.GetTask(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	ctx := r.Context()
	now := time.Now()
	action := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/"+strconv.FormatInt(id, 10)+"/")
	switch action {
	case "retry":
		if t.Status != model.TaskFailed && t.Status != model.TaskCanceled {
			writeErr(w, http.StatusConflict, errors.New("仅 failed/canceled 可 retry"))
			return
		}
		_ = s.Store.UpdateTask(ctx, id, store.TaskPatch{Status: ptrStatus(model.TaskQueued),
			Error: ptrStr(""), Result: ptrStr(""), VisibleAfter: &now})
	case "replay":
		// I18 死信重放：与 retry 的区别是清零重试计数（重新获得完整重试预算），
		// 语义为「人工判定值得从头再跑」而非「再试一次」
		if t.Status != model.TaskFailed {
			writeErr(w, http.StatusConflict, errors.New("仅 failed 可 replay"))
			return
		}
		zero := 0
		_ = s.Store.UpdateTask(ctx, id, store.TaskPatch{Status: ptrStatus(model.TaskQueued),
			Error: ptrStr(""), Result: ptrStr(""), RetryCount: &zero, VisibleAfter: &now})
	case "cancel":
		if t.Status == model.TaskDone || t.Status == model.TaskCanceled {
			writeErr(w, http.StatusConflict, errors.New("任务已结束"))
			return
		}
		// I33：review 状态下取消 = 审批拒绝 → gate-reject 步留痕（与 approve 的 approved-by-human 对称）
		if t.Status == model.TaskReview {
			if runs, _ := s.Store.ListRuns(ctx, id); len(runs) > 0 {
				_, _ = s.Store.AppendStep(ctx, runs[0].ID, &model.RunStep{
					Kind: model.StepGate, Name: "gate-reject",
					Note: "[审批记录] 人工拒绝（cancel）——任务终止不执行",
					Input: map[string]any{"task_id": id, "risk_level": t.RiskLevel}})
			}
		}
		_ = s.Store.UpdateTask(ctx, id, store.TaskPatch{Status: ptrStatus(model.TaskCanceled), FinishedAt: &now})
	case "approve":
		if t.Status != model.TaskReview {
			writeErr(w, http.StatusConflict, errors.New("仅 review 状态可 approve"))
			return
		}
		// F3：审批可带意见；留痕 + 意见注入 prompt（runner 重放时确定性走完）+ gate 步落 trace
		var in struct {
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil && err.Error() != "EOF" {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		record := fmt.Sprintf("[审批记录] %s 人工放行", now.Format("2006-01-02 15:04"))
		// I29：高风险任务的 approve 必须附审批意见（强制留痕，说明为什么放行）
		if t.RiskLevel == "high" && strings.TrimSpace(in.Note) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("高风险任务审批必须附审批意见（note）"))
			return
		}
		if strings.TrimSpace(in.Note) != "" {
			record += "\n审批意见: " + strings.TrimSpace(in.Note)
		}
		approved := t.Prompt + "\n\n" + record
		if err := s.Store.UpdateTask(ctx, id, store.TaskPatch{
			Status: ptrStatus(model.TaskQueued), VisibleAfter: &now, Prompt: &approved}); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// 审批留痕进最近一次 run 的 trace（kind=gate，前端瀑布图黄色可见）
		if runs, _ := s.Store.ListRuns(ctx, id); len(runs) > 0 {
			_, _ = s.Store.AppendStep(ctx, runs[0].ID, &model.RunStep{
				Kind: model.StepGate, Name: "approved-by-human",
				Note: record, Input: map[string]any{"task_id": id, "note": in.Note}})
		}
	default:
		writeErr(w, http.StatusNotFound, errors.New("未知操作"))
		return
	}
	s.publish(ctx, "task.update", map[string]any{"task_id": id, "status": "changed", "action": action})
	if s.Sched != nil {
		s.Sched.Wake() // retry/approve 后即时派发（F13-a）
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "action": action})
}

// ---- runs ----

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.ListRuns(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Store.GetRun(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ---- SSE ----

func (s *Server) globalEvents(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, parseSince(r))
}

func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	runID := pathID(r)
	if _, err := s.Store.GetRun(r.Context(), runID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.serveSSE(w, r, parseSince(r))
}

func parseSince(r *http.Request) int64 {
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	return int64(queryInt(r, "since", 0))
}

func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request, since int64) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := s.Hub.Subscribe(r.Context(), since)
	defer cancel()
	if last := s.Hub.LastSeq(); since == 0 && last > 0 {
		// 首连告诉客户端当前 seq，便于断线重连
		_, _ = w.Write([]byte("retry: 3000\n\n"))
		fl.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			payload, _ := json.Marshal(e)
			_, _ = w.Write([]byte("id: " + strconv.FormatInt(e.Seq, 10) + "\ndata: " + string(payload) + "\n\n"))
			fl.Flush()
		}
	}
}

// ---- webhooks（I7）----

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil ||
		strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 与 url 必填"))
		return
	}
	// 密钥生成对齐 auth.go（crypto/rand + hex）；前缀 whk- 区分用途
	plaintext, _, err := generateAPIKey()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	secret := "whk-" + strings.TrimPrefix(plaintext, "fleet-")
	hook := &model.Webhook{Name: in.Name, URL: in.URL, Secret: secret, Events: in.Events, Active: true}
	id, err := s.Store.CreateWebhook(r.Context(), hook)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "webhook.update", map[string]any{"webhook_id": id, "action": "created"})
	// secret 明文仅创建响应返回（对齐 API Key 惯例）
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "secret": secret})
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListWebhooks(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteWebhook(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "deleted": true})
}

// ingestWebhook 入站回调（I37，与出站 I7 对称）：外部告警/工单系统 POST payload → 校验
// → 自动建任务。token = IngestToken(hook)（secret 派生）；payload 需 agent_id/title/prompt。
func (s *Server) ingestWebhook(w http.ResponseWriter, r *http.Request) {
	hooks, err := s.Store.ListWebhooks(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var hook *model.Webhook
	id := pathID(r)
	for i := range hooks {
		if hooks[i].ID == id && hooks[i].Active {
			hook = &hooks[i]
		}
	}
	if hook == nil {
		writeErr(w, http.StatusNotFound, errors.New("webhook 不存在或未启用"))
		return
	}
	if webhook.IngestToken(hook) != r.PathValue("token") {
		writeErr(w, http.StatusUnauthorized, errors.New("ingest token 无效"))
		return
	}
	// I37 幂等：外部系统重试同一请求 → 返回首个任务，不重复建
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		if taskID, hit := s.ingestDedup(key); hit {
			writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "deduplicated": true})
			return
		}
	}
	var in struct {
		AgentID  int64    `json:"agent_id"`
		Title    string   `json:"title"`
		Prompt   string   `json:"prompt"`
		Priority int      `json:"priority"`
		Labels   []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Prompt) == "" || in.AgentID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("title/prompt/agent_id 必填"))
		return
	}
	a, err := s.Store.GetAgent(r.Context(), in.AgentID)
	if err != nil || !a.Enabled {
		writeErr(w, http.StatusBadRequest, errors.New("agent 不存在或未启用"))
		return
	}
	labels := append(in.Labels, "webhook:"+strconv.FormatInt(hook.ID, 10))
	t := &model.Task{Title: clip(in.Title, 120), Prompt: clip(in.Prompt, 4000),
		AgentID: in.AgentID, Priority: in.Priority, Labels: labels}
	if in.Priority < 1 || in.Priority > 9 {
		t.Priority = 5
	}
	taskID, err := s.Store.CreateTask(r.Context(), t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		s.ingestRemember(key, taskID)
	}
	s.publish(r.Context(), "webhook.ingest", map[string]any{
		"webhook_id": hook.ID, "task_id": taskID, "title": t.Title})
	if s.Sched != nil {
		s.Sched.Wake()
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task_id": taskID, "webhook_id": hook.ID})
}

// ---- chat ----

type chatIn struct {
	AgentID        int64  `json:"agent_id"`
	Message        string `json:"message"`
	ConversationID int64  `json:"conversation_id"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var in chatIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	agent, err := s.Store.GetAgent(ctx, in.AgentID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	convID := in.ConversationID
	if convID <= 0 {
		convID, _ = s.Store.CreateConversation(ctx, &model.Conversation{AgentID: agent.ID, Title: head2(in.Message, 30)})
	}
	_, _ = s.Store.AppendMessage(ctx, &model.Message{ConversationID: convID, Role: "user", Content: in.Message})

	// I6 多轮上下文：历史（含刚入库的当前消息）压成有界时间线——
	// 此前每轮只发当前消息，模型对之前的对话毫无记忆
	msgs, _ := s.Store.ListMessages(ctx, convID)
	transcript, _ := chatHistoryTranscript(msgs, chatKeepRecent)
	if transcript == "" {
		transcript = in.Message
	}
	reply, tokens, err := s.LLM.Chat(ctx, agent.SystemPrompt, transcript, 900)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	_, _ = s.Store.AppendMessage(ctx, &model.Message{ConversationID: convID, Role: "agent", Content: reply})

	// chat 也计用量（网关计量统一入口；prompt 侧按有界时间线计）
	_ = s.Store.AppendUsage(ctx, &model.UsageEvent{AgentID: agent.ID, AgentName: agent.Name,
		Model: agent.Model, Purpose: "chat", PromptTokens: len2(transcript), CompletionTokens: tokens, Requests: 1})
	writeJSON(w, http.StatusOK, map[string]any{"conversation_id": convID, "reply": reply, "run_id": 0})
}

// ---- usage & models ----

func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	var d time.Duration
	switch window {
	case "7d":
		d = 7 * 24 * time.Hour
	default:
		d, window = 24*time.Hour, "24h"
	}
	total, byModel, byAgent, err := s.Store.UsageSummary(r.Context(), d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	byPurpose, err := s.Store.UsageByPurpose(r.Context(), d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	enabled, perMinute, rejected := s.limiter.Stats()
	// F10 成本核算：按 by_model 的 token 量 × 定价表聚合（本地模型=0）
	pricing := s.LLM.Pricing
	totalCost := 0.0
	byModelCost := map[string]float64{}
	for _, m := range byModel {
		c := gateway.CostUSD(pricing, m.Model, m.PromptTokens, m.CompletionTokens)
		byModelCost[m.Model] = c
		totalCost += c
	}
	byAgentCost := map[string]float64{}
	agentModel := map[int64]string{}
	if agents, err := s.Store.ListAgents(r.Context(), store.AgentFilter{}); err == nil {
		for _, a := range agents {
			agentModel[a.ID] = a.Model
		}
	}
	for _, a := range byAgent {
		byAgentCost[a.AgentName] = gateway.CostUSD(pricing, agentModel[a.AgentID], a.PromptTokens, a.CompletionTokens)
	}
	writeJSON(w, http.StatusOK, map[string]any{"window": window, "requests": total.Requests,
		"prompt_tokens": total.PromptTokens, "completion_tokens": total.CompletionTokens,
		"cost_usd": round4(totalCost), "cost_by_model": byModelCost, "cost_by_agent": byAgentCost,
		"by_model": byModel, "by_agent": byAgent,
		"by_purpose": byPurpose,
		"rate_limit": map[string]any{"enabled": enabled, "per_minute": perMinute, "rejected_total": rejected}})
}

func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	// v1.4 模型可选化：动态发现（Ollama 已装 + 内置），不再硬编码清单
	writeJSON(w, http.StatusOK, map[string]any{"models": s.LLM.ListModels()})
}

// mcpServers 已注册的 MCP server 及其动态发现的工具列表（F4-b 观测端点）。
func (s *Server) mcpServers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"servers": runner.ListMCPServers()})
}

// ---- eval ----

func (s *Server) listDatasets(w http.ResponseWriter, r *http.Request) {
	ds, err := s.Store.ListDatasets(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

func (s *Server) listEvals(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.ListEvalRuns(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) createEval(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentID   int64 `json:"agent_id"`
		DatasetID int64 `json:"dataset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AgentID <= 0 || in.DatasetID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("agent_id 与 dataset_id 必填"))
		return
	}
	e := &model.EvalRun{DatasetID: in.DatasetID, AgentID: in.AgentID}
	id, err := s.Store.CreateEvalRun(r.Context(), e)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.Eval.Start(r.Context(), id, in.DatasetID, in.AgentID)
	s.publish(r.Context(), "eval.update", map[string]any{"eval_id": id, "status": "running"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "running"})
}

func (s *Server) getEval(w http.ResponseWriter, r *http.Request) {
	e, err := s.Store.GetEvalRun(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// ---- 小工具 ----

type contextCtx = context.Context

func contextTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func ptrStatus(s model.TaskStatus) *model.TaskStatus { return &s }
func ptrStr(s string) *string                        { return &s }

func head2(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// len2 粗略 token 估算（中文 ~1.5 字符/token，按 2 保守取整）。
func len2(s string) int { return len([]rune(s))/2 + 8 }

func round4(f float64) float64 { return float64(int64(f*10000+0.5)) / 10000 }
