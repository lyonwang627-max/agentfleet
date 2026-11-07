// Package scheduler 任务调度器：
//   - 认领：ClaimNextTask（pg 实现为 FOR UPDATE SKIP LOCKED，多实例安全）
//   - 执行：按 agent 的 max_concurrency 限流，runner 落 step（trace）
//   - 状态机：queued→running→done/failed/review；handoff 改派；重试退避
//   - 事件：状态变化与 step 均发 SSE（经 events.Hub 持久化+广播）
package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
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
var gateAcquireTimeout = 60 * time.Second

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
	log.Printf("[scheduler] started (poll=%s)", pollInterval)
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	inflight := len(s.running)
	s.mu.Unlock()
	if inflight > 0 {
		log.Printf("[scheduler] drain：停止认领，等待 %d 个在途任务完成…", inflight)
	}
	s.wg.Wait()
	log.Printf("[scheduler] 已停止（在途任务全部落定）")
}

func (s *Scheduler) Healthy() bool { return s.cancel != nil }

// dispatch 每轮尽力认领（一次认领一个，多任务靠下一轮 tick 自然吞吐）。
func (s *Scheduler) dispatch(ctx context.Context) {
	task, err := s.store.ClaimNextTask(ctx)
	if err != nil {
		log.Printf("[scheduler] claim: %v", err)
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
	case <-time.After(gateAcquireTimeout):
		// I24 并发闸超时：runner 卡死不再无限占闸——任务带延迟重排队 + 告警事件
		// （与 F14 看门狗互补：看门狗管已执行任务的进展，这里管还没拿到闸的）
		visible := timeNow().Add(retryBackoff * 2)
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskQueued), VisibleAfter: &visible,
			Error: ptrStr("并发闸等待超时（agent 疑似卡死），已延迟重排队")})
		s.emit(ctx, "agent.gate_timeout", map[string]any{
			"task_id": task.ID, "agent": agent.Name, "retry_after_s": (retryBackoff * 2).Seconds()})
		log.Printf("[scheduler] task#%d 并发闸等待超时（%s），延迟重排队", task.ID, gateAcquireTimeout)
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
			ws = w
			execPrompt += fmt.Sprintf("\n\n[执行环境] %s（分支 %s，路径 %s）", w.Name, w.Branch, w.RootPath)
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
	execTask := task
	execTask.Prompt = execPrompt

	run := &model.Run{TaskID: task.ID, AgentID: agent.ID}
	runID, err := s.store.CreateRun(ctx, run)
	if err != nil {
		s.failTask(ctx, &task, "创建 run 失败: "+err.Error())
		return
	}
	s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskRunning), "run_id": runID, "agent": agent.Name})
	log.Printf("[scheduler] task#%d 「%s」→ %s/%s (run#%d)", task.ID, task.Title, agent.Name, agent.Runner, runID)

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
	_, _ = s.store.AppendStep(ctx, runID, &model.RunStep{Kind: model.StepLLM, Name: "route",
		Input: map[string]any{"tier": string(tier), "model": routedModel, "retry": task.RetryCount},
		Note:  "F18-b 任务级路由"})

	res, execErr := rn.Execute(ctx, runner.ExecRequest{Agent: *agent, Task: execTask, Recorder: recorder,
		Workspace: ws, Skills: skills, ModelOverride: routedModel})

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
	case execErr != nil:
		s.failTask(ctx, &task, execErr.Error())
	case res.NeedsApproval:
		note := "人工审批门：" + res.Note
		_, _ = s.store.AppendStep(ctx, runID, &model.RunStep{Kind: model.StepGate, Name: "waiting-approval", Note: res.Note})
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskReview), Result: ptrStr(res.Reply), Error: ptrStr(note),
			FinishedAt: &fin})
		s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskReview), "run_id": runID})
		log.Printf("[scheduler] task#%d 等待人工审批", task.ID)
	case res.HandoffTo != "":
		s.handoff(ctx, &task, runID, res)
	default:
		_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
			Status: ptrStatus(model.TaskDone), Result: ptrStr(res.Reply), Error: ptrStr(""),
			FinishedAt: &fin})
		s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskDone), "run_id": runID})
		log.Printf("[scheduler] task#%d done", task.ID)
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
	log.Printf("[scheduler] task#%d handoff → %s（第 %d 次）", task.ID, target.Name, newCount)
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
				log.Printf("[scheduler] task#%d 因依赖 #%d %s 级联失败", t.ID, dep, d.Status)
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
		log.Printf("[scheduler] task#%d 失败将重试(%d/%d): %s", task.ID, newRetry, task.MaxRetries, msg)
		return
	}
	fin := timeNow()
	_ = s.store.UpdateTask(ctx, task.ID, store.TaskPatch{
		Status: ptrStatus(model.TaskFailed), Error: ptrStr(msg), FinishedAt: &fin})
	s.emit(ctx, "task.update", map[string]any{"task_id": task.ID, "status": string(model.TaskFailed), "error": msg})
	log.Printf("[scheduler] task#%d failed: %s", task.ID, msg)
}

func (s *Scheduler) emit(ctx context.Context, kind string, data map[string]any) {
	if s.events != nil {
		s.events.Publish(ctx, kind, data)
	}
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
