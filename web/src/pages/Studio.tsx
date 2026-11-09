import { useState } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import type { Skill, Workspace } from '../types';
import { Btn, ErrorText, Panel, SkeletonBlock, fmtTime } from '../ui';

// 工作台：执行环境（workspaces）与经验库（skills）——任务创建时可绑定/引用。
export default function Studio() {
  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
      <WorkspacesPanel />
      <SkillsPanel />
    </div>
  );
}

function WorkspacesPanel() {
  const list = useApi(() => api.listWorkspaces(), []);
  const [form, setForm] = useState({ name: '', root_path: '', branch: '', note: '' });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const create = async () => {
    if (!form.name.trim()) {
      setErr('名称必填');
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      await api.createWorkspace(form);
      setForm({ name: '', root_path: '', branch: '', note: '' });
      await list.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const items = list.data?.items ?? [];
  const inputCls = 'w-full rounded border border-border bg-bg px-2.5 py-1.5 text-xs text-fg outline-none placeholder:text-muted focus:border-accent-dim';

  return (
    <Panel title={`执行环境 Workspaces（${items.length}）`}>
      <div className="space-y-2.5">
        <div className="grid grid-cols-2 gap-2">
          <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="名称，如 release-hotfix" className={inputCls} />
          <input value={form.branch} onChange={(e) => setForm({ ...form, branch: e.target.value })} placeholder="分支（可选）" className={inputCls} />
          <input value={form.root_path} onChange={(e) => setForm({ ...form, root_path: e.target.value })} placeholder="根路径（可选）" className={inputCls} />
          <input value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} placeholder="备注（可选）" className={inputCls} />
        </div>
        {err && <ErrorText>{err}</ErrorText>}
        <Btn variant="primary" disabled={busy} onClick={() => void create()}>新建环境</Btn>
        {list.loading && <SkeletonBlock rows={3} />}
        <div className="divide-y divide-border/60 rounded border border-border">
          {items.length === 0 && !list.loading && (
            <div className="px-3 py-6 text-center text-xs text-muted">暂无执行环境</div>
          )}
          {items.map((w: Workspace) => (
            <div key={w.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-xs">
              <span className="rounded border border-ok/40 bg-ok/10 px-1.5 py-0.5 text-[11px] text-ok">{w.status}</span>
              <span className="font-medium text-fg">{w.name}</span>
              <span className="font-mono text-[11px] text-accent">{w.branch || '—'}</span>
              <span className="truncate font-mono text-[11px] text-muted" title={w.root_path}>{w.root_path || '—'}</span>
              <span className="ml-auto flex items-center gap-2">
                <span className="font-mono text-[10px] text-muted">{fmtTime(w.created_at)}</span>
                <Btn variant="danger" onClick={() => void api.deleteWorkspace(w.id).then(() => list.reload())}>删除</Btn>
              </span>
            </div>
          ))}
        </div>
        <div className="text-[11px] leading-relaxed text-muted">
          任务创建时通过 workspace_id 绑定环境；agent 在绑定环境中执行并产出环境内执行记录。
        </div>
      </div>
    </Panel>
  );
}

function SkillsPanel() {
  const list = useApi(() => api.listSkills(), []);
  const [form, setForm] = useState({ name: '', content: '', tags: '' });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const create = async () => {
    if (!form.name.trim() || !form.content.trim()) {
      setErr('名称与内容必填');
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      await api.createSkill({
        name: form.name, content: form.content,
        tags: form.tags.split(',').map((s) => s.trim()).filter(Boolean),
      });
      setForm({ name: '', content: '', tags: '' });
      await list.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const items = list.data?.items ?? [];
  const inputCls = 'w-full rounded border border-border bg-bg px-2.5 py-1.5 text-xs text-fg outline-none placeholder:text-muted focus:border-accent-dim';

  return (
    <Panel title={`经验库 Skills（${items.length}）`}>
      <div className="space-y-2.5">
        <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="名称，如 线上告警处置 SOP" className={inputCls} />
        <textarea value={form.content} onChange={(e) => setForm({ ...form, content: e.target.value })} rows={3} placeholder="可复用的操作要点 / SOP / 沉淀经验…" className={inputCls} />
        <input value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} placeholder="标签，逗号分隔（可选）" className={inputCls} />
        {err && <ErrorText>{err}</ErrorText>}
        <Btn variant="primary" disabled={busy} onClick={() => void create()}>沉淀经验</Btn>
        {list.loading && <SkeletonBlock rows={3} />}
        <div className="divide-y divide-border/60 rounded border border-border">
          {items.length === 0 && !list.loading && (
            <div className="px-3 py-6 text-center text-xs text-muted">暂无沉淀经验</div>
          )}
          {items.map((s: Skill) => (
            <div key={s.id} className="px-3 py-2 text-xs">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="font-medium text-fg">{s.name}</span>
                {s.tags.map((t) => (
                  <span key={t} className="rounded border border-violet-500/30 bg-violet-500/10 px-1.5 py-px text-[11px] text-violet-300">{t}</span>
                ))}
                <span className="ml-auto flex items-center gap-2">
                  <span className="font-mono text-[10px] text-muted">引用 {s.usage_count} 次</span>
                  <Btn variant="danger" onClick={() => void api.deleteSkill(s.id).then(() => list.reload())}>删除</Btn>
                </span>
              </div>
              <div className="mt-1 line-clamp-2 leading-relaxed text-muted" title={s.content}>{s.content}</div>
            </div>
          ))}
        </div>
        <div className="text-[11px] leading-relaxed text-muted">
          任务创建时通过 skill_ids 引用；注入只发生在执行副本，不污染任务指令（指令通道纯净性）。
        </div>
      </div>
    </Panel>
  );
}
