import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useApi, useEventSource, useThrottledReload } from '../hooks';
import type { AgentSummary, TaskStatus, TaskSummary } from '../types';
import { Btn, ErrorText, Panel, PriorityBadge, SkeletonBlock, StatusBadge, fmtTime } from '../ui';

const COLS: { key: TaskStatus; label: string }[] = [
  { key: 'queued', label: '排队中' },
  { key: 'running', label: '运行中' },
  { key: 'review', label: '待审核' },
  { key: 'done', label: '已完成' },
  { key: 'failed', label: '失败' },
];

export default function Tasks() {
  const [tasks, setTasks] = useState<TaskSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actingId, setActingId] = useState<number | null>(null);
  const [view, setView] = useState<'board' | 'dlq'>('board');
  const agents = useApi(() => api.listAgents(), []);

  const load = useCallback(async () => {
    try {
      const page = await api.listTasks({ page_size: 200, dlq: view === 'dlq' });
      setTasks(page.items);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [view]);

  useEffect(() => {
    void load();
  }, [load]);

  // 全局 SSE：任务/agent 相关事件到达 → 节流 refetch 当前页数据
  const throttled = useThrottledReload(load);
  useEventSource(() => throttled());

  const act = async (t: TaskSummary, action: 'retry' | 'cancel' | 'approve' | 'replay') => {
    setActingId(t.id);
    try {
      await api.taskAction(t.id, action);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setActingId(null);
    }
  };

  const byStatus = new Map<TaskStatus, TaskSummary[]>();
  for (const c of COLS) byStatus.set(c.key, []);
  const canceled: TaskSummary[] = [];
  for (const t of tasks) {
    if (t.status === 'canceled') canceled.push(t);
    else byStatus.get(t.status)?.push(t);
  }

  return (
    <div className="flex min-h-full flex-col gap-4">
      <div className="flex items-center justify-between">
        <NewTaskForm agents={agents.data?.items ?? []} onCreated={() => void load()} />
        <div className="flex overflow-hidden rounded border border-border">
          <button type="button" onClick={() => setView('board')}
            className={`px-3 py-1 text-xs transition-colors ${view === 'board' ? 'bg-sky-600/25 font-medium text-sky-300' : 'bg-panel2 text-muted hover:text-fg'}`}>
            看板
          </button>
          <button type="button" onClick={() => setView('dlq')}
            className={`px-3 py-1 text-xs transition-colors ${view === 'dlq' ? 'bg-rose-600/25 font-medium text-rose-300' : 'bg-panel2 text-muted hover:text-fg'}`}>
            死信队列
          </button>
        </div>
      </div>

      {error && <ErrorText>{error}（将随 SSE 事件自动重试刷新）</ErrorText>}

      {loading ? (
        <SkeletonBlock rows={5} />
      ) : view === 'dlq' ? (
        <Panel title={`死信队列（${tasks.length}）——重试耗尽的任务`} bodyClass="p-0">
          {tasks.length === 0 && (
            <div className="px-4 py-10 text-center text-xs text-muted">无死信任务——没有重试耗尽的失败任务</div>
          )}
          {tasks.map((t) => (
            <div key={t.id} className="flex flex-wrap items-center gap-3 border-b border-border/60 px-4 py-2.5 text-xs last:border-b-0">
              <span className="font-mono text-muted">#{t.id}</span>
              <Link to={`/tasks/${t.id}`} className="max-w-72 truncate font-medium text-fg hover:text-sky-300">{t.title}</Link>
              <StatusBadge status={t.status} />
              <span className="text-muted">agent {t.agent_name}</span>
              <span className="ml-auto font-mono text-[10px] text-muted">{fmtTime(t.created_at)}</span>
              <Btn variant="primary" disabled={actingId === t.id}
                onClick={() => void act(t, 'replay')}>
                重放（清零重试预算）
              </Btn>
            </div>
          ))}
        </Panel>
      ) : (
        <>
          <div className="text-xs text-muted">
            共 {tasks.length} 个任务{canceled.length > 0 && <> · 已取消 {canceled.length}（不在看板展示）</>}
          </div>
          <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
            {COLS.map((c) => {
              const list = byStatus.get(c.key) ?? [];
              return (
                <div key={c.key} className="flex min-h-0 flex-col rounded-lg border border-border bg-panel/60">
                  <div className="flex items-center justify-between border-b border-border px-3 py-2">
                    <span className="text-xs font-semibold">{c.label}</span>
                    <span className="rounded bg-panel2 px-1.5 py-0.5 font-mono text-[11px] tabular-nums text-muted">
                      {list.length}
                    </span>
                  </div>
                  <div className="flex-1 space-y-2 overflow-y-auto p-2">
                    {list.length === 0 && <div className="py-6 text-center text-[11px] text-muted">空</div>}
                    {list.map((t) => (
                      <TaskCard key={t.id} t={t} busy={actingId === t.id} onAct={(a) => void act(t, a)} />
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}

function TaskCard({ t, busy, onAct }: { t: TaskSummary; busy: boolean; onAct: (a: 'retry' | 'cancel' | 'approve') => void }) {
  return (
    <div className="rounded-md border border-border bg-panel p-2.5">
      <Link to={`/tasks/${t.id}`} className="line-clamp-2 text-xs font-medium leading-relaxed hover:text-sky-300">
        {t.title}
      </Link>
      <div className="mt-2 flex flex-wrap items-center gap-1.5 text-[11px] text-muted">
        <PriorityBadge p={t.priority} />
        <StatusBadge status={t.status} />
        <span className="truncate">{t.agent_name || `agent#${t.agent_id}`}</span>
      </div>
      {(t.blocked_by?.length ?? 0) > 0 && (
        <div className="mt-1.5 text-[11px] text-amber-400/80">阻塞于 #{t.blocked_by.join(', #')}</div>
      )}
      <div className="mt-2 flex items-center justify-between gap-1">
        <span className="font-mono text-[10px] text-muted">{fmtTime(t.created_at)}</span>
        <div className="flex gap-1">
          {t.status === 'review' && (
            <Btn variant="primary" disabled={busy} onClick={() => onAct('approve')}>
              通过
            </Btn>
          )}
          {t.status === 'failed' && (
            <Btn variant="primary" disabled={busy} onClick={() => onAct('retry')}>
              重试
            </Btn>
          )}
          {(t.status === 'queued' || t.status === 'running' || t.status === 'review') && (
            <Btn variant="danger" disabled={busy} onClick={() => onAct('cancel')}>
              取消
            </Btn>
          )}
        </div>
      </div>
    </div>
  );
}

function NewTaskForm({ agents, onCreated }: { agents: AgentSummary[]; onCreated: () => void }) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [prompt, setPrompt] = useState('');
  const [agentId, setAgentId] = useState('');
  const [priority, setPriority] = useState(5);
  const [dependsOn, setDependsOn] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const firstAgent = useRef<string>('');

  if (agents.length > 0 && !firstAgent.current) firstAgent.current = String(agents[0].id);

  const submit = async () => {
    const aid = Number(agentId || firstAgent.current);
    if (!title.trim() || !prompt.trim() || !aid) {
      setErr('标题、prompt、agent 均必填');
      return;
    }
    const depends = dependsOn
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
      .map(Number)
      .filter((n) => Number.isInteger(n) && n > 0);
    setSubmitting(true);
    setErr(null);
    try {
      await api.createTask({ title: title.trim(), prompt: prompt.trim(), agent_id: aid, priority, depends_on: depends });
      setTitle('');
      setPrompt('');
      setDependsOn('');
      onCreated();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const inputCls =
    'w-full rounded border border-border bg-bg px-2.5 py-1.5 text-xs text-fg outline-none placeholder:text-muted focus:border-sky-600/60';

  return (
    <Panel
      title="新建任务"
      right={
        <Btn variant="ghost" onClick={() => setOpen((v) => !v)}>
          {open ? '收起' : '展开'}
        </Btn>
      }
    >
      {open && (
        <div className="space-y-2.5">
          <div className="grid grid-cols-1 gap-2.5 md:grid-cols-4">
            <input className={inputCls} placeholder="任务标题" value={title} onChange={(e) => setTitle(e.target.value)} />
            <select className={inputCls} value={agentId || firstAgent.current} onChange={(e) => setAgentId(e.target.value)}>
              {agents.length === 0 && <option value="">无可用 Agent</option>}
              {agents.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}（{a.enabled ? '启用' : '停用'}）
                </option>
              ))}
            </select>
            <label className="flex items-center gap-2 text-xs text-muted">
              优先级 <span className="font-mono text-fg">P{priority}</span>
              <input
                type="range"
                min={1}
                max={9}
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
                className="flex-1 accent-sky-500"
              />
            </label>
            <input
              className={inputCls}
              placeholder="依赖任务 id（逗号分隔，如 3,7）"
              value={dependsOn}
              onChange={(e) => setDependsOn(e.target.value)}
            />
          </div>
          <textarea
            className={`${inputCls} h-20 resize-none font-mono`}
            placeholder="任务 prompt：详细任务说明…"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
          {err && <ErrorText>{err}</ErrorText>}
          <div className="flex justify-end">
            <Btn variant="primary" disabled={submitting} onClick={() => void submit()}>
              {submitting ? '提交中…' : '创建任务'}
            </Btn>
          </div>
        </div>
      )}
      {!open && <div className="text-xs text-muted">展开填写标题 / prompt / agent / 优先级 / 依赖任务</div>}
    </Panel>
  );
}
