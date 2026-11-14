// Package scheduler 任务调度器：
//   - 认领：ClaimNextTask（pg 实现为 FOR UPDATE SKIP LOCKED，多实例安全）
//   - 执行：按 agent 的 max_concurrency 限流，runner 落 step（trace）
//   - 状态机：queued→running→done/failed/review；handoff 改派；重试退避
//   - 事件：状态变化与 step 均发 SSE（经 events.Hub 持久化+广播）
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

const (
	retryBackoff = 5 * time.Second
	pollInterval = 800 * time.Millisecond
	maxHandoffs  = 3
)

// timeNow 可替换时钟（测试用）。
var timeNow = time.Now

// gateAcquireTimeout 并发闸获取超时（I24）：可测试覆盖。
// atomic.Int64（纳秒）：测试会与在途 execute goroutine 并发改写（-race 检出裸包级变量竞争）。
var gateAcquireTimeout atomic.Int64

func init() { gateAcquireTimeout.Store(int64(60 * time.Second)) }

// spillThreshold 上下文 Spill 阈值（rune 数）：超过则 handoff 时压缩 prompt（F11-b）。
const spillThreshold = 1500

func runeLen(s string) int { return len([]rune(s)) }

// spillIfNeeded 上下文 Spill：超阈值压缩为「原始诉求 + 上一结论摘要 + run 引用」；
// 完整历史在 trace 按需链式召回。短 prompt 原样返回（不压缩）。
func spillIfNeeded(composed, title string, handoffCount int, runID int64, lastNote string) string {
	if runeLen(composed) <= spillThreshold {
		return composed
	}
	if runeLen(lastNote) > 200 {
		lastNote = string([]rune(lastNote)[:200]) + "…"
	}
	return fmt.Sprintf(
		"[任务背景（已压缩，完整历史见 run#%d 及此前 trace）]\n原始诉求：%s\n历经 %d 次交接；上一结论：%s\n[本轮职责：完成该任务的当前阶段]",
		runID, title, handoffCount, lastNote)
}

// TaskTier 任务级路由档位（F18-b，ADR-0002）。
type TaskTier string

const (
	TierS TaskTier = "S" // 简单任务：小模型执行
	TierM TaskTier = "M" // 标准任务：主力模型
	TierL TaskTier = "L" // 复杂/高风险：最强模型
)

// ScoreTier 任务级打分器（纯规则，零模型调用——可解释可调试）：
// 基础 M；加权信号升/降档。信号全部来自已有数据（priority/长度/handoff 指令/近期成功率）。
func ScoreTier(title, prompt string, priority int, successRate float64, recentRuns int) TaskTier {
	if recentRuns >= 3 && successRate < 0.5 {
		return TierL // 近期表现差的任务交给最强模型（质量问题先升级）
	}
	score := 0
	if priority >= 8 {
		score += 2 // 高优任务质量优先
	}
	if runeLen(prompt) > 2000 {
		score += 1 // 长任务说明复杂度高
	}
	if strings.Contains(prompt, "审批") || strings.Contains(title, "发布") {
		score += 1 // 涉及审批/发布的任务质量优先
	}
	if strings.Contains(prompt, "handoff:") {
		score += 1 // 协作任务链路长
	}
	switch {
	case score >= 3:
		return TierL
	case score == 0 && priority <= 3 && runeLen(prompt) < 200:
		return TierS // 低优短任务才降档到 S
	default:
		return TierM
	}
}

// TierToModel 档位 → 执行模型（经 gateway 分流；S 档执行用小模型，L 档用最强可用）。
func (s *Scheduler) TierToModel(tier TaskTier, fallback string) string {
	if s.llm == nil {
		return fallback
	}
	switch tier {
	case TierS:
		if m := s.llm.SelectFor(gateway.PurposeJudge); m != "" {
			return m
		}
	case TierL:
		if m := s.llm.SelectFor(gateway.PurposeReview); m != "" {
			return m
		}
	}
	return fallback // M 档 = agent 自带模型
}

type Scheduler struct {
	store        store.Store
	events       *Hub
	judge        LLMJudge     // 进展看门狗用（nil 或不健康时 fail-open）
	llm          *gateway.LLM // ADR-0002 分流：档位 → 模型解析
	mu           sync.Mutex
	semaphores   map[int64]chan struct{} // agent_id -> 并发闸
	wg           sync.WaitGroup
	cancel       context.CancelFunc
	running      map[int64]bool // task_id -> 执行中（防同任务重复派发）
	lastWatchdog time.Time
	wake         chan struct{} // 事件驱动唤醒（F13-a）：建任务即时派发，不等 tick
}

