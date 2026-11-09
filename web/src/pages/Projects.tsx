import { useState } from 'react';
import { Link } from 'react-router-dom';
import { FolderKanban, Plus, Trash2, GitBranch, Tag } from 'lucide-react';
import { api } from '../api';
import { useApi } from '../hooks';
import type { Project } from '../types';
import { Btn, ErrorText, Panel, SkeletonBlock } from '../ui';

// 项目页（Multica Projects 对标）：任务分组容器，挂 repo 上下文。
export default function Projects() {
  const list = useApi(() => api.listProjects(), []);
  const [form, setForm] = useState({ name: '', description: '', repos: '' });
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
      await api.createProject({
        name: form.name,
        description: form.description,
        repos: form.repos.split(/[,\n]/).map((s) => s.trim()).filter(Boolean),
      });
      setForm({ name: '', description: '', repos: '' });
      await list.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const items = list.data?.items ?? [];

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[360px_minmax(0,1fr)]">
      <div className="self-start">
        <Panel title="新建项目">
          <div className="space-y-2.5">
            <input
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="项目名，如 订单服务重构"
              className={inputCls}
            />
            <input
              value={form.description}
              onChange={(e) => setForm({ ...form, description: e.target.value })}
              placeholder="一句话描述（可选）"
              className={inputCls}
            />
            <textarea
              value={form.repos}
              onChange={(e) => setForm({ ...form, repos: e.target.value })}
              rows={2}
              placeholder="关联仓库（逗号或换行分隔，可选）"
              className={inputCls}
            />
            {err && <ErrorText>{err}</ErrorText>}
            <Btn variant="primary" disabled={busy} onClick={() => void create()}>
              <Plus size={13} /> 创建项目
            </Btn>
          </div>
        </Panel>
      </div>

      <div className="self-start space-y-3">
        {list.loading && <Panel><SkeletonBlock rows={3} /></Panel>}
        {items.length === 0 && !list.loading && (
          <Panel>
            <div className="py-10 text-center text-xs text-muted">
              还没有项目——项目是任务的分组容器，可挂关联仓库作为 agent 上下文。
            </div>
          </Panel>
        )}
        {items.map((p: Project & { task_count?: number }) => (
          <div key={p.id} className="rounded-xl bg-panel px-5 py-4">
            <div className="flex items-center gap-3">
              <FolderKanban size={18} className="text-accent" />
              <span className="text-[14px] font-semibold">{p.name}</span>
              <Tag size={12} className="text-muted" />
              <span className="font-mono text-xs text-muted">{p.task_count ?? 0} 个任务</span>
              <Btn
                variant="ghost"
                className="ml-auto"
                onClick={() => void api.deleteProject(p.id).then(() => list.reload())}
              >
                <Trash2 size={13} />
              </Btn>
            </div>
            {p.description && <div className="mt-1.5 pl-8 text-xs text-muted">{p.description}</div>}
            {p.repos?.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-2 pl-8">
                {p.repos.map((r: string) => (
                  <span key={r} className="inline-flex items-center gap-1 rounded-md bg-white/5 px-2 py-0.5 font-mono text-[11px] text-muted">
                    <GitBranch size={11} /> {r}
                  </span>
                ))}
              </div>
            )}
            <div className="mt-2 pl-8">
              <Link to={`/tasks?project_id=${p.id}`} className="text-[11px] text-accent hover:underline">
                查看项目任务 →
              </Link>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

const inputCls =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-xs text-fg outline-none placeholder:text-muted focus:border-accent';
