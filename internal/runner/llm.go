package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
)

// timeNow 可替换时钟（测试用）。
var timeNow = time.Now

// LLMRunner 本地模型执行器：JSON 协议一次决策（reply / handoff / 审批门）。
// 结构化输出走 Ollama 原生通道（think:false + format:"json"），解析失败降级为纯文本 reply。
type LLMRunner struct {
	LLM *gateway.LLM
}

func NewLLM(llm *gateway.LLM) *LLMRunner { return &LLMRunner{LLM: llm} }

type decision struct {
	Reply         string         `json:"reply"`
	HandoffTo     string         `json:"handoff_to"`
	NeedsApproval bool           `json:"needs_approval"`
	Note          string         `json:"note"`
	Tool          string         `json:"tool"`      // 非空 = 请求调用工具（F11-a 多轮循环）
	ToolArgs      map[string]any `json:"tool_args"` // 工具参数（如 {"msg": "…"}）
}

const maxToolSteps = 4 // 多轮工具循环上限（防上下文膨胀与死循环）

// decisionSchema 决策协议 schema（I42）：ChatJSON strict 级——schema 违反结构性不可能。
// 兼容 Ollama 旧版：不支持对象 format 时由服务端自行回退。
var decisionSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "reply":           {"type": "string"},
    "handoff_to":      {"type": "string"},
    "needs_approval":  {"type": "boolean"},
    "note":            {"type": "string"},
    "tool":            {"type": "string"},
    "tool_args":       {"type": "object"}
  },
  "required": ["reply"]
}`)

// ToolGateError 工具级审批中断（I33）：高风险工具调用在执行前暂停，任务进 review 门。
// approve 后审批记录（含工具名）注入 task prompt，重跑时闸放行（与 F3 审批记录消费同机制）。
type ToolGateError struct {
	Ref  string
	Args map[string]any
}

func (e *ToolGateError) Error() string { return "工具级审批: " + e.Ref }

// highRiskTool 工具风险关键词（I29 同源：对外发布/删除/资金/部署）。
func highRiskTool(ref string) bool {
	l := strings.ToLower(ref)
	for _, k := range []string{"发布", "publish", "删除", "delete", "drop", "资金", "payment", "transfer", "deploy", "退款"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

// toolApproved 审批记录是否已覆盖该工具（approve note 需提及工具名——防止一次审批放行所有工具）。
func toolApproved(prompt, ref string) bool {
	tool := ref
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		tool = ref[i+1:]
	}
	return strings.Contains(prompt, "[审批记录]") && strings.Contains(prompt, tool)
}

// runToolLoop 多轮工具循环（F11-a）：决策 →（要工具则执行→观察→再决策）→ … → 最终决策。
// 抽成纯函数：decide/callTool 由调用方注入（生产=网关+MCP 注册中心，测试=fake）。
// 返回最终决策、完整观察轨迹、实际步数；步数耗尽用最后一次决策收尾（不失败——已有信息仍可用）。
func runToolLoop(
	baseSystem, baseUser string,
	decide func(system, user string) (decision, error),
	callTool func(ref string, args map[string]any) (string, error),
	onToolTurn func(turn int, d decision, observation string),
) (decision, []string, int, error) {
	user := baseUser
	observations := []string{}
	var last decision
	for step := 1; step <= maxToolSteps; step++ {
		d, err := decide(baseSystem, user)
		if err != nil {
			return last, observations, step, err
		}
		last = d
		if d.Tool == "" {
			return d, observations, step, nil
		}
		out, toolErr := callTool(d.Tool, d.ToolArgs)
		var gateErr *ToolGateError
		if toolErr != nil && errors.As(toolErr, &gateErr) {
			// I33 工具闸：中止循环上抛，由 Execute 映射为审批门（不是失败）
			return last, observations, step, toolErr
		}
		observation := out
		if toolErr != nil {
			observation = "工具执行失败: " + toolErr.Error()
		}
		observations = append(observations, observation)
		if onToolTurn != nil {
			onToolTurn(step, d, observation)
		}
		user = baseUser + "\n\n---\n已执行工具 " + d.Tool + "，观察结果：\n" + head(observation, 800) +
			"\n\n请基于以上观察继续：可再调用工具，或给出最终 reply（此时 tool 留空）。"
	}
	// 步数耗尽：强制收尾（最后一次决策已有 reply 则用之，否则带说明）
	if last.Reply == "" {
		last.Reply = "（已达工具调用步数上限，基于已有观察收尾）"
	}
	return last, observations, maxToolSteps, nil
}

func (r *LLMRunner) Execute(ctx context.Context, req ExecRequest) (Result, error) {
	totalTokens := 0 // 多轮工具循环的 token 合计（本 run 内）
	// F18-b 任务级路由：调度器按档位覆盖模型（空=agent 自带模型）
	execLLM := r.LLM
	if req.ModelOverride != "" {
		execLLM = r.LLM.WithModel(req.ModelOverride)
	}
	// emit 返回落库后的步（含 AppendStep 回填的 Seq——I17 层级挂接用）；Recorder 为 nil 时返回 nil
	emit := func(kind model.StepKind, name string, in, out map[string]any, latencyMS int64, tokens int) *model.RunStep {
		if req.Recorder == nil {
			return nil
		}
		st := &model.RunStep{Kind: kind, Name: name, Input: in, Output: out,
			LatencyMS: latencyMS, Tokens: tokens}
		_ = req.Recorder(ctx, st)
		return st
	}

	system := buildSystemPrompt(req.Agent) + `

