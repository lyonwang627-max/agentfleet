package api

import (
	"fmt"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 会话历史上下文（I6，复用 F11 Spill 思想）：prompt 是工作记忆不是归档。
// - 1:1 chat（sync/stream）此前每轮只发当前消息，多轮上下文缺失——本次补上；
// - 群聊 buildRoomPrompt 此前全量时间线无界增长——本次加上限。
// 压缩是确定性文本折叠（非模型摘要）：超过阈值时较早消息折叠为单行摘要，
// 仅最近 keepRecent 条保留全文——prompt 长度有界，完整历史在会话回放（F6）可查。
const (
	chatCompressThreshold = 20 // 超过此条数触发压缩（I6 验收：>20 轮仍可续聊且 prompt 有界）
	chatKeepRecent        = 12 // 压缩时保留全文的最近条数
	roomKeepRecent        = 16 // 群聊时间线保留全文的最近条数（room 单条本就有 300 rune 头）
)

// chatHistoryTranscript 把会话消息格式化为 LLM 可用的时间线文本。
// 返回文本与折叠条数（0=未触发压缩）。
func chatHistoryTranscript(msgs []model.Message, keepRecent int) (string, int) {
	if len(msgs) == 0 {
		return "", 0
	}
	var b strings.Builder
	folded := 0
	start := 0
	if len(msgs) > chatCompressThreshold {
		folded = len(msgs) - keepRecent
		start = folded
		fmt.Fprintf(&b, "[较早 %d 条消息已压缩为单行摘要；完整历史见会话回放]\n", folded)
		for _, m := range msgs[:folded] {
			b.WriteString(historyLine(m, 40))
			b.WriteString("\n")
		}
		b.WriteString("\n[最近对话]\n")
	}
	for _, m := range msgs[start:] {
		b.WriteString(historyLine(m, 300))
		b.WriteString("\n")
	}
	return b.String(), folded
}

// historyLine 单条消息一行：发言人标签 + 内容头部截断。
func historyLine(m model.Message, head int) string {
	who := "助手"
	if m.Role == "user" {
		who = "用户"
	} else if m.AgentName != "" {
		who = m.AgentName
	}
	c := m.Content
	if r := []rune(c); len(r) > head {
		c = string(r[:head]) + "…"
	}
	return "[" + who + "] " + c
}
