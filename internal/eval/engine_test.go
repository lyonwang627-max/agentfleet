package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// scoreCase 错误路径（作答失败——网关不可用）与 judge 越界分拒绝。
func TestScoreCaseAnswerFailure(t *testing.T) {
	// LLM 指向不可达端点 → Chat 报错 → 0 分不通过，comment 带错误
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"content":"ok"}}`))
	}))
	l := gateway.NewWithBaseURL("m", srv.URL)
	srv.Close()
	eng := &Engine{Store: nil, LLM: l}
	score, pass, comment := eng.scoreCase(context.Background(),
		&model.Agent{Name: "x"}, model.EvalCase{Input: "题"})
	if score != 0 || pass {
		t.Fatalf("作答失败应 0 分不通过: %d %v", score, pass)
	}
	if !strings.Contains(comment, "作答失败") {
		t.Fatalf("comment 应带失败原因: %q", comment)
	}
}

func TestJudgeScoreRejected(t *testing.T) {
	// judge 输出越界分（15）→ 拒绝采用，回退启发式分数
	st := store.NewMemStore()
	eng := &Engine{Store: st, LLM: gateway.New("mock")}
	eng.LLM.Fallback = func(system, user string) string {
		if strings.Contains(system, "judge") {
			return `{"score": 15, "comment": "越界分"}`
		}
		return "(mock) 回答"
	}
	agent := &model.Agent{Name: "tester", SystemPrompt: "p", Model: "mock"}
	score, _, _ := eng.scoreCase(context.Background(), agent, model.EvalCase{
		Input:    "题干包含 要点甲",
		Expected: map[string]any{"points": []string{"要点甲"}},
	})
	// 越界分（15）被拒 → 启发式分数（Fallback 固定回答不含要点 → hit=0 → 3）
	// 若 judge 越界分被误采用则 score=15——边界防护锁
	if score != 3 {
		t.Fatalf("越界 judge 分应被拒并回退启发式(3): %d", score)
	}
}
