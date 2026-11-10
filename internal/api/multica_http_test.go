package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Multica 系列 + 会话回放 HTTP 回归锁（此前仅手工实弹，handler 0%）。

func multicaServer(t *testing.T) (*Server, *store.MemStore, context.Context, int64, int64) {
	st := store.NewMemStore()
	s := &Server{Store: st, Hub: scheduler.NewHub(st), LLM: gateway.New("mock"), StartedAt: time.Now()}
	ctx := context.Background()
	aid, _ := st.CreateAgent(ctx, &model.Agent{Name: "reviewer", Model: "mock", Runner: "mock", Enabled: true})
	tid, _ := st.CreateTask(ctx, &model.Task{Title: "评审任务", Prompt: "普通任务，直接完成。", AgentID: aid})
	return s, st, ctx, aid, tid
}

func TestProjectsLifecycleHTTP(t *testing.T) {
	s, _, _, _, _ := multicaServer(t)

	// 创建（缺 name 400 / 正常 201）
	r0 := httptest.NewRequest("POST", "/api/v1/projects", strings.NewReader(`{"description":"无名字"}`))
	w0 := httptest.NewRecorder()
	s.createProject(w0, r0)
	if w0.Code != 400 {
		t.Fatalf("缺 name 应 400: %d", w0.Code)
	}
	r := httptest.NewRequest("POST", "/api/v1/projects",
		strings.NewReader(`{"name":"订单重构","description":"订单服务改造","repos":["git@repo"]}`))
	w := httptest.NewRecorder()
	s.createProject(w, r)
	if w.Code != 201 {
		t.Fatalf("创建应 201: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 列表带任务计数
	wl := httptest.NewRecorder()
	s.listProjects(wl, httptest.NewRequest("GET", "/api/v1/projects", nil))
	if wl.Code != 200 || !strings.Contains(wl.Body.String(), "订单重构") {
		t.Fatalf("列表缺项目: %s", wl.Body.String())
	}
	// 删除 → 列表空
	rd := httptest.NewRequest("DELETE", "/api/v1/projects/"+itoaInt64(created.ID), nil)
	rd.SetPathValue("id", itoaInt64(created.ID))
	wd := httptest.NewRecorder()
	s.deleteProject(wd, rd)
	if wd.Code != 200 {
		t.Fatalf("删除应 200: %d", wd.Code)
	}
	wl2 := httptest.NewRecorder()
	s.listProjects(wl2, httptest.NewRequest("GET", "/api/v1/projects", nil))
	if strings.Contains(wl2.Body.String(), "订单重构") {
		t.Fatal("删除后列表不应含项目")
	}
}

func TestTaskCommentsAndSteerHTTP(t *testing.T) {
	s, st, ctx, _, tid := multicaServer(t)

	// 评论 @agent 唤醒（mock 网关回帖）→ 线程 2 条（人 + agent）
	rc := httptest.NewRequest("POST", "/api/v1/tasks/"+itoaInt64(tid)+"/comments",
		strings.NewReader(`{"content":"@reviewer 这个方案怎么看"}`))
	rc.SetPathValue("id", itoaInt64(tid))
	wc := httptest.NewRecorder()
	s.postTaskComment(wc, rc)
	if wc.Code != 201 {
		t.Fatalf("评论应 201: %d %s", wc.Code, wc.Body.String())
	}
	comments, _ := st.ListTaskComments(ctx, tid)
	if len(comments) != 2 {
		t.Fatalf("应 2 条（人+agent 唤醒回帖）: %d", len(comments))
	}
	if comments[1].AuthorType != "agent" || comments[1].AuthorName != "reviewer" {
		t.Fatalf("回帖作者不符: %+v", comments[1])
	}

	// 列表端点
	wl := httptest.NewRecorder()
	rq := httptest.NewRequest("GET", "/api/v1/tasks/"+itoaInt64(tid)+"/comments", nil)
	rq.SetPathValue("id", itoaInt64(tid))
	s.listTaskComments(wl, rq)
	if wl.Code != 200 || !strings.Contains(wl.Body.String(), "reviewer") {
		t.Fatalf("评论列表不符: %s", wl.Body.String())
	}

	// Steer：非 running 409
	rs := httptest.NewRequest("POST", "/api/v1/tasks/"+itoaInt64(tid)+"/steer",
		strings.NewReader(`{"message":"补充约束"}`))
	rs.SetPathValue("id", itoaInt64(tid))
	ws := httptest.NewRecorder()
	s.steerTask(ws, rs)
	if ws.Code != 409 {
		t.Fatalf("非 running steer 应 409: %d", ws.Code)
	}
	// running → 200 + steer_note 落库 + 评论留痕
	_ = st.UpdateTask(ctx, tid, store.TaskPatch{Status: ptrStatus(model.TaskRunning)})
	rs2 := httptest.NewRequest("POST", "/api/v1/tasks/"+itoaInt64(tid)+"/steer",
		strings.NewReader(`{"message":"优先校验边界"}`))
	rs2.SetPathValue("id", itoaInt64(tid))
	ws2 := httptest.NewRecorder()
	s.steerTask(ws2, rs2)
	if ws2.Code != 200 {
		t.Fatalf("running steer 应 200: %d %s", ws2.Code, ws2.Body.String())
	}
	t2, _ := st.GetTask(ctx, tid)
	if t2.SteerNote == "" || !strings.Contains(t2.SteerNote, "优先校验边界") {
		t.Fatalf("steer_note 未落库: %q", t2.SteerNote)
	}
	comments2, _ := st.ListTaskComments(ctx, tid)
	last := comments2[len(comments2)-1]
	if !strings.Contains(last.Content, "优先校验边界") {
		t.Fatalf("steer 未留痕评论线程: %+v", last)
	}
}

func TestConversationsHTTP(t *testing.T) {
	s, st, ctx, aid, _ := multicaServer(t)
	cid, _ := st.CreateConversation(ctx, &model.Conversation{AgentID: aid, Title: "试聊"})
	_, _ = st.AppendMessage(ctx, &model.Message{ConversationID: cid, Role: "user", Content: "你好"})
	_, _ = st.AppendMessage(ctx, &model.Message{ConversationID: cid, Role: "agent", Content: "你好，我是 reviewer"})

	// 列表（带消息数）
	wl := httptest.NewRecorder()
	s.listConversations(wl, httptest.NewRequest("GET", "/api/v1/conversations", nil))
	if wl.Code != 200 || !strings.Contains(wl.Body.String(), "试聊") {
		t.Fatalf("会话列表不符: %s", wl.Body.String())
	}
	// 详情（全量消息）
	rg := httptest.NewRequest("GET", "/api/v1/conversations/"+itoaInt64(cid), nil)
	rg.SetPathValue("id", itoaInt64(cid))
	wg := httptest.NewRecorder()
	s.getConversation(wg, rg)
	if wg.Code != 200 {
		t.Fatalf("会话详情应 200: %d", wg.Code)
	}
	var detail struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(wg.Body.Bytes(), &detail)
	if len(detail.Messages) != 2 || detail.Messages[1].Content != "你好，我是 reviewer" {
		t.Fatalf("详情消息不符: %+v", detail.Messages)
	}
}
