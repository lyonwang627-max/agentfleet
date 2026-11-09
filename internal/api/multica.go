package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Multica 对标功能 API：Projects / TaskComments（@ 唤醒）/ Steer（运行中插话）。

// ---- Projects ----

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Repos       []string `json:"repos"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 必填"))
		return
	}
	pr := &model.Project{Name: in.Name, Description: in.Description, Repos: in.Repos}
	id, err := s.Store.CreateProject(r.Context(), pr)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "project.update", map[string]any{"project_id": id, "action": "created"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Store.ListProjects(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// 附各项目任务计数
	type row struct {
		model.Project
		TaskCount int `json:"task_count"`
	}
	out := make([]row, 0, len(projects))
	tasks, _, _ := s.Store.ListTasks(r.Context(), nil, 0, 1, 200)
	for _, pr := range projects {
		ro := row{Project: pr}
		for _, t := range tasks {
			if t.ProjectID == pr.ID {
				ro.TaskCount++
			}
		}
		out = append(out, ro)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteProject(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "deleted": true})
}

// ---- Task Comments（线程 + @agent 唤醒接任务上下文） ----

func (s *Server) listTaskComments(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListTaskComments(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) postTaskComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Content) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("content 必填"))
		return
	}
	taskID := pathID(r)
	task, err := s.Store.GetTask(r.Context(), taskID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	c := &model.TaskComment{TaskID: taskID, AuthorType: "member", AuthorName: "you", Content: in.Content}
	if _, err := s.Store.AddTaskComment(r.Context(), c); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "task.comment", map[string]any{"task_id": taskID, "author": "you"})

	// @ 唤醒：评论里 @agent → 该 agent 读任务+评论线程，以任务上下文回帖
	mentions := parseMentions(in.Content)
	var replies []roomReply
	for _, name := range mentions {
		if len(replies) >= 2 { // 单条评论最多唤醒 2 个
			break
		}
		agent, err := s.Store.GetAgentByName(r.Context(), name)
		if err != nil || !agent.Enabled {
			continue
		}
		content, _, err := s.agentReplyOnTask(r.Context(), task, agent, in.Content)
		if err != nil {
			content = "（回帖失败：" + err.Error() + "）"
		}
		rc := &model.TaskComment{TaskID: taskID, AuthorType: "agent", AuthorName: agent.Name, Content: content}
		_, _ = s.Store.AddTaskComment(r.Context(), rc)
		replies = append(replies, roomReply{AgentName: agent.Name, Content: content})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": c.ID, "replies": replies})
}

// agentReplyOnTask：agent 以任务上下文回帖（任务 + 评论线程 + 触发评论）。
func (s *Server) agentReplyOnTask(ctx context.Context, task *model.Task, agent *model.Agent, trigger string) (string, int, error) {
	comments, _ := s.Store.ListTaskComments(ctx, task.ID)
	var tl strings.Builder
	for _, c := range comments {
		author := c.AuthorName
		if c.AuthorType == "member" {
			author = "用户(" + author + ")"
		}
		head := c.Content
		if len([]rune(head)) > 200 {
			head = string([]rune(head)[:200]) + "…"
		}
		tl.WriteString(author + ": " + head + "\n")
	}
	prompt := fmt.Sprintf(`你在任务评论线程中被 @。任务：#%d %s
说明：%s
当前状态：%s
评论线程：
%s

请以 %s 的身份回应触发评论（最新一条），简洁（3 句内），只输出回帖内容：`, task.ID, task.Title, task.Prompt, task.Status, tl.String(), agent.Name)
	return s.LLM.Chat(ctx, agent.SystemPrompt, prompt, 500)
}

// ---- Steer：运行中插话 ----

func (s *Server) steerTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Message) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("message 必填"))
		return
	}
	taskID := pathID(r)
	task, err := s.Store.GetTask(r.Context(), taskID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if task.Status != model.TaskRunning {
		writeErr(w, http.StatusConflict, errors.New("仅运行中的任务可插话（Steer）"))
		return
	}
	note := "[运行中插话] " + strings.TrimSpace(in.Message)
	if err := s.Store.UpdateTask(r.Context(), taskID, store.TaskPatch{SteerNote: &note}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// 同时落评论线程留痕
	_, _ = s.Store.AddTaskComment(r.Context(), &model.TaskComment{
		TaskID: taskID, AuthorType: "member", AuthorName: "you",
		Content: "🛞 " + strings.TrimSpace(in.Message)})
	s.publish(r.Context(), "task.steer", map[string]any{"task_id": taskID})
	writeJSON(w, http.StatusOK, map[string]any{"id": taskID, "steered": true,
		"hint": "插话将在下一个工具步/决策边界注入当前 run"})
}

func hasLabelLocal(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
