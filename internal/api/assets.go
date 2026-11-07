package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

// Artifacts / Workspaces / Skills 三模块 API（对标 Multica worktree、Orca artifacts、compound skills）。

// ---- artifacts ----

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	taskID := int64(queryInt(r, "task_id", 0))
	items, err := s.Store.ListArtifacts(r.Context(), taskID, r.URL.Query().Get("content") == "true")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetArtifact(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// ---- workspaces ----

type workspaceIn struct {
	Name     string `json:"name"`
	RootPath string `json:"root_path"`
	Branch   string `json:"branch"`
	Note     string `json:"note"`
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var in workspaceIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 必填"))
		return
	}
	ws := &model.Workspace{Name: in.Name, RootPath: in.RootPath, Branch: in.Branch, Note: in.Note}
	id, err := s.Store.CreateWorkspace(r.Context(), ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "workspace.update", map[string]any{"workspace_id": id, "action": "created"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true"
	items, err := s.Store.ListWorkspaces(r.Context(), activeOnly)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, err := s.Store.GetWorkspace(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (s *Server) patchWorkspace(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	patch := store.WorkspacePatch{}
	if v, ok := in["name"].(string); ok {
		patch.Name = &v
	}
	if v, ok := in["branch"].(string); ok {
		patch.Branch = &v
	}
	if v, ok := in["note"].(string); ok {
		patch.Note = &v
	}
	if v, ok := in["status"].(string); ok {
		patch.Status = &v
	}
	if err := s.Store.UpdateWorkspace(r.Context(), pathID(r), patch); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r)})
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteWorkspace(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "deleted": true})
}

// ---- skills ----

type skillIn struct {
	Name       string   `json:"name"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	SourceTask int64    `json:"source_task"`
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var in skillIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Content) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name / content 必填"))
		return
	}
	sk := &model.Skill{Name: in.Name, Content: in.Content, Tags: in.Tags, SourceTask: in.SourceTask}
	id, err := s.Store.SaveSkill(r.Context(), sk)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListSkills(r.Context(), r.URL.Query().Get("tag"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.Store.GetSkill(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

func (s *Server) patchSkill(w http.ResponseWriter, r *http.Request) {
	var in skillIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id := pathID(r)
	sk, err := s.Store.GetSkill(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if strings.TrimSpace(in.Name) != "" {
		sk.Name = in.Name
	}
	if strings.TrimSpace(in.Content) != "" {
		sk.Content = in.Content
	}
	if in.Tags != nil {
		sk.Tags = in.Tags
	}
	if err := s.Store.UpdateSkill(r.Context(), sk); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteSkill(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "deleted": true})
}
