package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- 知识库（K 系列）：文档集管理 + 关键词检索 ----
// 检索策略：个人规模零 embedding——关键词命中计分（子串出现次数 ×10 聚合），
// 升级触发（单库 >200 分段或语义检索需求）再引入 pgvector（pg 已有基建零新依赖）。

// SplitChunks 自动分段：≤500 rune/段，优先按空行（段落）边界切，段内超长按行切。
func SplitChunks(content string, maxRunes int) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	// 先按空行切段
	paras := strings.Split(content, "\n\n")
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, para := range paras {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if cur.Len() > 0 && cur.Len()+len([]rune(para)) > maxRunes {
			flush()
		}
		if len([]rune(para)) <= maxRunes {
			cur.WriteString(para)
			cur.WriteString("\n\n")
			continue
		}
		// 单段超长：按行硬切
		flush()
		for _, line := range strings.Split(para, "\n") {
			if cur.Len() > 0 && cur.Len()+len([]rune(line))+1 > maxRunes {
				flush()
			}
			cur.WriteString(line)
			cur.WriteString("\n")
		}
		flush()
	}
	flush()
	return out
}

// ---- MemStore ----

func (m *MemStore) CreateKB(_ context.Context, kb *model.KnowledgeBase) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	kb.ID = m.nextID
	kb.CreatedAt = time.Now()
	cp := *kb
	m.kbs[kb.ID] = &cp
	return kb.ID, nil
}

