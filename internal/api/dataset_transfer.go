package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 评测数据集导入导出（I4）：数据集可移植（备份/分享），服务 F17 处方闭环的再评测工作流。

// DatasetExport 导出格式（契约稳定，可入 git）。
type DatasetExport struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	ExportedAt  string         `json:"exported_at"`
	Cases       []ExportedCase `json:"cases"`
}

type ExportedCase struct {
	Input    string         `json:"input"`
	Expected map[string]any `json:"expected"`
}

func (s *Server) exportDataset(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	datasets, err := s.Store.ListDatasets(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var target *model.EvalDataset
	for i := range datasets {
		if datasets[i].ID == id {
			target = &datasets[i]
			break
		}
	}
	if target == nil {
		writeErr(w, http.StatusNotFound, errors.New("数据集不存在"))
		return
	}
	cases, err := s.Store.ListCases(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	exported := []ExportedCase{}
	for _, c := range cases {
		exported = append(exported, ExportedCase{Input: c.Input, Expected: c.Expected})
	}
	out := DatasetExport{Name: target.Name, Description: target.Description,
		ExportedAt: target.CreatedAt.Format("2006-01-02"), Cases: exported}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q",
		"dataset-"+strings.ReplaceAll(target.Name, " ", "-")+".json"))
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) importDataset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Cases       []struct {
			Input    string         `json:"input"`
			Expected map[string]any `json:"expected"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 与 cases 必填"))
		return
	}
	if len(in.Cases) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("cases 不能为空"))
		return
	}
	did, err := s.Store.CreateDataset(r.Context(), &model.EvalDataset{Name: in.Name, Description: in.Description})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cases := make([]model.EvalCase, 0, len(in.Cases))
	for _, c := range in.Cases {
		if strings.TrimSpace(c.Input) == "" {
			continue
		}
		cases = append(cases, model.EvalCase{Input: c.Input, Expected: c.Expected})
	}
	if err := s.Store.SaveCases(r.Context(), did, cases); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publish(r.Context(), "eval.update", map[string]any{"dataset_id": did, "action": "imported", "cases": len(cases)})
	writeJSON(w, http.StatusCreated, map[string]any{"id": did, "cases": len(cases)})
}
