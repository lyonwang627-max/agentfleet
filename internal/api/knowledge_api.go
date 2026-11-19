package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- 知识库（K 系列）：管理 + 检索 ----

func (s *Server) createKB(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 必填"))
		return
	}
	kb := &model.KnowledgeBase{Name: in.Name, Description: in.Description}
	id, err := s.Store.CreateKB(r.Context(), kb)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "kb.update", map[string]any{"kb_id": id, "action": "created"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listKB(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListKB(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) deleteKB(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteKB(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "deleted": true})
}

func (s *Server) saveKBDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil ||
		strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Content) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 与 content 必填"))
		return
	}
	doc := &model.KBDoc{KBID: pathID(r), Name: in.Name, Content: in.Content}
	id, err := s.Store.SaveKBDoc(r.Context(), doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "kb.update", map[string]any{"kb_id": pathID(r), "doc_id": id, "action": "doc_saved"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "chunks": doc.ID})
}

func (s *Server) listKBDocs(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListKBDocs(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) getKBDoc(w http.ResponseWriter, r *http.Request) {
	doc, err := s.Store.GetKBDoc(r.Context(), pathID2(r, "docId"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) deleteKBDoc(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteKBDoc(r.Context(), pathID2(r, "docId")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "deleted": true})
}

// searchKB 检索端点：?q=&kb_id=&top=3
func (s *Server) searchKB(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, errors.New("q 必填"))
		return
	}
	kbID := int64(queryInt(r, "kb_id", 0))
	top := queryInt(r, "top", 3)
	if top <= 0 || top > 10 {
		top = 3
	}
	hits, err := s.Store.SearchKB(r.Context(), kbID, q, top)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits, "total": len(hits)})
}
