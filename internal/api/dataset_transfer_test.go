package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/scheduler"
	"github.com/lyonwang/agentfleet/internal/store"
)

// 数据集导入导出（I4）：导出→导入 roundtrip 内容保真；导入与导出独立成新数据集。
func TestDatasetExportImportRoundtrip(t *testing.T) {
	st := store.NewMemStore()
	hub := scheduler.NewHub(st)
	s := &Server{Store: st, Hub: hub, StartedAt: time.Now()}
	ctx := context.Background()

	// 源数据集
	did, _ := st.CreateDataset(ctx, &model.EvalDataset{Name: "源数据集", Description: "导出测试"})
	_ = st.SaveCases(ctx, did, []model.EvalCase{
		{Input: "题1", Expected: map[string]any{"points": []string{"要点A", "要点B"}}},
		{Input: "题2", Expected: map[string]any{"points": []string{"要点C"}, "threshold": 8.0}},
	})

	// 导出
	reqExp := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/eval-datasets/%d/export", did), nil)
	reqExp.SetPathValue("id", fmt.Sprint(did))
	wExp := httptest.NewRecorder()
	s.exportDataset(wExp, reqExp)
	if wExp.Code != 200 {
		t.Fatalf("导出应 200: %d", wExp.Code)
	}

	// 导入（把导出体原样灌回）
	reqImp := httptest.NewRequest("POST", "/api/v1/eval-datasets/import", bytes.NewReader(wExp.Body.Bytes()))
	wImp := httptest.NewRecorder()
	s.importDataset(wImp, reqImp)
	if wImp.Code != 201 {
		t.Fatalf("导入应 201: %d %s", wImp.Code, wImp.Body.String())
	}
	var impRes struct {
		ID    int64 `json:"id"`
		Cases int   `json:"cases"`
	}
	_ = json.NewDecoder(wImp.Body).Decode(&impRes)
	if impRes.Cases != 2 {
		t.Fatalf("导入 case 数应为 2: %+v", impRes)
	}

	// 内容保真：新数据集的 cases 与源一致
	cases, _ := st.ListCases(ctx, impRes.ID)
	if len(cases) != 2 || cases[0].Input != "题1" {
		t.Fatalf("roundtrip 内容失真: %+v", cases)
	}
	if cases[1].Expected["threshold"] != 8.0 {
		t.Fatalf("threshold 保真失败: %+v", cases[1].Expected)
	}

	// 空cases导入 → 400
	bad := httptest.NewRequest("POST", "/api/v1/eval-datasets/import",
		strings.NewReader(`{"name":"空","cases":[]}`))
	wBad := httptest.NewRecorder()
	s.importDataset(wBad, bad)
	if wBad.Code != 400 {
		t.Fatalf("空 cases 应 400: %d", wBad.Code)
	}
}