func New(st store.Store, hub *Hub) *Scheduler {
	return &Scheduler{store: st, events: hub, semaphores: map[int64]chan struct{}{},
		running: map[int64]bool{}, wake: make(chan struct{}, 1)}
}

// SetLLM 注入网关（F18-b 任务级分流用）。
func (s *Scheduler) SetLLM(l *gateway.LLM) { s.llm = l }

// Wake 事件驱动唤醒（非阻塞；已有待处理唤醒时合并）。建任务/审批放行等状态变化后调用。
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default: // 已有 pending 唤醒，合并（tick 兜底保证不丢）
	}
}

// SetJudge 注入进展看门狗的判定模型（gateway.LLM 天然满足该接口）。
func (s *Scheduler) SetJudge(j LLMJudge) { s.judge = j }

func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(pollInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.wake: // 事件驱动：建任务/放行等即时派发（F13-a）
				s.sweepDependents(ctx)
				s.sweepBudgets(ctx)
				s.maybeWatchdog(ctx)
				s.runAutomations(ctx)
				s.dispatch(ctx)
			case <-tick.C:
				s.sweepDependents(ctx)
				s.sweepBudgets(ctx)
				s.maybeWatchdog(ctx)
				s.runAutomations(ctx)
				s.dispatch(ctx)
			}
		}
	}()
	slog.Info("scheduler started", "poll_interval", pollInterval)
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	inflight := len(s.running)
	s.mu.Unlock()
	if inflight > 0 {
		slog.Info("scheduler draining", "inflight", inflight)
	}
	s.wg.Wait()
	slog.Info("scheduler stopped")
}

func (s *Scheduler) Healthy() bool { return s.cancel != nil }

// dispatch 每轮尽力认领（一次认领一个，多任务靠下一轮 tick 自然吞吐）。
func (s *Scheduler) dispatch(ctx context.Context) {
	task, err := s.store.ClaimNextTask(ctx)
	if err != nil {
		slog.Error("claim failed", "err", err)
		return
	}
	if task == nil {
		return
	}
	s.mu.Lock()
	if s.running[task.ID] {
		s.mu.Unlock()
		return
	}
	s.running[task.ID] = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.running, task.ID)
			s.mu.Unlock()
		}()
		// 执行用独立 ctx（与轮询 ctx 解耦）：drain 时停认领，但在途任务必须跑完（含 LLM 长调用）
		execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.execute(execCtx, *task)
	}()
}

func (s *Scheduler) gate(agentID int64, n int) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.semaphores[agentID]; !ok {
		if n <= 0 {
			n = 1
		}
		s.semaphores[agentID] = make(chan struct{}, n)
	}
	return s.semaphores[agentID]
}

