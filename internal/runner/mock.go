package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// MockRunner 确定性执行器：按任务文本中的场景关键词走不同剧本。
// 用途：无模型环境的产品演示（全流程可重放）、集成测试、trace 瀑布图填充。
// 剧本关键词：审批→人工审批门；handoff:名字→交接；失败→报错（测重试）；搜索→工具步。
type MockRunner struct{}

func NewMock() *MockRunner { return &MockRunner{} }

func (m *MockRunner) Execute(ctx context.Context, req ExecRequest) (Result, error) {
	prompt := req.Task.Prompt
	emit := func(kind model.StepKind, name string, in, out map[string]any, latencyMS int64, tokens int) {
		if req.Recorder == nil {
			return
		}
		_ = req.Recorder(ctx, &model.RunStep{Kind: kind, Name: name, Input: in, Output: out,
			LatencyMS: latencyMS, Tokens: tokens})
	}

	emit(model.StepLLM, "plan", map[string]any{"prompt_head": head(prompt, 120)},
		map[string]any{"decision": "已解析任务，规划执行路径", "mock": true}, 180, 120)

	// 工具步与决策不互斥：需要检索的剧本先搜索，再走决策分支
	if strings.Contains(prompt, "搜索") {
		emit(model.StepTool, "text_search", map[string]any{"query": head(prompt, 60)},
			map[string]any{"hits": 2, "source": "seed-knowledge", "mock": true}, 95, 0)
	}

	// MCP 指令分支：任务文本含「MCP:server/tool 参数」→ 真实调用 MCP server（trace 落实际输出）
	if _, rest, ok := strings.Cut(prompt, "MCP:"); ok {
		server, remainder, _ := strings.Cut(strings.TrimSpace(rest), "/")
		tool := remainder
		arg := ""
		if i := strings.IndexAny(remainder, " \n"); i >= 0 { // 工具名不含空格；空格后是参数
			tool, arg = remainder[:i], strings.TrimSpace(remainder[i:])
		}
		server, tool = strings.TrimSpace(server), strings.TrimSpace(tool)
		args := map[string]any{"msg": arg}
		out, err := CallMCPTool("mcp:"+server+"/"+tool, args)
		if err != nil {
			emit(model.StepTool, "mcp:"+server+"/"+tool, args, map[string]any{"error": err.Error()}, 50, 0)
			return Result{}, fmt.Errorf("mcp: %w", err)
		}
		emit(model.StepTool, "mcp:"+server+"/"+tool, args,
			map[string]any{"result": out, "transport": "stdio", "protocol": "mcp"}, 60, 0)
		return Result{Reply: fmt.Sprintf("MCP 工具调用完成：%s", out)}, nil
	}

	switch {
	case strings.Contains(prompt, "[审批记录]"):
		// 审批放行后的重放：确定性走完执行（这是审批闭环的另一半）
		emit(model.StepGate, "approval-passed", nil, map[string]any{"status": "已获人工放行，执行发布/变更"}, 40, 0)
		return Result{Reply: fmt.Sprintf("已获人工审批，完成执行。产出：%s", head(prompt, 200))}, nil

	case strings.Contains(prompt, "失败"):
		emit(model.StepLLM, "explode", nil, map[string]any{"error": "模拟执行失败（用于测试重试）"}, 60, 40)
		return Result{}, fmt.Errorf("mock: 任务要求模拟失败")

	case strings.Contains(prompt, "handoff:") || strings.Contains(prompt, "交接给"):
		// handoff 判定先于审批：带交接指令的任务先移交，由接收方决定是否进审批门
		target := extractAfter(prompt, "handoff:")
		if target == "" {
			target = extractAfter(prompt, "交接给")
		}
		if target == "" {
			target = "reviewer"
		}
		emit(model.StepHandoff, "->"+target, map[string]any{"from": req.Agent.Name},
			map[string]any{"reason": "超出当前 agent 职责范围", "target": target}, 30, 0)
		return Result{HandoffTo: target,
			Reply:    fmt.Sprintf("[%s 阶段结论] %s（已移交 %s 继续）", req.Agent.Name, head(prompt, 100), target),
			Note:     fmt.Sprintf("%s 完成初步处理，移交 %s 继续执行其职责", req.Agent.Name, target)}, nil

	case strings.Contains(prompt, "审批"):
		emit(model.StepGate, "approval-gate", map[string]any{"reason": "产出涉及对外发布"},
			map[string]any{"status": "等待人工审批"}, 40, 0)
		return Result{Reply: "（草稿）" + head(prompt, 200), NeedsApproval: true,
			Note: "涉及对外发布，需要人工确认后执行"}, nil

	default:
		time.Sleep(80 * time.Millisecond) // 让瀑布图有真实可感的耗时分布
		emit(model.StepLLM, "execute", nil, map[string]any{"steps": 1, "mock": true}, 240, 180)
		artifacts := []ArtifactDraft{}
		// 周报/报告类任务产出结构化报告产物
		if strings.Contains(req.Task.Title, "周报") || strings.Contains(prompt, "报告") {
			artifacts = append(artifacts, ArtifactDraft{Kind: "report", Name: "报告-" + req.Task.Title + ".md",
				Content: "# " + req.Task.Title + "\n\n## 结论\n" + head(prompt, 200) + "\n\n（mock 产出的结构化报告）\n"})
		}
		// 绑定执行环境的任务产出环境内执行记录（对标 worktree 内产物）
		if req.Workspace != nil {
			artifacts = append(artifacts, ArtifactDraft{Kind: "log", Name: "workspace-exec.log",
				Content: "workspace=" + req.Workspace.Name + " path=" + req.Workspace.RootPath + "\n任务在绑定环境中执行完成\n"})
		}
		return Result{Reply: fmt.Sprintf("任务「%s」已完成（mock runner）：%s", req.Task.Title, head(prompt, 150)),
			Artifacts: artifacts}, nil
	}
}

func head(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func extractAfter(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := strings.TrimLeft(s[i+len(marker):], "：: \t\n")
	if j := strings.IndexAny(rest, "，。\n "); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(strings.Trim(rest, "：: "))
}
