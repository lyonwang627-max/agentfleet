package seed

import (
	"context"
	"testing"

	"github.com/lyonwang/agentfleet/internal/store"
)

// 种子完整性：一次 seed 产出完整舰队/任务链/评测集；二次 seed 幂等跳过。
func TestSeedFreshMemStore(t *testing.T) {
	st := store.NewMemStore()
	if err := Run(context.Background(), st, "mock", "mock"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	agents, _ := st.ListAgents(context.Background(), store.AgentFilter{})
	if len(agents) != 5 {
		t.Fatalf("期望 5 agent 舰队，得到 %d", len(agents))
	}
	names := map[string]bool{}
	for _, a := range agents {
		names[a.Name] = true
	}
	for _, want := range []string{"intake", "fixer", "reviewer", "analyst", "summarizer"} {
		if !names[want] {
			t.Fatalf("缺角色 %q", want)
		}
	}

	tasks, total, _ := st.ListTasks(context.Background(), nil, 0, 1, 50)
	if total != 4 {
		t.Fatalf("期望 4 条种子任务，得到 %d", total)
	}
	_ = tasks

	datasets, _ := st.ListDatasets(context.Background())
	if len(datasets) != 3 {
		t.Fatalf("期望 3 个评测数据集，得到 %d", len(datasets))
	}
	totalCases := 0
	for _, d := range datasets {
		totalCases += d.CaseCount
	}
	if totalCases != 24 {
		t.Fatalf("评测 case 总数应为 24，得到 %d", totalCases)
	}

	workspaces, _ := st.ListWorkspaces(context.Background(), true)
	if len(workspaces) != 1 || workspaces[0].Name != "release-hotfix" {
		t.Fatalf("执行环境种子缺失: %+v", workspaces)
	}
	skills, _ := st.ListSkills(context.Background(), "")
	if len(skills) != 1 || skills[0].Name != "晨会摘要" && skills[0].Name != "线上告警处置 SOP" {
		t.Fatalf("经验种子异常: %+v", skills)
	}
	automations, _ := st.ListAutomations(context.Background(), false)
	if len(automations) != 1 || automations[0].CronExpr != "30 9 * * 1-5" {
		t.Fatalf("自动化规则种子缺失: %+v", automations)
	}

	// 幂等：二次 seed 不重复创建
	if err := Run(context.Background(), st, "mock", "mock"); err != nil {
		t.Fatal(err)
	}
	agents2, _ := st.ListAgents(context.Background(), store.AgentFilter{})
	if len(agents2) != 5 {
		t.Fatalf("幂等性破坏：agent 变 %d", len(agents2))
	}
	automations2, _ := st.ListAutomations(context.Background(), false)
	if len(automations2) != 1 {
		t.Fatalf("幂等性破坏：automation 变 %d", len(automations2))
	}
}