func (s *Scheduler) execute(ctx context.Context, task model.Task) {
	originalAgent := task.AgentID // I30：handoff 前的 agent（squad 场景=初始 leader）
	setOriginalAgent(task.ID, originalAgent)
	agent, err := s.store.GetAgent(ctx, task.AgentID)
	if err != nil || !agent.Enabled {
		s.failTask(ctx, &task, fmt.Sprintf("agent 不可用: %v", err))
		return
	}
	g := s.gate(agent.ID, agent.MaxConcurrency)
	select {
	case g <- struct{}{}:
		defer func() { <-g }()
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(gateAcquireTimeout.Load())):
		// I24 并发闸超时：runner 卡死不再无限占闸——任务带延迟重排队 + 告警事件
		// （与 F14 看门狗互补：看门狗管已执行任务的进展，这里管还没拿到闸的）
		visible := timeNow().Add(retryBackoff * 2)
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskQueued), VisibleAfter: &visible,
			Error: ptrStr("并发闸等待超时（agent 疑似卡死），已延迟重排队")})
		s.emit(ctx, "agent.gate_timeout", map[string]any{
			"task_id": task.ID, "agent": agent.Name, "retry_after_s": (retryBackoff * 2).Seconds()})
		slog.Warn("并发闸等待超时，延迟重排队", "task_id", task.ID, "timeout", time.Duration(gateAcquireTimeout.Load()))
		return
	}

	rn, err := runner.Get(agent.Runner)
	if err != nil {
		s.failTask(ctx, &task, err.Error())
		return
	}

	// 执行环境绑定（workspace）与经验注入（skills）：注入只发生在给 runner 的副本上，
	// 不改任务本体 prompt——skill 内容是「参考资料」不是任务指令（指令通道纯净性）
	execPrompt := task.Prompt
	var ws *model.Workspace
	if task.WorkspaceID > 0 {
		if w, err := s.store.GetWorkspace(ctx, task.WorkspaceID); err == nil {
			// I16 EnsureReady：执行环境 degraded → 拒绝执行（不烧模型算力），错误带探测原因
			if w.RuntimeStatus == "degraded" {
				reason := w.RuntimeNote
				if reason == "" {
					reason = "未通过运行时探测"
				}
				s.failTask(ctx, &task, "执行环境不可用: "+reason)
				return
			}
			ws = w
			execPrompt += fmt.Sprintf("\n\n[执行环境] %s（分支 %s，路径 %s）", w.Name, w.Branch, w.RootPath)
		}
	}
	// ADR-0003 M1：长期记忆确定性注入——active 记忆按 created_at 升序（旧事实在前），
	// 预算 ≤10 条 + ≤2000 字符，超限截断最近的优先（倒序取再反转）
	var memLines []string
	if mems, err := s.store.ListMemories(ctx, task.AgentID, []string{"active"}); err == nil && len(mems) > 0 {
		const memMaxCount, memMaxChars = 10, 2000
		start := 0
		if len(mems) > memMaxCount {
			start = len(mems) - memMaxCount
		}
		charBudget := memMaxChars
		for _, mem := range mems[start:] {
			line := "- " + mem.Content
			if len([]rune(line)) > charBudget {
				break
			}
			memLines = append(memLines, line)
			charBudget -= len([]rune(line))
		}
		if len(memLines) > 0 {
			execPrompt += "\n\n[已知背景（长期记忆）]\n" + strings.Join(memLines, "\n")
		}
	}
	var skills []model.Skill
	if len(task.SkillIDs) > 0 {
		var sb strings.Builder
		for _, sid := range task.SkillIDs {
			if sk, err := s.store.GetSkill(ctx, sid); err == nil {
				skills = append(skills, *sk)
				sb.WriteString(fmt.Sprintf("\n\n[经验参考：%s]\n%s", sk.Name, sk.Content))
				_ = s.store.IncrSkillUsage(ctx, sid)
			}
		}
		execPrompt += sb.String()
	}
	// I38 灰度：ramp_pct>0 时按 taskID 分桶——ramping 桶用灰度版本快照覆盖 agent 副本
	if agent.RampPct > 0 && agent.RampingVersion > 0 {
		curV := 0
		if vers, _ := s.store.ListAgentVersions(ctx, agent.ID); len(vers) > 0 {
			for _, av := range vers {
				if av.Version > curV {
					curV = av.Version
				}
			}
		}
		if v := routeVersion(task.ID, curV, agent.RampingVersion, agent.RampPct); v == agent.RampingVersion {
			if snap, err := s.store.GetAgentVersion(ctx, agent.ID, v); err == nil {
				applyVersionSnapshot(agent, snap.Config)
			}
		}
	}
	execTask := task
	execTask.Prompt = renderTemplate(execPrompt, &task, ws, agent.Name)

	// I11 结果重放：重试（RetryCount>0）时携带上一次 run 的确定性 tool 步观察——
	// 命中则不重复调用 MCP（Temporal 结果重放语义）；replay 端点清零重试 → 不带（完全重跑）。
	// 跨版本拒绝：agent 配置版本在上次 run 之后发生变化 → 旧观察不可信，拒绝重放并提示重新评估。
	var replayTools []runner.ReplayTool
	var replayResume string
	if task.RetryCount > 0 {
		if runs, _ := s.store.ListRuns(ctx, task.ID); len(runs) > 0 {
			last := runs[0]
			var curVersion int
			if vers, _ := s.store.ListAgentVersions(ctx, agent.ID); len(vers) > 0 {
				// 显式取最大版本（pg 按 version DESC 返回，mem 按 ASC——不依赖排序约定）
				for _, v := range vers {
					if v.Version > curVersion {
						curVersion = v.Version
					}
				}
			}
			if last.AgentVersion > 0 && curVersion > 0 && last.AgentVersion != curVersion {
				s.emit(ctx, "task.replay_rejected", map[string]any{
					"task_id": task.ID, "run_id": last.ID,
					"run_version": last.AgentVersion, "current_version": curVersion,
					"hint": "agent 配置已变更，重放观察不可信——本次完全重跑，请重新评估结果"})
				slog.Warn("重放拒绝：版本变化，完全重跑", "task_id", task.ID, "run_id", last.ID,
					"run_version", last.AgentVersion, "current_version", curVersion)
			} else if last.SessionID != "" {
				// I34 会话级续跑：版本一致时携带 CC session（与步级重放互补；变化时一并拒绝）
				replayResume = last.SessionID
			} else if steps, _ := s.store.ListSteps(ctx, last.ID); len(steps) > 0 {
				for _, st := range steps {
					if st.Deterministic && st.Name != "" && st.Output != nil {
						// 全量 result 优先（I11 起落库），旧步回退 result_head
						result, _ := st.Output["result"].(string)
						if result == "" {
							result, _ = st.Output["result_head"].(string)
						}
						if result != "" {
							replayTools = append(replayTools, runner.ReplayTool{
								Ref:      st.Name,
								ArgsJSON: mustJSONStep(st.Input),
								Result:   result,
							})
						}
					}
				}
			}
		}
	}
	run := &model.Run{TaskID: task.ID, AgentID: agent.ID}
	// I17 handoff 链：同任务第 2+ 次 run 指向前一 run（瀑布图跨 run 成链）
	if task.HandoffCount > 0 {
		if prev, _ := s.store.ListRuns(ctx, task.ID); len(prev) > 0 {
			run.ParentRunID = prev[0].ID
		}
	}
	// I11：执行时 agent 版本进 run（跨版本续跑拒绝的数据依据）
	if vers, _ := s.store.ListAgentVersions(ctx, agent.ID); len(vers) > 0 {
		run.AgentVersion = vers[len(vers)-1].Version
	}
	runID, err := s.store.CreateRun(ctx, run)
	if err != nil {
		s.failTask(ctx, &task, "创建 run 失败: "+err.Error())
		return
	}
	s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskRunning), "run_id": runID, "agent": agent.Name})
	slog.Info("task dispatched", "task_id", task.ID, "title", task.Title,
		"agent", agent.Name, "runner", agent.Runner, "run_id", runID)

	recorder := func(_ context.Context, st *model.RunStep) error {
		_, err := s.store.AppendStep(ctx, runID, st)
		if err == nil {
			s.emit(ctx, "run.step", map[string]any{"run_id": runID, "task_id": task.ID, "seq": st.Seq,
				"kind": string(st.Kind), "name": st.Name, "latency_ms": st.LatencyMS, "tokens": st.Tokens})
		}
		return err
	}

	// F18-b 任务级分流：纯规则打分选档，失败重试自动升档（escalation）；模型覆盖落 trace
	tier := ScoreTier(task.Title, execTask.Prompt, task.Priority, 1.0, 0)
	if task.RetryCount > 0 {
		tier = TierL
	}
	modelOverride := s.TierToModel(tier, agent.Model)
	routedModel := agent.Model
	if modelOverride != agent.Model {
		routedModel = modelOverride
	}
	routeInput := map[string]any{"tier": string(tier), "model": routedModel, "retry": task.RetryCount}
	if s.llm != nil {
		if cooling := s.llm.CoolingList(); len(cooling) > 0 {
			routeInput["cooling_models"] = cooling // I35：冷却中模型观测
		}
	}
	_, _ = s.store.AppendStep(ctx, runID, &model.RunStep{Kind: model.StepLLM, Name: "route",
		Input: routeInput,
		Note:  "F18-b 任务级路由"})

	// I30 小队模式：任务分给 squad 时，leader 的 system prompt 追加小队 briefing
	// （协议 + Roster + 指令）——leader 用 handoff_to 转派，成员完成后事件 re-trigger。
	if task.SquadID > 0 {
		if squad, err := s.store.GetSquad(ctx, task.SquadID); err == nil {
			agent.SystemPrompt += "\n\n" + buildSquadBriefing(ctx, s.store, squad)
			slog.Info("小队模式任务", "task_id", task.ID, "leader", agent.Name, "squad_id", squad.ID, "squad_name", squad.Name)
		}
	}

	// I1 取消传播：CancelCheck 注入（DB 重取任务状态——认领与取消的竞态由二次确认兜底）
	cancelCheck := func() bool {
		fresh, err := s.store.GetTask(context.Background(), task.ID)
		return err != nil || fresh.Status != model.TaskRunning
	}
	// Multica Steer 对标：取出一次性插话并清空（原子读改）
	steerDrain := func() string {
		fresh, err := s.store.GetTask(context.Background(), task.ID)
		if err != nil || fresh.SteerNote == "" {
			return ""
		}
		note := fresh.SteerNote
		empty := ""
		_ = s.store.UpdateTask(context.Background(), task.ID, store.TaskPatch{SteerNote: &empty})
		return note
	}
	res, execErr := rn.Execute(ctx, runner.ExecRequest{Agent: *agent, Task: execTask, Recorder: recorder, ReplayTools: replayTools, ResumeSession: replayResume,
		Workspace: ws, Skills: skills, ModelOverride: routedModel,
		CancelCheck: cancelCheck, SteerDrain: steerDrain})

	// I34：外部 agent 会话 ID 与自报成本落库（session 续跑依据 / F10 直连）
	if res.SessionID != "" {
		_ = s.store.SetRunSession(ctx, runID, res.SessionID)
	}
	// 结构化产物落库（artifacts：报告/补丁/执行记录）
	for _, d := range res.Artifacts {
		_, _ = s.store.SaveArtifact(ctx, &model.Artifact{TaskID: task.ID, RunID: runID,
			Kind: d.Kind, Name: d.Name, MimeType: mimeOf(d.Kind), Content: d.Content})
	}
	if len(res.Artifacts) > 0 {
		s.emit(ctx, "task.artifacts", map[string]any{"task_id": task.ID, "count": len(res.Artifacts)})
	}

	fin := timeNow()
	// 从 store 取回最新 run（token 累加发生在 AppendStep 的存储侧，本地副本是旧值）
	if fresh, err := s.store.GetRun(ctx, runID); err == nil && fresh != nil {
		run = fresh
	}
	run.Status, run.FinishedAt = model.RunDone, &fin
	if execErr != nil {
		run.Status, run.Error = model.RunFailed, execErr.Error()
	}
	_ = s.store.UpdateRun(ctx, run)
	// 用量回填：run 的 tokens 进 usage_events（预算熔断与用量看板的数据源）
	if run.TotalTokens > 0 {
		_ = s.store.AppendUsage(ctx, &model.UsageEvent{AgentID: agent.ID, AgentName: agent.Name,
			Model: agent.Model, Purpose: "execute", CompletionTokens: run.TotalTokens, Requests: 1})
	}

	switch {
	case errors.Is(execErr, runner.ErrCanceled):
		// I1 协作式取消：任务已被 cancel 端点置为 canceled（终态），此处只收尾 run
		run.Status = model.RunFailed
		run.Error = "canceled: 协作式取消退出"
		_ = s.store.UpdateRun(ctx, run)
		slog.Info("协作式取消退出（不重试）", "task_id", task.ID)
	case execErr != nil:
		s.failTask(ctx, &task, execErr.Error())
	case res.NeedsApproval || res.RiskLevel == "high":
		// I29 风险分级：medium=人审放行即过；high=阻断+强制审批意见
		risk := res.RiskLevel
		if risk == "" {
			risk = "medium"
		}
		note := "人工审批门（风险 " + risk + "）：" + res.Note
		_, _ = s.store.AppendStep(ctx, runID, &model.RunStep{Kind: model.StepGate, Name: "waiting-approval",
			Note: note, Input: map[string]any{"risk_level": risk}})
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskReview), Result: ptrStr(res.Reply), Error: ptrStr(note),
			RiskLevel: ptrStr(risk), FinishedAt: &fin})
		s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskReview),
			"run_id": runID, "risk_level": risk})
		slog.Info("等待人工审批", "task_id", task.ID, "risk", risk)
	case res.HandoffTo != "":
		s.handoff(ctx, &task, runID, res)
	default:
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskDone), Result: ptrStr(res.Reply), Error: ptrStr(""),
			FinishedAt: &fin})
		s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskDone), "run_id": runID})
		slog.Info("task done", "task_id", task.ID)

		// I30 小队闭环：成员（handoff 过来的）完成 → 自动 re-trigger leader 评估下一步。
		// 实现轻量版：squad 任务的 handoff 链在成员 done 后，为 leader 建后续评估任务
		// （带 handoff 上下文，leader 重新决策：继续转派 / 上报 / 收尾）。
		if task.SquadID > 0 && task.AgentID != originalAgentID(task) {
			if squad, err := s.store.GetSquad(ctx, task.SquadID); err == nil && squad.LeaderID != task.AgentID {
				followUp := &model.Task{
					Title: fmt.Sprintf("[小队跟进] %s（成员已完成）", task.Title),
					Prompt: fmt.Sprintf("小队成员已完成子任务「%s」，产出：%s\n\n请评估：是否继续转派下一步、上报人工、还是收尾？",
						task.Title, headStr(res.Reply, 300)),
					AgentID: squad.LeaderID, Priority: task.Priority, SquadID: task.SquadID,
				}
				if nid, err := s.store.CreateTask(ctx, followUp); err == nil {
					s.emit(ctx, "task.update", map[string]any{"task_id": nid, "status": "queued", "squad_followup": task.ID})
					slog.Info("小队跟进评估任务生成", "member_task_id", task.ID, "leader_task_id", nid)
					s.Wake()
				}
			}
		}
	}
}

