package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 小队 leader briefing（I30，协议结构对标 Multica Squad Operating Protocol）。
// Leader 只协调不动手：读 Roster 按技能选人 → 输出转派指令（走 handoff 通道）→ 记录评估。

const squadProtocolHeader = `## 小队运行协议

**你被激活为这个小队的 LEADER。你的职责是协调，不是亲自干活。**
即使任务看起来像「直接让你做 X」，你也必须把 X 转派给最合适的成员——
leader 亲自干活违反协议。

职责顺序：
1. **读任务**，对照下方小队花名册的技能，选出最合适的成员。
2. **转派**：在你的最终输出 JSON 中设 "handoff_to" 为该成员名，note 用
   两三句话说明：为什么选 TA + 额外约束或提示。不要复述任务本身（成员自己会读）。
3. **无动作也记录**：若评估后认为无需任何人行动，reply 写明"no_action"及原因。
4. **派完即停**：转派后结束本轮，不要继续工作。成员完成后你会被再次唤醒评估下一步。
5. **每次重新评估**：再次唤醒时读新进展，决定继续转派、上报人工、还是收尾。`

// buildSquadBriefing 组装 leader 的完整 briefing：协议 + Roster（含成员技能）+ 小队指令。
func buildSquadBriefing(ctx context.Context, st store.Store, squad *model.Squad) string {
	var b strings.Builder
	b.WriteString(squadProtocolHeader)

	// Roster：leader 自身 + 成员（技能来自 agent 的 Tools 字段与 skills 库）
	b.WriteString("\n\n## 小队花名册\n\n")
	leader, err := st.GetAgent(ctx, squad.LeaderID)
	if err == nil {
		b.WriteString(fmt.Sprintf("Leader（你）：%s — 转派用 handoff_to: %s\n", leader.Name, leader.Name))
	}
	members, _ := st.ListSquadMembers(ctx, squad.ID)
	agentRows := 0
	for _, m := range members {
		if m.MemberType != "agent" || m.MemberID == squad.LeaderID {
			continue
		}
		a, err := st.GetAgent(ctx, m.MemberID)
		if err != nil {
			continue
		}
		skills := memberSkills(ctx, st, a)
		role := m.Role
		if role == "" {
			role = a.Role
		}
		b.WriteString(fmt.Sprintf("- %s — %s — 技能：%s\n", a.Name, role, strings.Join(skills, "、")))
		agentRows++
	}
	if agentRows == 0 {
		b.WriteString("\n（无其他成员——你是唯一成员，此时可自行完成任务）\n")
	}

	if strings.TrimSpace(squad.Instructions) != "" {
		b.WriteString(fmt.Sprintf("\n## 小队指令（%s）\n\n%s\n", squad.Name, squad.Instructions))
	}
	return b.String()
}

// memberSkills 成员技能清单：tools + 关联 skills 库条目名。
func memberSkills(ctx context.Context, st store.Store, a *model.Agent) []string {
	out := append([]string{}, a.Tools...)
	if skills, err := st.ListSkills(ctx, ""); err == nil {
		for _, sk := range skills {
			// agent tools 引用过的 skill 视为其技能（简化匹配：名字出现在 tools 里）
			for _, t := range a.Tools {
				if t == sk.Name {
					out = append(out, sk.Name)
				}
			}
		}
	}
	return out
}

// ---- I30 辅助：原始 agent 跟踪（handoff 前的初始执行者）----

var originalAgents = struct {
	sync.Mutex
	m map[int64]int64
}{m: map[int64]int64{}}

func setOriginalAgent(taskID, agentID int64) {
	originalAgents.Lock()
	defer originalAgents.Unlock()
	if _, ok := originalAgents.m[taskID]; !ok {
		originalAgents.m[taskID] = agentID
	}
}

func originalAgentID(task model.Task) int64 {
	originalAgents.Lock()
	defer originalAgents.Unlock()
	if id, ok := originalAgents.m[task.ID]; ok {
		return id
	}
	return task.AgentID
}

func headStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