工具协作协议：若需要外部信息或动作，可先输出 {"tool": "mcp:<server>/<tool>", "tool_args": {...}, "reply": ""} 请求调用工具；` +
		"系统会执行并把观察结果回给你（最多 " + itoa(maxToolSteps) + " 轮）。拿到足够信息后输出最终决策（tool 留空）。"

	// I28 工具能力清单：decide 提示注入「可选工具+描述」，LLM 按能力选工具（非盲选协议）
	catalog := UnifiedTools().Catalog()
	toolSection := ""
	if catalog != "" {
		toolSection = "\n\n# 可用工具（只能选择下列清单中的工具引用；清单为空或无合适工具则 tool 必须留空，禁止编造未列出的工具或服务器名）\n" + catalog
	}
	user := fmt.Sprintf("# 任务\n%s\n\n# 详细说明\n%s\n%s\n\n只输出一个 JSON 对象。", req.Task.Title, req.Task.Prompt, toolSection)

	// I1 取消传播：每轮决策前检查（工具步边界=runToolLoop 的每次 decide 入口）
	cancelCheck := req.CancelCheck
	if cancelCheck == nil {
		cancelCheck = func() bool { return false }
	}
	steerCheck := req.SteerDrain // 取走一次性插话（注入后清空）
	if steerCheck == nil {       // 与 CancelCheck 同款 nil 守卫：直调/测试路径无插话源
		steerCheck = func() string { return "" }
	}
	decide := func(system, user string) (decision, error) {
		if cancelCheck() {
			return decision{}, ErrCanceled
		}
		if steer := steerCheck(); steer != "" {
			user += "\n\n" + steer + "\n（请结合以上插话调整后续行动）"
		}
		raw, tokens, err := execLLM.ChatJSONSchema(ctx, system, user, 900, decisionSchema)
		if err != nil {
			return decision{}, err
		}
		totalTokens += tokens
		var d decision
		if err := json.Unmarshal(raw, &d); err != nil || strings.TrimSpace(d.Reply) == "" {
			if d.Tool == "" {
				// 模型给了非预期结构且未请求工具：原文当 reply，不让任务失败
				d = decision{Reply: string(raw), Note: "（模型输出非 JSON，已按原文采用）"}
			}
		}
		return d, nil
	}
	// I11 重放索引：ref+argsJSON → 已记录结果
	replayIdx := map[string]string{}
	for _, rt := range req.ReplayTools {
		replayIdx[rt.Ref+"|"+rt.ArgsJSON] = rt.Result
	}
	callTool := func(ref string, args map[string]any) (string, error) {
		// I11 结果重放：命中已记录观察直接返回（不重复调用 MCP——确定性步重试语义）
		if req.ReplayTools != nil {
			if cached, ok := replayIdx[ref+"|"+mustJSON(args)]; ok {
				return cached, nil
			}
		}
		// I28 第 2 轮：幻觉工具名——不进闸不执行，转引导观察回灌下一轮（模型从清单重选）
		// （toolKnownFn seam：生产=ToolKnown 真判定，测试注入模拟已注册工具）
		if !toolKnownFn(ref) {
			guide := UnifiedTools().Catalog()
			if guide != "" {
				guide = "可用工具：\n" + guide
			} else {
				guide = "当前没有任何可用工具，请直接基于已有信息给出最终回答（tool 留空）。"
			}
			return fmt.Sprintf("错误：工具 %s 不存在。%s\n请重新决策：从清单中选择或给出最终回答。", ref, guide), nil
		}
		// I33 工具级审批：高风险工具在执行前拦截（审批记录提及该工具名才放行）
		if highRiskTool(ref) && !toolApproved(req.Task.Prompt, ref) {
			emit(model.StepGate, "tool-approval-gate", map[string]any{"tool": ref, "args": args},
				map[string]any{"status": "等待人工审批（工具级）", "risk_level": "high"}, 30, 0)
			return "", &ToolGateError{Ref: ref, Args: args}
		}
		// I8：统一工具池——builtin:/mcp:/无前缀引用单一入口分发
		return UnifiedTools().Invoke(ref, args)
	}
	onToolTurn := func(turn int, d decision, observation string) {
		// I17 层级：每轮决策先落 decide-turn-N 步，工具观察步挂其下（parent_seq）
		turnStep := emit(model.StepLLM, "decide-turn-"+itoa(turn),
			map[string]any{"model": req.Agent.Model, "turn": turn},
			map[string]any{"tool_requested": d.Tool}, 0, 0)
		parentSeq := int64(0)
		if turnStep != nil {
			parentSeq = turnStep.Seq
		}
		// I28 第 2 轮：幻觉工具（引导观察）与真实执行的 output 标记区分
		hallucination := strings.HasPrefix(observation, "错误：工具")
		out := map[string]any{"result_head": head(observation, 200),
			"result": head(observation, 2000), // I11：完整观察（重放通道回灌用，head 不够）
			"turn":   turn, "transport": "stdio", "protocol": "mcp"}
		if hallucination {
			out["hallucination"] = true
		}
		// I11：tool 步是确定性步（可重放）；本次是否来自重放
		replayed := false
		if req.ReplayTools != nil {
			if _, ok := replayIdx[d.Tool+"|"+mustJSON(d.ToolArgs)]; ok {
				replayed = true
				out["replay"] = true
			}
		}
		st := &model.RunStep{Kind: model.StepTool, ParentSeq: parentSeq, Name: d.Tool,
			Input: d.ToolArgs, Output: out, LatencyMS: 60, Deterministic: true, Replay: replayed}
		if req.Recorder != nil {
			_ = req.Recorder(ctx, st)
		}
	}

	start := timeNow()
	d, observations, steps, loopErr := runToolLoop(system, user, decide, callTool, onToolTurn)
	latency := timeNow().Sub(start).Milliseconds()
	var gateErr *ToolGateError
	if loopErr != nil && errors.As(loopErr, &gateErr) {
		// I33：工具级审批——不是失败，任务进 review 门；approve 后审批记录入 prompt 重跑放行
		return Result{Reply: "（暂停）工具调用需人工审批：" + gateErr.Ref,
			NeedsApproval: true, RiskLevel: "high",
			Note: fmt.Sprintf("工具级审批: %s 参数 %v（approve 意见需提及工具名以放行）", gateErr.Ref, gateErr.Args)}, nil
	}
	if loopErr != nil {
		emit(model.StepLLM, "llm-failed", map[string]any{"model": req.Agent.Model}, map[string]any{"error": loopErr.Error()}, latency, totalTokens)
		return Result{}, fmt.Errorf("llm: %w", loopErr)
	}

	emit(model.StepLLM, "decide", map[string]any{"model": req.Agent.Model, "steps": steps,
		"observations": len(observations)},
		map[string]any{"handoff_to": d.HandoffTo, "needs_approval": d.NeedsApproval, "reply_head": head(d.Reply, 200)},
		latency, totalTokens)

	// 长回复自动固化为报告产物（不再只是 result 文本）
	artifacts := []ArtifactDraft{}
	if runeLen(d.Reply) > 400 && d.HandoffTo == "" {
		artifacts = append(artifacts, ArtifactDraft{Kind: "report", Name: "reply-report.md", Content: d.Reply})
	}
	return Result{Reply: d.Reply, HandoffTo: d.HandoffTo, NeedsApproval: d.NeedsApproval, Note: d.Note,
		Artifacts: artifacts}, nil
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// mustJSON map→JSON（Go json.Marshal 对 map 键排序，同参数序列化确定）
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func runeLen(s string) int { return len([]rune(s)) }

func buildSystemPrompt(a model.Agent) string {
	base := a.SystemPrompt
	if base == "" {
		base = fmt.Sprintf("你是 %s（%s），一名可靠的执行者。", a.Name, a.Role)
	}
	return base + `

输出要求：只输出一个 JSON 对象，字段如下：
{"reply": "任务的最终产出或处理说明", "handoff_to": "需要交接给其他 agent 时填其名字，否则留空", "needs_approval": true/false, "note": "交接或需要审批的原因"}
规则：涉及对外发布、资金、删除操作时 needs_approval 必须为 true；任务超出你的职责时用 handoff_to 交接，不要硬做。`
}