// handoff 把任务改派给目标 agent 重新排队（记录 handoff 步骤与次数上限防环路）。
func (s *Scheduler) handoff(ctx context.Context, task *model.Task, runID int64, res runner.Result) {
	agents, err := s.store.ListAgents(ctx, store.AgentFilter{EnabledOnly: true})
	if err != nil {
		s.failTask(ctx, task, "查询 agent 失败: "+err.Error())
		return
	}
	var target *model.Agent
	for i := range agents {
		if agents[i].Name == res.HandoffTo {
			target = &agents[i]
			break
		}
	}
	if target == nil {
		s.failTask(ctx, task, fmt.Sprintf("handoff 目标 agent %q 不存在", res.HandoffTo))
		return
	}
	// 自交防护：handoff 给自己 = 当前 agent 已完成份内工作，用其产出收尾而不是死循环
	if target.ID == task.AgentID {
		_, _ = s.store.AppendStep(ctx, runID, &model.RunStep{Kind: model.StepHandoff, Name: "handoff-self",
			Note: "交接目标为自身，按当前产出收尾"})
		fin := timeNow()
		reply := res.Reply
		if reply == "" {
			reply = "处理完成（交接目标为自身，无进一步动作）"
		}
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskDone), Result: ptrStr(reply), FinishedAt: &fin})
		s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskDone), "note": "handoff-self"})
		return
	}
	if task.HandoffCount+1 > task.MaxHandoffs || task.HandoffCount+1 > maxHandoffs {
		_, _ = s.store.AppendStep(ctx, runID, &model.RunStep{Kind: model.StepHandoff, Name: "handoff-limit",
			Note: fmt.Sprintf("交接次数超限(%d)，任务结束", task.HandoffCount)})
		fin := timeNow()
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskFailed), Error: ptrStr("handoff 超限: " + res.Note), FinishedAt: &fin})
		s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskFailed)})
		return
	}
	newCount := task.HandoffCount + 1
	// mock 已记录 handoff 步骤（trace 不重复）；scheduler 只负责改派与指令消费
	// 指令一次性消费：在「原 prompt + 交接备注」的最终文本上剥离全部指令词形态，
	// 防止备注通道把指令词再泄漏给接收方（曾导致 fixer→reviewer 连环误交接）
	composed := task.Prompt + "\n\n[handoff #" + fmt.Sprint(newCount) + " 来自 agent#" +
		fmt.Sprint(task.AgentID) + "] " + res.Note
	for _, trigger := range []string{"handoff:" + target.Name, "handoff:" + res.HandoffTo, "交接给" + target.Name} {
		composed = strings.ReplaceAll(composed, trigger, fmt.Sprintf("（已由 %s 接手）", target.Name))
	}
	// 上下文 Spill（F11-b，借鉴 dsh-superagent）：prompt 超阈值时压缩为
	// 「原始诉求一行 + 上一结论摘要 + run 引用」；完整历史在 trace，按 ID 链式召回——
	// prompt 是工作记忆不是归档，多次交接后不再线性膨胀
	composed = spillIfNeeded(composed, task.Title, newCount, runID, res.Note)
	_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
		AgentID: ptrI64(target.ID), Status: ptrStatus(model.TaskQueued),
		HandoffCount: ptrInt(newCount), Prompt: ptrStr(composed)})
	s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": "queued", "handoff_to": target.Name})
	slog.Info("handoff 改派", "task_id", task.ID, "to", target.Name, "count", newCount)
}

