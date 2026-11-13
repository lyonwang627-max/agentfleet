package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/runner"
	"github.com/lyonwang/agentfleet/internal/store"
)

func TestScoreTier(t *testing.T) {
	// 高优+审批 → L
	if got := ScoreTier("紧急发布", "涉及审批的发布任务", 9, 1.0, 0); got != TierL {
		t.Fatalf("高优审批任务应 L: %v", got)
	}
	// 低优短任务 → S
	if got := ScoreTier("小任务", "直接完成", 2, 1.0, 0); got != TierS {
		t.Fatalf("低优短任务应 S: %v", got)
	}
	// 标准任务 → M
	if got := ScoreTier("常规处理", strings.Repeat("说明。", 100), 5, 1.0, 0); got != TierM {
		t.Fatalf("常规任务应 M: %v", got)
	}
	// 近期表现差 → 直接 L（质量问题先升级）
	if got := ScoreTier("普通", "普通任务", 5, 0.3, 5); got != TierL {
		t.Fatalf("近期表现差应 L: %v", got)
	}
}

func TestTierToModel(t *testing.T) {
	// llm 未注入时回退 fallback（MemStore 测试场景）
	s := New(store.NewMemStore(), nil)
	if got := s.TierToModel(TierL, "agent-model"); got != "agent-model" {
		t.Fatalf("无网关应回退 agent 模型: %v", got)
	}
}

// 灰度分桶（I38）：确定性 / 边界 / ramping 桶快照覆盖。
func TestRouteVersionBucketing(t *testing.T) {
	// ramp=0 或无 ramping → 恒 current
	if got := routeVersion(42, 3, 0, 0); got != 3 {
		t.Fatalf("无灰度应 current: %d", got)
	}
	if got := routeVersion(42, 3, 4, 0); got != 3 {
		t.Fatalf("ramp=0 应 current: %d", got)
	}
	// 确定性：同 taskID+版本 恒同结果
	a := routeVersion(42, 3, 4, 50)
	for i := 0; i < 10; i++ {
		if got := routeVersion(42, 3, 4, 50); got != a {
			t.Fatalf("分桶不确定: %d vs %d", got, a)
		}
	}
	// ramp=100 → 全部 ramping
	if got := routeVersion(42, 3, 4, 100); got != 4 {
		t.Fatalf("ramp=100 应全 ramping: %d", got)
	}
	// 掺 rampingVersion：不同灰度轮分桶可变（避免同任务永远锁定同桶）
	_ = routeVersion(42, 3, 5, 50)
}

// applyVersionSnapshot：快照覆盖 agent 副本（system_prompt/model）。
func TestApplyVersionSnapshot(t *testing.T) {
	a := &model.Agent{SystemPrompt: "旧", Model: "qwen3.5"}
	applyVersionSnapshot(a, map[string]any{"system_prompt": "新提示词", "model": "lfm25:latest"})
	if a.SystemPrompt != "新提示词" || a.Model != "lfm25:latest" {
		t.Fatalf("快照覆盖不符: %+v", a)
	}
	// model 空不覆盖
	a2 := &model.Agent{SystemPrompt: "旧", Model: "qwen3.5"}
	applyVersionSnapshot(a2, map[string]any{"system_prompt": "新"})
	if a2.Model != "qwen3.5" {
		t.Fatalf("空 model 不应覆盖: %q", a2.Model)
	}
}

// slog 结构化日志（I41 第 1 轮）：字段化断言（task_id/run_id 为独立字段而非消息文本）。
func TestSlogFieldsInDispatch(t *testing.T) {
	buf := &lockedBuffer{}
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })

	st := store.NewMemStore()
	s := New(st, NewHub(st))
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "sl", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "日志任务", Prompt: "p", AgentID: aid})
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})
	task, _ := st.GetTask(ctx, tid)

	capR := &captureRunner{}
	runner.Register("mock", capR)
	t.Cleanup(func() { runner.Register("mock", runner.NewMock()) })
	s.execute(ctx, *task)

	out := buf.String()
	// JSON 行解析：task_id/run_id 为独立字段
	var rec map[string]any
	found := false
	for _, line := range strings.Split(out, "\n") {
		if json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == "task dispatched" {
			found = true
			if rec["task_id"].(float64) != float64(task.ID) {
				t.Fatalf("task_id 字段不符: %v", rec["task_id"])
			}
			if _, ok := rec["run_id"]; !ok {
				t.Fatal("缺 run_id 字段")
			}
			break
		}
	}
	if !found {
		t.Fatalf("缺 task dispatched 记录: %s", out[:min(len(out), 200)])
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
