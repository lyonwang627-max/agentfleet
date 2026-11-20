import { useState } from 'react';
import { BookOpen, Database, FileText, Plus, Search, Trash2 } from 'lucide-react';
import { api } from '../api';
import { useApi } from '../hooks';
import type { KBDoc, KBChunk, KnowledgeBase } from '../types';
import { Btn, ErrorText, Panel, SkeletonBlock, fmtTime } from '../ui';

// 知识库管理（K 系列）：建库/传文档/分段可见/检索试测——agent 记忆的陈述性知识底座。
export default function Knowledge() {
  const list = useApi(() => api.listKBs(), []);
  const [sel, setSel] = useState<number>(0);
  const selKB = list.data?.items.find((k) => k.id === sel) ?? null;
  const docs = useApi(() => api.listKBDocs(sel), [sel], sel > 0);

  // 建库表单
  const [kbForm, setKbForm] = useState({ name: '', description: '' });
  const [kbErr, setKbErr] = useState<string | null>(null);
  const [kbBusy, setKbBusy] = useState(false);

  // 文档表单
  const [docForm, setDocForm] = useState({ name: '', content: '' });
  const [docErr, setDocErr] = useState<string | null>(null);
  const [docBusy, setDocBusy] = useState(false);
  const [docSaved, setDocSaved] = useState('');

  // 检索试测
  const [q, setQ] = useState('');
  const [hits, setHits] = useState<KBChunk[] | null>(null);
  const [qBusy, setQBusy] = useState(false);
  const [qErr, setQErr] = useState<string | null>(null);

  const createKB = async () => {
    if (!kbForm.name.trim()) {
      setKbErr('库名必填');
      return;
    }
    setKbBusy(true);
    setKbErr(null);
    try {
      const r = await api.createKB({ name: kbForm.name, description: kbForm.description });
      setKbForm({ name: '', description: '' });
      await list.reload();
      setSel(r.id);
    } catch (e) {
      setKbErr(e instanceof Error ? e.message : String(e));
    } finally {
      setKbBusy(false);
    }
  };

  const saveDoc = async () => {
    if (!sel || !docForm.name.trim() || !docForm.content.trim()) {
      setDocErr('文档名与内容必填');
      return;
    }
    setDocBusy(true);
    setDocErr(null);
    try {
      const r = await api.saveKBDoc(sel, docForm);
      setDocForm({ name: '', content: '' });
      setDocSaved(`已入库 · 自动分段 ${r.chunks} 段`);
      await docs.reload();
      await list.reload();
    } catch (e) {
      setDocErr(e instanceof Error ? e.message : String(e));
    } finally {
      setDocBusy(false);
    }
  };

  const runSearch = async () => {
    if (!q.trim()) {
      return;
    }
    setQBusy(true);
    setQErr(null);
    try {
      const r = await api.searchKB(q, sel, 5);
      setHits(r.items);
    } catch (e) {
      setQErr(e instanceof Error ? e.message : String(e));
    } finally {
      setQBusy(false);
    }
  };

  const kbs = list.data?.items ?? [];

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[360px_minmax(0,1fr)]">
      <div className="self-start space-y-3">
        <Panel title="新建知识库">
          <div className="space-y-2.5">
            <input
              value={kbForm.name}
              onChange={(e) => setKbForm({ ...kbForm, name: e.target.value })}
              placeholder="库名，如 运维手册"
              className={inputCls}
            />
            <input
              value={kbForm.description}
              onChange={(e) => setKbForm({ ...kbForm, description: e.target.value })}
              placeholder="一句话描述（可选）"
              className={inputCls}
            />
            {kbErr && <ErrorText>{kbErr}</ErrorText>}
            <Btn variant="primary" disabled={kbBusy} onClick={() => void createKB()}>
              <Plus size={13} /> 创建
            </Btn>
          </div>
        </Panel>

        <Panel title="检索试测">
          <div className="space-y-2.5">
            <div className="flex gap-2">
              <input
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && void runSearch()}
                placeholder={selKB ? `在「${selKB.name}」中检索…` : '跨全部库检索…'}
                className={inputCls}
              />
              <Btn variant="primary" disabled={qBusy} onClick={() => void runSearch()}>
                <Search size={13} />
              </Btn>
            </div>
            {qErr && <ErrorText>{qErr}</ErrorText>}
            {hits !== null && hits.length === 0 && (
              <div className="text-[11px] text-muted">无命中——试拆关键词（空格分隔）</div>
            )}
            {hits?.map((h) => (
              <div key={h.id} className="rounded-lg border border-[var(--hairline)] px-3 py-2">
                <div className="flex items-center gap-2 font-mono text-[11px] text-muted">
                  <span className="text-accent">{h.doc_name ?? `doc#${h.doc_id}`}</span>
                  <span>seq {h.seq}</span>
                  {h.score != null && h.score > 0 && <span className="ml-auto">{h.score} 分</span>}
                </div>
                <div className="mt-1 line-clamp-3 text-[12px] leading-relaxed">{h.content}</div>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      <div className="self-start space-y-3">
        {list.loading && <Panel><SkeletonBlock rows={3} /></Panel>}
        {kbs.length === 0 && !list.loading && (
          <Panel>
            <div className="py-10 text-center text-xs text-muted">
              还没有知识库——知识库存放 agent 的陈述性知识（手册/规范/FAQ），
              任务绑定后执行时自动检索注入，agent 也可经 kb_search 工具主动查询。
            </div>
          </Panel>
        )}
        {kbs.map((k: KnowledgeBase) => (
          <div key={k.id} className="rounded-xl bg-panel px-5 py-4">
            <button className="flex w-full items-center gap-3 text-left" onClick={() => setSel(sel === k.id ? 0 : k.id)}>
              <Database size={18} className={sel === k.id ? 'text-accent' : 'text-muted'} />
              <span className="text-[14px] font-semibold">{k.name}</span>
              <span className="font-mono text-xs text-muted">{k.doc_count} 文档</span>
              <span className="font-mono text-[11px] text-muted">{fmtTime(k.created_at)}</span>
              <span
                role="button"
                tabIndex={0}
                className="ml-auto p-1 text-muted hover:text-bad"
                onClick={(e) => {
                  e.stopPropagation();
                  if (confirm(`删除知识库「${k.name}」及其全部文档？`)) {
                    void api.deleteKB(k.id).then(() => {
                      if (sel === k.id) setSel(0);
                      return list.reload();
                    });
                  }
                }}
                onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.click()}
              >
                <Trash2 size={13} />
              </span>
            </button>
            {k.description && <div className="mt-1.5 pl-8 text-xs text-muted">{k.description}</div>}

            {sel === k.id && (
              <div className="mt-4 space-y-3 border-t border-[var(--hairline)] pt-4 pl-0">
                {/* 文档列表 */}
                {docs.loading && <SkeletonBlock rows={2} />}
                {docs.data?.items.length === 0 && !docs.loading && (
                  <div className="text-[11px] text-muted">暂无文档——在下方粘贴首份文档。</div>
                )}
                {docs.data?.items.map((d: KBDoc) => (
                  <div key={d.id} className="flex items-center gap-3 rounded-lg border border-[var(--hairline)] px-3 py-2">
                    <FileText size={14} className="text-muted" />
                    <span className="text-[12px]">{d.name}</span>
                    <span className="font-mono text-[11px] text-muted">{d.chunks ?? 1} 段</span>
                    <span className="font-mono text-[11px] text-muted">{fmtTime(d.created_at)}</span>
                    <button
                      className="ml-auto p-1 text-muted hover:text-bad"
                      onClick={() => {
                        if (confirm(`删除文档「${d.name}」？`)) {
                          void api.deleteKBDoc(k.id, d.id).then(() => { void docs.reload(); void list.reload(); });
                        }
                      }}
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                ))}

                {/* 加文档 */}
                <div className="space-y-2 rounded-lg border border-[var(--hairline)] px-3 py-3">
                  <div className="flex items-center gap-2 text-[11px] text-muted">
                    <BookOpen size={12} /> 添加文档——粘贴原文，自动分段（≤500 字/段，段落边界优先）
                  </div>
                  <input
                    value={docForm.name}
                    onChange={(e) => setDocForm({ ...docForm, name: e.target.value })}
                    placeholder="文档名，如 发布流程"
                    className={inputCls}
                  />
                  <textarea
                    value={docForm.content}
                    onChange={(e) => setDocForm({ ...docForm, content: e.target.value })}
                    rows={6}
                    placeholder="粘贴文档原文（Markdown/纯文本均可）…"
                    className={inputCls}
                  />
                  {docErr && <ErrorText>{docErr}</ErrorText>}
                  <div className="flex items-center gap-3">
                    <Btn variant="primary" disabled={docBusy} onClick={() => void saveDoc()}>
                      <Plus size={13} /> 入库
                    </Btn>
                    {docSaved && <span className="text-[11px] text-accent">{docSaved}</span>}
                  </div>
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

const inputCls =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-xs text-fg outline-none placeholder:text-muted focus:border-accent';