// sweepDependents 依赖失败级联：依赖已 failed/canceled 的排队任务不再死等，直接判败。
func (s *Scheduler) sweepDependents(ctx context.Context) {
	queued := model.TaskQueued
	tasks, _, err := s.store.ListTasks(ctx, &queued, 0, 1, 100)
	if err != nil {
		return
	}
	for _, t := range tasks {
		if len(t.DependsOn) == 0 {
			continue
		}
		for _, dep := range t.DependsOn {
			d, err := s.store.GetTask(ctx, dep)
			if err != nil {
				continue
			}
			if d.Status == model.TaskFailed || d.Status == model.TaskCanceled {
				fin := timeNow()
				_ = s.store.UpdateTask(ctx, t.ID, store.TaskPatch{
					Status:     ptrStatus(model.TaskFailed),
					Error:      ptrStr(fmt.Sprintf("前置任务 #%d %s，本任务级联终止", dep, d.Status)),
					FinishedAt: &fin})
				s.emit(ctx, "task.update", map[string]any{"task_id": t.ID, "status": string(model.TaskFailed), "cascade": dep})
				slog.Warn("依赖失败级联", "task_id", t.ID, "dep_task_id", dep, "dep_status", d.Status)
				break
			}
		}
	}
}

func (s *Scheduler) failTask(ctx context.Context, task *model.Task, msg string) {
	if task.RetryCount < task.MaxRetries {
		newRetry := task.RetryCount + 1
		visible := timeNow().Add(retryBackoff * time.Duration(newRetry))
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskQueued), RetryCount: ptrInt(newRetry),
			Error: ptrStr(msg), VisibleAfter: &visible})
		s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": "queued",
			"retry": newRetry, "error": msg})
		slog.Warn("任务失败将重试", "task_id", task.ID, "retry", newRetry, "max_retries", task.MaxRetries, "err", msg)
		return
	}
	fin := timeNow()
	_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
		Status: ptrStatus(model.TaskFailed), Error: ptrStr(msg), FinishedAt: &fin})
	s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskFailed), "error": msg})
	slog.Error("task failed", "task_id", task.ID, "err", msg)
}

