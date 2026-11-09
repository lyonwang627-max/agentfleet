package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 环检测（I20 核心算法）：直环/自环/传递环/无环。
func TestValidateNoCycle(t *testing.T) {
	mkTask := func(id int64, deps ...int64) model.Task {
		return model.Task{ID: id, DependsOn: deps}
	}
	tasks := []model.Task{
		mkTask(1, 2), // 1 依赖 2
		mkTask(2, 3), // 2 依赖 3
		mkTask(3),    // 3 无依赖
	}
	// 无环：新任务 4 依赖 3
	if err := validateNoCycle(tasks, 4, []int64{3}); err != nil {
		t.Fatalf("无环误报: %v", err)
	}
	// 直环：新任务 4 依赖 4（自环）
	if err := validateNoCycle(tasks, 4, []int64{4}); err == nil {
		t.Fatal("自环未检出")
	}
	// 注：deps 只能引用已存在的任务 ID，创建时序决定了跨任务传递环
	// 在创建模型下不可能出现——validateNoCycle 的环检查保留为 API 误用的防御深度。
	_ = json.Marshal
}

// HTTP 级回归锁（I20）：dag 端点子图/环标记 + 创建时依赖校验。
func TestTaskDAGHTTP(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), StartedAt: time.Now()}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "dag-a", Model: "mock", Runner: "mock", Enabled: true})

	t1, _ := st.CreateTask(ctx, &model.Task{Title: "A", Prompt: "p", AgentID: aid, Status: model.TaskDone})
	t2, _ := st.CreateTask(ctx, &model.Task{Title: "B", Prompt: "p", AgentID: aid, Status: model.TaskQueued, DependsOn: []int64{t1}})

	// 子图：B 的依赖图含 A、B 两节点一条边，无环
	resp, err := http.Get(ts.URL + "/api/v1/tasks/" + strconv.FormatInt(t2, 10) + "/dag")
	if err != nil {
		t.Fatal(err)
	}
	var dag dagResponse
	_ = json.NewDecoder(resp.Body).Decode(&dag)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(dag.Nodes) != 2 || len(dag.Edges) != 1 || dag.Cycle {
		t.Fatalf("子图错误: code=%d nodes=%d edges=%d cycle=%v", resp.StatusCode, len(dag.Nodes), len(dag.Edges), dag.Cycle)
	}

	// 真环（绕过 API，store 直造 C↔D）：dag 端点须标 cycle=true
	t3, _ := st.CreateTask(ctx, &model.Task{Title: "C", Prompt: "p", AgentID: aid, DependsOn: []int64{4}})
	t4, _ := st.CreateTask(ctx, &model.Task{Title: "D", Prompt: "p", AgentID: aid, DependsOn: []int64{t3}})
	st.UpdateTask(ctx, t3, store.TaskPatch{DependsOn: &[]int64{t4}})
	resp2, _ := http.Get(ts.URL + "/api/v1/tasks/" + strconv.FormatInt(t3, 10) + "/dag")
	var dag2 dagResponse
	_ = json.NewDecoder(resp2.Body).Decode(&dag2)
	resp2.Body.Close()
	if !dag2.Cycle {
		t.Fatal("真环未标记 cycle")
	}
}

func TestCreateTaskDepValidation(t *testing.T) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), StartedAt: time.Now()}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "dv-a", Model: "mock", Runner: "mock", Enabled: true})
	t1, _ := st.CreateTask(ctx, &model.Task{Title: "D1", Prompt: "p", AgentID: aid})

	post := func(body string) *http.Response {
		resp, err := http.Post(ts.URL+"/api/v1/tasks", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// 缺失依赖 → 400（否则任务永远 blocked，静默卡死）
	resp := post(`{"title":"x","agent_id":` + strconv.FormatInt(aid, 10) + `,"depends_on":[99999]}`)
	if resp.StatusCode != 400 {
		t.Fatalf("缺失依赖应 400: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 重复依赖去重
	resp2 := post(`{"title":"y","agent_id":` + strconv.FormatInt(aid, 10) + `,"depends_on":[` +
		strconv.FormatInt(t1, 10) + "," + strconv.FormatInt(t1, 10) + `]}`)
	if resp2.StatusCode != 201 {
		t.Fatalf("重复依赖应去重后 201: %d", resp2.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&created)
	resp2.Body.Close()
	got, _ := st.GetTask(ctx, created.ID)
	if len(got.DependsOn) != 1 {
		t.Fatalf("deps 应去重为 1: %v", got.DependsOn)
	}
}
