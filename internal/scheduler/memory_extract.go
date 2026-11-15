package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 记忆抽取（ADR-0003 M2）：run done 后 judge 旁路抽取「值得记住的事实」→ draft 池。
// 铁律：抽取失败不阻塞 run 收尾（fail-open）；草稿不生效（人审 approve 后 active——M1 API）。

const extractMaxMemories = 3 // 单次 run 最多抽取 3 条（防噪音）

// ExtractMemories 从 run 产出中抽取长期记忆草稿（LLM 一次调用；mock/不可用时跳过）。
// 抽取失败仅 log——绝不阻塞 run 收尾。
func (s *Scheduler) ExtractMemories(ctx context.Context, agentID, runID int64, taskTitle, reply string) {
	if s.judge == nil || !s.judge.Healthy() {
		return // fail-open：与看门狗同款放行语义
	}
	prompt := "# 任务\n" + taskTitle + "\n\n# 产出\n" + clipStr(reply, 1500) +
		"\n\n从以上产出中提取值得长期记住的事实（关于环境/偏好/决策——非一次性操作细节）。" +
		"只输出 JSON：{\"memories\": [{\"kind\": \"fact|preference|decision\", \"content\": \"一句话陈述\"}]}，" +
		"最多 3 条；没有值得记住的则返回 {\"memories\": []}。"
	raw, _, err := s.judge.ChatJSON(ctx,
		"你是记忆提取器。只输出 JSON：{\"memories\": [{\"kind\": \"fact|preference|decision\", \"content\": \"一句话陈述\"}]}。"+
			"kind 取值：fact=环境事实，preference=偏好，decision=决策。最多 3 条，没有则空数组。",
		prompt, 400)
	if err != nil {
		slog.Debug("记忆抽取调用失败", "task_id", runID, "err", err)
		return
	}
	var out struct {
		Memories []struct {
			Kind    string `json:"kind"`
			Content string `json:"content"`
		} `json:"memories"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		slog.Debug("记忆抽取解析失败", "err", err)
		return
	}
	n := 0
	for _, m := range out.Memories {
		if n >= extractMaxMemories {
			break
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		kind := m.Kind
		if kind != "fact" && kind != "preference" && kind != "decision" {
			kind = "fact"
		}
		if _, err := s.store.CreateMemory(ctx, &model.Memory{
			AgentID: agentID, Scope: "agent", Kind: kind,
			Content: content, SourceRunID: runID, Status: "draft",
		}); err != nil {
			slog.Warn("记忆草稿落库失败", "err", err)
			continue
		}
		n++
	}
	if n > 0 {
		s.emit(ctx, "memory.extracted", map[string]any{"agent_id": agentID, "run_id": runID, "count": n})
		slog.Info("记忆草稿已抽取", "agent_id", agentID, "run_id", runID, "count", n)
	}
}

// clipStr rune 安全截断。
func clipStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

var _ = time.Now
