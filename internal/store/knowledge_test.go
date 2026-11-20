package store

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 知识库（K1）：CRUD/自动分段/关键词检索计分。
func TestKBCRUDAndChunking(t *testing.T) {
	m := NewMemStore()
	ctx := context.Background()
	kid, _ := m.CreateKB(ctx, &model.KnowledgeBase{Name: "运维手册", Description: "db/runbook"})
	// 长文档 → 自动分段（>500 rune）
	long := strings.Repeat("第 paragraph 段内容，含 db 连接池与 P99 告警处置。\n\n", 20)
	docID, err := m.SaveKBDoc(ctx, &model.KBDoc{KBID: kid, Name: "runbook-db", Content: long})
	if err != nil {
		t.Fatal(err)
	}
	docs, _ := m.ListKBDocs(ctx, kid)
	if len(docs) != 1 || docs[0].Chunks < 2 {
		t.Fatalf("长文档应多段: chunks=%d", docs[0].Chunks)
	}
	// 检索：全 term 命中计分
	hits, err := m.SearchKB(ctx, kid, "db 连接池", 3)
	if err != nil || len(hits) == 0 {
		t.Fatalf("检索应命中: %v err=%v", hits, err)
	}
	if hits[0].Score <= 0 || hits[0].DocName != "runbook-db" {
		t.Fatalf("命中计分/来源不符: %+v", hits[0])
	}
	// 无关词 → 无命中
	if hits2, _ := m.SearchKB(ctx, kid, "完全无关词组xyz", 3); len(hits2) != 0 {
		t.Fatalf("无关词不应命中: %d", len(hits2))
	}
	// 删库级联
	if err := m.DeleteKB(ctx, kid); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetKBDoc(ctx, docID); err == nil {
		t.Fatal("级联删除后文档应不存在")
	}
}

func TestKBDocCRUD(t *testing.T) {
	m := NewMemStore()
	ctx := context.Background()
	kid, _ := m.CreateKB(ctx, &model.KnowledgeBase{Name: "k"})
	d1, _ := m.SaveKBDoc(ctx, &model.KBDoc{KBID: kid, Name: "a", Content: "内容A"})
	d2, _ := m.SaveKBDoc(ctx, &model.KBDoc{KBID: kid, Name: "b", Content: "内容B"})
	got, err := m.GetKBDoc(ctx, d1)
	if err != nil || got.Content != "内容A" {
		t.Fatalf("详情往返不符: %+v err=%v", got, err)
	}
	_ = m.DeleteKBDoc(ctx, d2)
	docs, _ := m.ListKBDocs(ctx, kid)
	if len(docs) != 1 {
		t.Fatalf("删除后应剩 1: %d", len(docs))
	}
}

// 中文感知切词（K2 修复）：中文段拆 2-gram 可命中，ASCII 段整词无回归，无关词不命中。
func TestKBQueryTerms(t *testing.T) {
	grams := kbQueryTerms("发布检查")
	if len(grams) != 1 || len(grams[0]) != 3 {
		t.Fatalf("四字中文段应拆 3 个 2-gram: %v", grams)
	}
	ascii := kbQueryTerms("db staging")
	if len(ascii) != 2 || ascii[0][0] != "db" || ascii[1][0] != "staging" {
		t.Fatalf("ASCII 段应整词保留: %v", ascii)
	}

	ctx := context.Background()
	m := NewMemStore()
	kid, _ := m.CreateKB(ctx, &model.KnowledgeBase{Name: "k"})
	m.SaveKBDoc(ctx, &model.KBDoc{KBID: kid, Name: "d", Content: "发布前必须检查数据库只读副本状态。"})
	// 中文整短语（修复前必漏检）现在应命中
	hits, err := m.SearchKB(ctx, kid, "发布检查", 3)
	if err != nil || len(hits) == 0 {
		t.Fatalf("中文短语应命中: %v err=%v", hits, err)
	}
	// 段间 AND：任一段完全不相关则不命中
	if hits2, _ := m.SearchKB(ctx, kid, "发布 kubernetesargtokencustom", 3); len(hits2) != 0 {
		t.Fatalf("含不相关段不应命中: %d", len(hits2))
	}
}
