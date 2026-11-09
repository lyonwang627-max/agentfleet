package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 小队管理 API（I30）。

func (s *Server) createSquad(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string  `json:"name"`
		Description  string  `json:"description"`
		LeaderID     int64   `json:"leader_id"`
		Instructions string  `json:"instructions"`
		MemberIDs    []int64 `json:"member_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" || in.LeaderID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("name 与 leader_id 必填"))
		return
	}
	// leader 必须是存在的 agent
	if _, err := s.Store.GetAgent(r.Context(), in.LeaderID); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("leader_id 必须指向存在的 agent"))
		return
	}
	sq := &model.Squad{Name: in.Name, Description: in.Description,
		LeaderID: in.LeaderID, Instructions: in.Instructions}
	id, err := s.Store.CreateSquad(r.Context(), sq)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, mid := range in.MemberIDs {
		if mid == in.LeaderID {
			continue // leader 不重复入列
		}
		_ = s.Store.AddSquadMember(r.Context(), &model.SquadMember{
			SquadID: id, MemberType: "agent", MemberID: mid})
	}
	s.publish(r.Context(), "squad.update", map[string]any{"squad_id": id, "action": "created"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listSquads(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListSquads(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// 附成员名（Roster 预览）
	type row struct {
		model.Squad
		LeaderName string   `json:"leader_name"`
		Members    []string `json:"members"`
	}
	out := make([]row, 0, len(items))
	for _, sq := range items {
		ro := row{Squad: sq}
		if a, err := s.Store.GetAgent(r.Context(), sq.LeaderID); err == nil {
			ro.LeaderName = a.Name
		}
		mems, _ := s.Store.ListSquadMembers(r.Context(), sq.ID)
		for _, m := range mems {
			if m.MemberType == "agent" {
				if a, err := s.Store.GetAgent(r.Context(), m.MemberID); err == nil {
					ro.Members = append(ro.Members, a.Name)
				}
			} else {
				ro.Members = append(ro.Members, "human#"+strconv.FormatInt(m.MemberID, 10))
			}
		}
		out = append(out, ro)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (s *Server) getSquad(w http.ResponseWriter, r *http.Request) {
	sq, err := s.Store.GetSquad(r.Context(), pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, sq)
}

func (s *Server) deleteSquad(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteSquad(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "deleted": true})
}

func (s *Server) addSquadMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentID int64 `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AgentID <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("agent_id 必填"))
		return
	}
	squadID := pathID(r)
	if _, err := s.Store.GetSquad(r.Context(), squadID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err := s.Store.AddSquadMember(r.Context(), &model.SquadMember{
		SquadID: squadID, MemberType: "agent", MemberID: in.AgentID}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"squad_id": squadID, "added": in.AgentID})
}

func (s *Server) removeSquadMember(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RemoveSquadMember(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "removed": true})
}