func (m *MemStore) ListKB(_ context.Context) ([]model.KnowledgeBase, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.KnowledgeBase{}
	for _, kb := range m.kbs {
		cp := *kb
		for _, doc := range m.kbDocs {
			if doc.KBID == kb.ID {
				cp.DocCount++
			}
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemStore) GetKB(_ context.Context, id int64) (*model.KnowledgeBase, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	kb, ok := m.kbs[id]
	if !ok {
		return nil, fmt.Errorf("kb %d: %w", id, ErrNotFound)
	}
	cp := *kb
	for _, doc := range m.kbDocs {
		if doc.KBID == id {
			cp.DocCount++
		}
	}
	return &cp, nil
}

func (m *MemStore) DeleteKB(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.kbs[id]; !ok {
		return fmt.Errorf("kb %d: %w", id, ErrNotFound)
	}
	delete(m.kbs, id)
	for docID, doc := range m.kbDocs {
		if doc.KBID == id {
			delete(m.kbDocs, docID)
			delete(m.kbChunks, docID)
		}
	}
	return nil
}

func (m *MemStore) SaveKBDoc(_ context.Context, doc *model.KBDoc) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	doc.ID = m.nextID
	doc.CreatedAt = time.Now()
	cp := *doc
	m.kbDocs[doc.ID] = &cp
	chunks := SplitChunks(doc.Content, 500)
	models := make([]model.KBChunk, 0, len(chunks))
	for i, c := range chunks {
		models = append(models, model.KBChunk{DocID: doc.ID, KBID: doc.KBID, Seq: i + 1, Content: c})
	}
	m.kbChunks[doc.ID] = models
	return doc.ID, nil
}

func (m *MemStore) ListKBDocs(_ context.Context, kbID int64) ([]model.KBDoc, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []model.KBDoc{}
	for _, doc := range m.kbDocs {
		if doc.KBID == kbID {
			cp := *doc
			cp.Content = ""
			cp.Chunks = len(m.kbChunks[doc.ID])
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemStore) GetKBDoc(_ context.Context, docID int64) (*model.KBDoc, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	doc, ok := m.kbDocs[docID]
	if !ok {
		return nil, fmt.Errorf("kb doc %d: %w", docID, ErrNotFound)
	}
	cp := *doc
	return &cp, nil
}

func (m *MemStore) DeleteKBDoc(_ context.Context, docID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.kbDocs[docID]; !ok {
		return fmt.Errorf("kb doc %d: %w", docID, ErrNotFound)
	}
	delete(m.kbDocs, docID)
	delete(m.kbChunks, docID)
	return nil
}

// SearchKB 关键词检索：全 term 命中计分降序（topK 截断）；跨库（kbID=0）或单库。
func (m *MemStore) SearchKB(_ context.Context, kbID int64, query string, topK int) ([]model.KBChunk, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, nil
	}
	type hit struct {
		chunk model.KBChunk
		score int
	}
	var hits []hit
	for docID, chunks := range m.kbChunks {
		doc, ok := m.kbDocs[docID]
		if !ok {
			continue
		}
		if kbID > 0 && doc.KBID != kbID {
			continue
		}
		for _, ch := range chunks {
			lower := strings.ToLower(ch.Content)
			score := 0
			matched := 0
			for _, t := range terms {
				if c := strings.Count(lower, t); c > 0 {
					score += c * 10
					matched++
				}
			}
			// 全部 term 至少出现一次才算命中（避免单词弱相关刷分）
			if matched == len(terms) {
				hits = append(hits, hit{chunk: model.KBChunk{
					ID: ch.ID, DocID: ch.DocID, KBID: doc.KBID, Seq: ch.Seq,
					Content: ch.Content, DocName: doc.Name}, score: score})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if topK > 0 && len(hits) > topK {
		hits = hits[:topK]
	}
	out := []model.KBChunk{}
	for _, h := range hits {
		h.chunk.Score = h.score
		out = append(out, h.chunk)
	}
	return out, nil
}

// ---- PostgresStore ----

func (p *PostgresStore) CreateKB(ctx context.Context, kb *model.KnowledgeBase) (int64, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO knowledge_bases(name, description) VALUES ($1,$2) RETURNING id, created_at`,
		kb.Name, kb.Description).Scan(&kb.ID, &kb.CreatedAt)
	return kb.ID, err
}

func (p *PostgresStore) ListKB(ctx context.Context) ([]model.KnowledgeBase, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT d.id, d.name, d.description, d.created_at, (SELECT COUNT(*) FROM kb_docs c WHERE c.kb_id=d.id)
		 FROM knowledge_bases d ORDER BY d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.KnowledgeBase{}
	for rows.Next() {
		kb := model.KnowledgeBase{}
		if err := rows.Scan(&kb.ID, &kb.Name, &kb.Description, &kb.CreatedAt, &kb.DocCount); err != nil {
			return nil, err
		}
		out = append(out, kb)
	}
	return out, rows.Err()
}

func (p *PostgresStore) GetKB(ctx context.Context, id int64) (*model.KnowledgeBase, error) {
	kb := &model.KnowledgeBase{}
	err := p.pool.QueryRow(ctx,
		`SELECT d.id, d.name, d.description, d.created_at, (SELECT COUNT(*) FROM kb_docs c WHERE c.kb_id=d.id)
		 FROM knowledge_bases d WHERE d.id=$1`, id).
		Scan(&kb.ID, &kb.Name, &kb.Description, &kb.CreatedAt, &kb.DocCount)
	if err != nil {
		return nil, err
	}
	return kb, nil
}

func (p *PostgresStore) DeleteKB(ctx context.Context, id int64) error {
	// kb_docs/kb_chunks 由 ON DELETE CASCADE 级联
	ct, err := p.pool.Exec(ctx, `DELETE FROM knowledge_bases WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("kb %d: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresStore) SaveKBDoc(ctx context.Context, doc *model.KBDoc) (int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx,
		`INSERT INTO kb_docs(kb_id, name, content) VALUES ($1,$2,$3) RETURNING id, created_at`,
		doc.KBID, doc.Name, doc.Content).Scan(&doc.ID, &doc.CreatedAt)
	if err != nil {
		return 0, err
	}
	chunks := SplitChunks(doc.Content, 500)
	batch := &pgx.Batch{}
	for i, c := range chunks {
		batch.Queue(`INSERT INTO kb_chunks(doc_id, kb_id, seq, content) VALUES ($1,$2,$3,$4)`,
			doc.ID, doc.KBID, i+1, c)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return doc.ID, nil
}

func (p *PostgresStore) ListKBDocs(ctx context.Context, kbID int64) ([]model.KBDoc, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, name, (SELECT COUNT(*) FROM kb_chunks c WHERE c.doc_id=d.id), created_at FROM kb_docs WHERE kb_id=$1 ORDER BY id`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.KBDoc{}
	for rows.Next() {
		doc := model.KBDoc{KBID: kbID}
		if err := rows.Scan(&doc.ID, &doc.Name, &doc.Chunks, &doc.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func (p *PostgresStore) GetKBDoc(ctx context.Context, docID int64) (*model.KBDoc, error) {
	doc := &model.KBDoc{}
	err := p.pool.QueryRow(ctx,
		`SELECT id, kb_id, name, content, created_at FROM kb_docs WHERE id=$1`, docID).
		Scan(&doc.ID, &doc.KBID, &doc.Name, &doc.Content, &doc.CreatedAt)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func (p *PostgresStore) DeleteKBDoc(ctx context.Context, docID int64) error {
	// chunks 由 ON DELETE CASCADE 级联
	ct, err := p.pool.Exec(ctx, `DELETE FROM kb_docs WHERE id=$1`, docID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("kb doc %d: %w", docID, ErrNotFound)
	}
	return nil
}

// SearchKB 关键词检索：ILIKE 全 term 命中计分（pg 侧按命中 term 数 × 出现计数排序）。
func (p *PostgresStore) SearchKB(ctx context.Context, kbID int64, query string, topK int) ([]model.KBChunk, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, nil
	}
	sql := `SELECT c.id, c.doc_id, c.kb_id, c.seq, c.content, d.name
		 FROM kb_chunks c JOIN kb_docs d ON d.id = c.doc_id
		 WHERE c.kb_id = $1 OR $1 = 0`
	args := []any{kbID}
	for i, t := range terms {
		sql += ` AND c.content ILIKE $` + fmt.Sprint(len(args)+1)
		args = append(args, "%"+t+"%")
		_ = i
	}
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scored := map[int64]*model.KBChunk{}
	for rows.Next() {
		ch := model.KBChunk{}
		if err := rows.Scan(&ch.ID, &ch.DocID, &ch.KBID, &ch.Seq, &ch.Content, &ch.DocName); err != nil {
			return nil, err
		}
		lower := strings.ToLower(ch.Content)
		score := 0
		for _, t := range terms {
			score += strings.Count(lower, t) * 10
		}
		if exist, ok := scored[ch.ID]; !ok || exist.Score < score {
			ch.Score = score
			scored[ch.ID] = &ch
		}
	}
	out := []model.KBChunk{}
	for _, ch := range scored {
		out = append(out, *ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if topK > 0 && len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}