func (s *Scheduler) emit(ctx context.Context, kind string, data map[string]any) {
	if s.events != nil {
		s.events.Publish(ctx, kind, data)
	}
}

// applyVersionSnapshot 灰度版本快照覆盖 agent 执行副本（I38：system_prompt/model）。
func applyVersionSnapshot(a *model.Agent, cfg map[string]any) {
	if v, ok := cfg["system_prompt"].(string); ok {
		a.SystemPrompt = v
	}
	if v, ok := cfg["model"].(string); ok && v != "" {
		a.Model = v
	}
}

// routeVersion I38 灰度分桶：taskID 确定性映射（fnv32）→ [0,100) 与 rampPct 比较——
// 同任务判定恒一致（Temporal Ramping 同语义）；rampPct<=0 或 rampingVersion<=0 → current。
func routeVersion(taskID int64, currentVersion, rampingVersion, rampPct int) int {
	if rampingVersion <= 0 || rampPct <= 0 {
		return currentVersion
	}
	h := fnv.New32a()
	fmt.Fprintf(h, "%d:%d", taskID, rampingVersion) // 掺 rampingVersion：同任务不同灰度轮分桶可变
	if int(h.Sum32()%100) < rampPct {
		return rampingVersion
	}
	return currentVersion
}

// mustJSONStep 步 Input 重建参数 JSON（与 runner mustJSON 同序——均 json.Marshal map）
func mustJSONStep(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---- 小工具 ----

// mimeOf 产物类型 → MIME。
func mimeOf(kind string) string {
	switch kind {
	case "report", "log", "diff", "patch":
		return "text/plain; charset=utf-8"
	case "data":
		return "application/json"
	default:
		return "text/plain"
	}
}

func ptrStr(s string) *string                        { return &s }
func ptrInt(i int) *int                              { return &i }
func ptrI64(i int64) *int64                          { return &i }
func ptrStatus(s model.TaskStatus) *model.TaskStatus { return &s }
func ptrTime(t time.Time) *time.Time                 { return &t }
