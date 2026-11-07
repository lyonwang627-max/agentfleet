package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// run 报告 Markdown 导出（I23）：trace → 可读报告（归档/分享/贴汇报）。
// 纯函数渲染（renderRunMarkdown），端点只做取数与 content-type。

func renderRunMarkdown(run *model.Run, task *model.Task, agentName string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Run #%d 执行报告\n\n", run.ID))
	b.WriteString(fmt.Sprintf("- **任务**: %s（task#%d）\n", task.Title, task.ID))
	b.WriteString(fmt.Sprintf("- **Agent**: %s\n", agentName))
	b.WriteString(fmt.Sprintf("- **状态**: %s\n", run.Status))
	b.WriteString(fmt.Sprintf("- **Token**: %d\n", run.TotalTokens))
	if run.Error != "" {
		b.WriteString(fmt.Sprintf("- **错误**: %s\n", run.Error))
	}
	started := run.StartedAt.Format("2006-01-02 15:04:05")
	b.WriteString(fmt.Sprintf("- **开始**: %s\n", started))
	if run.FinishedAt != nil {
		dur := run.FinishedAt.Sub(run.StartedAt)
		b.WriteString(fmt.Sprintf("- **耗时**: %s\n", dur.Round(time.Millisecond)))
	}

	b.WriteString("\n## 执行步骤\n\n")
	b.WriteString("| # | 类型 | 名称 | 耗时 | Tokens | 说明 |\n|---|---|---|---|---|---|\n")
	for _, s := range run.Steps {
		note := s.Note
		if note == "" && s.Output != nil {
			if r, ok := s.Output["result"].(string); ok {
				note = r
			}
		}
		if len(note) > 80 {
			note = note[:80] + "…"
		}
		b.WriteString(fmt.Sprintf("| %d | %s | %s | %dms | %d | %s |\n",
			s.Seq, s.Kind, s.Name, s.LatencyMS, s.Tokens, note))
	}
	if len(run.Steps) == 0 {
		b.WriteString("| - | - | （无步骤记录） | - | - | - |\n")
	}

	if task.Result != "" {
		b.WriteString("\n## 最终产出\n\n")
		b.WriteString(truncateMd(task.Result, 2000))
		b.WriteString("\n")
	}
	return b.String()
}

func truncateMd(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *Server) exportRunMarkdown(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	run, err := s.Store.GetRun(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	task, err := s.Store.GetTask(r.Context(), run.TaskID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	agentName := fmt.Sprintf("agent#%d", run.AgentID)
	if a, err := s.Store.GetAgent(r.Context(), run.AgentID); err == nil {
		agentName = a.Name
	}

	md := renderRunMarkdown(run, task, agentName)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q",
		fmt.Sprintf("run-%d-report.md", run.ID)))
	_, _ = w.Write([]byte(md))
}
