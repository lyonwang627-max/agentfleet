package eval

import (
	"context"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Start 编排层直测（此前 0%）：异步跑完评测集 → 终态落库（done/failed + 报告）。
// mock 网关确定性：Healthy()=false 跳过 judge，回答= "(mock) "+题目 —— 要点取题目子串即全命中。

// waitForStatus 轮询 eval run 终态（Start 是异步 goroutine）。
func waitForStatus(t *testing.T, st *store.MemStore, id int64, want string) *model.EvalRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := st.GetEvalRun(context.Background(), id)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("3s 内未到达状态 %s", want)
	return nil
}

func TestStartHappyPath(t *testing.T) {
	st := store.NewMemStore()
	eng := &Engine{Store: st, LLM: gateway.New("mock")}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "tester", Model: "mock", Enabled: true})
	did, _ := st.CreateDataset(ctx, &model.EvalDataset{Name: "冒烟集"})
	// 要点取题目子串 → mock 回答（题目回显）全命中 → 满分路径
	_ = st.SaveCases(ctx, did, []model.EvalCase{
		{Input: "请解释 延迟双删 的作用", Expected: map[string]any{"points": []string{"延迟双删"}}},
		{Input: "请解释 先更新库再删缓存 的作用", Expected: map[string]any{"points": []string{"先更新库再删缓存"}}},
	})
	id, _ := st.CreateEvalRun(ctx, &model.EvalRun{DatasetID: did, AgentID: aid})

	eng.Start(ctx, id, did, aid)
	run := waitForStatus(t, st, id, "done")
	if run.Report == nil || len(run.Report.Cases) != 2 {
		t.Fatalf("报告缺失: %+v", run.Report)
	}
	if run.ScoreAvg != 10 || run.PassRate != 100 {
		t.Fatalf("全命中应满分: avg=%v pass=%v", run.ScoreAvg, run.PassRate)
	}
	for _, c := range run.Report.Cases {
		if c.Score != 10 || !c.Pass {
			t.Fatalf("case 结果不符: %+v", c)
		}
	}
}

func TestStartAgentMissing(t *testing.T) {
	st := store.NewMemStore()
	eng := &Engine{Store: st, LLM: gateway.New("mock")}
	ctx := context.Background()
	did, _ := st.CreateDataset(ctx, &model.EvalDataset{Name: "集"})
	id, _ := st.CreateEvalRun(ctx, &model.EvalRun{DatasetID: did, AgentID: 999})

	eng.Start(ctx, id, did, 999)
	run := waitForStatus(t, st, id, "failed")
	if run.Report != nil {
		t.Fatalf("失败路径不应有报告: %+v", run.Report)
	}
}

func TestStartEmptyDataset(t *testing.T) {
	st := store.NewMemStore()
	eng := &Engine{Store: st, LLM: gateway.New("mock")}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "tester", Model: "mock", Enabled: true})
	did, _ := st.CreateDataset(ctx, &model.EvalDataset{Name: "空集"}) // 无 cases
	id, _ := st.CreateEvalRun(ctx, &model.EvalRun{DatasetID: did, AgentID: aid})

	eng.Start(ctx, id, did, aid)
	waitForStatus(t, st, id, "failed")
}

func TestRound1(t *testing.T) {
	cases := []struct {
		in, want float64
	}{{0.25, 0.3}, {10.0, 10}, {7.54, 7.5}, {0.0, 0}}
	for _, c := range cases {
		if got := round1(c.in); got != c.want {
			t.Fatalf("round1(%v)=%v want %v", c.in, got, c.want)
		}
	}
}
