package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

// I30 小队模式端到端（mock 确定性链路）：
// 建 squad（leader=intake, 成员=fixer）→ 分任务给 squad（leader 被唤醒 + briefing 注入）
// → leader 按 mock 剧本 handoff:fixer → 成员完成 → 自动生成 leader 跟进评估任务。
func TestSquadModeEndToEnd(t *testing.T) {
	st := store.NewMemStore()
	hub := NewHub(st)
	runner.Register("mock", runner.NewMock())
	s := New(st, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop()

	leaderID := mustAgent(t, st, "intake", "mock")
	_ = leaderID
	// 建 fixer 成员（mustAgent 建的是普通 agent）
	memberID, err := st.CreateAgent(ctx, &model.Agent{Name: "fixer", Role: "修复", Model: "mock",
		Runner: "mock", MaxConcurrency: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	// 建 squad
	squadID, err := st.CreateSquad(ctx, &model.Squad{
		Name: "响应小队", Description: "告警处置", LeaderID: leaderID,
		Instructions: "优先让修复角色动手"})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.AddSquadMember(ctx, &model.SquadMember{SquadID: squadID, MemberType: "agent", MemberID: memberID})

	// briefing 含协议 + Roster（成员名）
	briefing := buildSquadBriefing(ctx, st, &model.Squad{ID: squadID, Name: "响应小队",
		LeaderID: leaderID, Instructions: "优先让修复角色动手"})
	for _, want := range []string{"LEADER", "小队花名册", "fixer", "优先让修复角色动手"} {
		if !strings.Contains(briefing, want) {
			t.Fatalf("briefing 缺 %q:\n%s", want, briefing)
		}
	}

	// 分任务给 squad（leader 执行）——mock 剧本：handoff:fixer
	taskID, err := st.CreateTask(ctx, &model.Task{
		Title: "P2 告警处置", Prompt: "定位原因后 handoff:fixer 执行修复。", AgentID: leaderID,
		Priority: 8, SquadID: squadID})
	if err != nil || taskID <= 0 {
		t.Fatal(err)
	}

	// 等 leader run done（handoff 后 fixer 接手 → fixer done → 跟进任务生成）
	deadline := time.Now().Add(15 * time.Second)
	var followUpID int64
	for time.Now().Before(deadline) {
		tasks, _, _ := st.ListTasks(ctx, nil, 0, 1, 50)
		for _, tk := range tasks {
			if strings.Contains(tk.Title, "小队跟进") && tk.AgentID == leaderID {
				followUpID = tk.ID
			}
		}
		if followUpID > 0 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if followUpID == 0 {
		t.Fatal("成员完成后未生成 leader 跟进评估任务")
	}
	// 跟进任务也属于该小队（leader 再次拿 briefing）
	fu, _ := st.GetTask(ctx, followUpID)
	if fu.SquadID != squadID || fu.AgentID != leaderID {
		t.Fatalf("跟进任务应归 leader 且属同小队: %+v", fu)
	}
}
