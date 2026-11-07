package scheduler

import (
	"fmt"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
)

// Prompt 模板变量（I21，技术深度）：system_prompt/prompt 支持 {{...}} 变量，
// 执行时渲染。设计原则：
//   - 未知变量原样保留（不静默清空——写错变量名应该被看见而不是悄悄消失）
//   - 渲染只发生在执行副本（与 skill/workspace 注入同通道，不污染任务本体）
//   - 渲染事实落 trace（route 步），可审计

// renderTemplate 纯函数：变量替换。task/ws 允许 nil（对应变量替换为空串）。
func renderTemplate(text string, task *model.Task, ws *model.Workspace, agentName string) string {
	if !strings.Contains(text, "{{") {
		return text // 快路径：无模板标记零开销
	}
	taskTitle, taskID := "", int64(0)
	if task != nil {
		taskTitle, taskID = task.Title, task.ID
	}
	wsPath, wsBranch, wsName := "", "", ""
	if ws != nil {
		wsPath, wsBranch, wsName = ws.RootPath, ws.Branch, ws.Name
	}
	replace := map[string]string{
		"task.title":       taskTitle,
		"task.id":          fmt.Sprint(taskID),
		"workspace.path":   wsPath,
		"workspace.branch": wsBranch,
		"workspace.name":   wsName,
		"agent.name":       agentName,
	}
	for k, v := range replace {
		text = strings.ReplaceAll(text, "{{"+k+"}}", v)
	}
	return text
}
