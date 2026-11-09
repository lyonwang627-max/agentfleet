import { useCallback, useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import { MessageSquare, Megaphone, Send } from 'lucide-react';
import { api } from '../api';
import { useApi, useEventSource, useThrottledReload } from '../hooks';
import type { RunStep, RunTrace, RunSummary, TaskComment } from '../types';
import {
  Btn,
  ErrorText,
  JsonBlock,
  KindBadge,
  Panel,
  SkeletonBlock,
  StatusBadge,
  fmtDuration,
  fmtNum,
  fmtTime,
} from '../ui';

export default function TaskDetail() {
  const params = useParams();
  const taskId = Number(params.id ?? 0);
  const task = useApi(() => api.getTask(taskId), [taskId], Number.isInteger(taskId) && taskId > 0);
  const runs = useApi(() => api.listTaskRuns(taskId), [taskId], Number.isInteger(taskId) && taskId > 0);
  const [openRunId, setOpenRunId] = useState<number | null>(null);
  const [comments, setComments] = useState<TaskComment[]>([]);
  const [commentInput, setCommentInput] = useState('');
  const [steerInput, setSteerInput] = useState('');
  const [posting, setPosting] = useState(false);
  const [trace, setTrace] = useState<RunTrace | null>(null);
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceErr, setTraceErr] = useState<string | null>(null);

  const throttled = useThrottledReload(
    useCallback(async () => {
      await task.reload();
      await runs.reload();
    }, [task, runs]),
  );
  useEventSource(() => void throttled());

  const viewTrace = async (runId: number) => {
    if (openRunId === runId) {
      setOpenRunId(null);
      setTrace(null);
      return;
    }
    setOpenRunId(runId);
    setTraceLoading(true);
    setTraceErr(null);
    try {
      setTrace(await api.getRun(runId));
    } catch (e) {
      setTraceErr(e instanceof Error ? e.message : String(e));
    } finally {
      setTraceLoading(false);
    }
  };

  if (task.loading) return <SkeletonBlock rows={6} />;
  if (task.error) return <ErrorText>{task.error}</ErrorText>;
  const t = task.data;
  if (!t) return null;

  // 评论线程 + Steer（Multica 对标）
  const loadComments = async () => {
    try {
      const res = await api.listTaskComments(t.id);
      setComments(res.items ?? []);
    } catch { /* 降级静默 */ }
  };
  useEffect(() => { void loadComments(); }, [t.id]);

  const postComment = async () => {
    const text = commentInput.trim();
    if (!text || posting) return;
    setPosting(true);
    try {
      await api.postTaskComment(t.id, text);
      setCommentInput('');
      await loadComments();
    } finally { setPosting(false); }
  };

  const doSteer = async () => {
    const text = steerInput.trim();
    if (!text) return;
    try {
      await api.steerTask(t.id, text);
      setSteerInput('');
      await loadComments();
    } catch { /* 非 running 会失败——按钮只在 running 显示 */ }
  };

  return (
    <div className="space-y-4">
      <TaskArtifacts taskId={t.id} />
      <CommentThread
        comments={comments}
        input={commentInput}
        setInput={setCommentInput}
        onPost={() => void postComment()}
        posting={posting}
      />
      {t.status === 'running' && (
        <div className="rounded-xl bg-panel px-5 py-3">
          <div className="mb-2 flex items-center gap-2 text-[13px] font-semibold text-fg">
            <Megaphone size={14} className="text-warn" /> 插话（Steer）
            <span className="text-[11px] font-normal text-muted">注入当前 run 的下一个决策边界</span>
          </div>
          <div className="flex gap-2">
            <input
              value={steerInput}
              onChange={(e) => setSteerInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && void doSteer()}
              placeholder="补充约束 / 纠偏 / 新信息…"
              className="flex-1 rounded-lg border border-border bg-transparent px-3 py-2 text-xs text-fg outline-none placeholder:text-muted focus:border-warn"
            />
            <Btn onClick={() => void doSteer()}>注入</Btn>
          </div>
        </div>
      )}
      <div className="flex items-center gap-2 text-xs text-muted">
        <Link to="/tasks" className="hover:text-accent">
          任务看板
        </Link>
        <span>/</span>
        <span className="font-mono">#{t.id}</span>
      </div>

      {/* 任务信息卡 */}
      <Panel
        title={
          <span className="flex items-center gap-2">
            <span className="font-mono text-muted">#{t.id}</span>
            {t.title}
          </span>
        }
        right={<StatusBadge status={t.status} />}
      >
        <div className="grid grid-cols-2 gap-x-6 gap-y-1.5 text-xs md:grid-cols-4">
          <Field label="Agent">
            <Link to={`/agents/${t.agent_id}`} className="hover:text-accent">
              {t.agent_name || `#${t.agent_id}`}
            </Link>
          </Field>
          <Field label="优先级">
            <span className="font-mono">P{t.priority}</span>
          </Field>
          <Field label="创建时间">{fmtTime(t.created_at)}</Field>
          <Field label="开始时间">{fmtTime(t.started_at)}</Field>
          <Field label="结束时间">{fmtTime(t.finished_at)}</Field>
          {(t.blocked_by?.length ?? 0) > 0 && (
            <Field label="阻塞于">
              <span className="text-accent">#{t.blocked_by.join(', #')}</span>
            </Field>
          )}
        </div>
        <div className="mt-3">
          <div className="mb-1 text-[11px] text-muted">Prompt</div>
          <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded border border-border bg-bg p-2 font-mono text-xs text-muted">
            {t.prompt || '—'}
          </pre>
        </div>
        {t.result !== null && t.result !== undefined && (
          <div className="mt-3">
            <div className="mb-1 text-[11px] text-ok/80">Result</div>
            <JsonBlock value={t.result} />
          </div>
        )}
        {t.error && (
          <div className="mt-3">
            <div className="mb-1 text-[11px] text-rose-400/80">Error</div>
            <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 font-mono text-xs text-bad">
              {t.error}
            </pre>
          </div>
        )}
        {(t.deps?.length ?? 0) > 0 && (
          <div className="mt-3">
            <div className="mb-1 text-[11px] text-muted">依赖任务</div>
            <div className="flex flex-wrap gap-2">
              {t.deps.map((d) => (
                <Link
                  key={d.id}
                  to={`/tasks/${d.id}`}
                  className="flex items-center gap-1.5 rounded border border-border bg-panel2 px-2 py-1 text-xs hover:border-border"
                >
                  <span className="font-mono text-muted">#{d.id}</span>
                  <span className="max-w-40 truncate">{d.title}</span>
                  <StatusBadge status={d.status} />
                </Link>
              ))}
            </div>
          </div>
        )}
      </Panel>

      {/* Runs 列表 */}
      <Panel title={`执行记录（Runs ${runs.data?.length ?? 0}）`} bodyClass="p-0">
        {runs.error && <div className="p-4"><ErrorText>{runs.error}</ErrorText></div>}
        {runs.loading && (
          <div className="p-4">
            <SkeletonBlock rows={2} />
          </div>
        )}
        {!runs.loading && !runs.error && (runs.data?.length ?? 0) === 0 && (
          <div className="px-4 py-6 text-center text-xs text-muted">尚无执行记录</div>
        )}
        {(runs.data ?? []).map((r) => (
          <RunRow key={r.id} r={r} open={openRunId === r.id} onToggle={() => void viewTrace(r.id)} />
        ))}
        {openRunId !== null && (
          <div className="border-t border-border p-4">
            {traceLoading && <SkeletonBlock rows={4} />}
            {traceErr && <ErrorText>{traceErr}</ErrorText>}
            {!traceLoading && !traceErr && trace && <TraceView trace={trace} />}
          </div>
        )}
      </Panel>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <span className="text-muted">{label}</span>
      <span className="ml-2 text-fg">{children}</span>
    </div>
  );
}

function RunRow({ r, open, onToggle }: { r: RunSummary; open: boolean; onToggle: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3 border-b border-border/60 px-4 py-2 text-xs last:border-b-0">
      <span className="font-mono text-muted">run #{r.id}</span>
      <StatusBadge status={r.status} />
      <span className="text-muted">
        tokens <span className="font-mono text-fg">{fmtNum(r.total_tokens)}</span>
      </span>
      <span className="text-muted">
        cost <span className="font-mono text-fg">${r.cost_usd?.toFixed(4) ?? '—'}</span>
      </span>
      <span className="text-muted">
        {fmtTime(r.started_at)} → {fmtTime(r.finished_at)}
      </span>
      <Btn variant={open ? 'default' : 'primary'} onClick={onToggle}>
        {open ? '收起 Trace' : '查看 Trace'}
      </Btn>
      <a
        href={`/api/v1/runs/${r.id}/export.md`}
        className="rounded border border-border bg-panel2 px-2.5 py-1 text-xs font-medium text-fg transition-colors hover:border-border"
      >
        导出报告
      </a>
    </div>
  );
}

// ---------- Trace：步骤瀑布图 ----------
export function TraceView({ trace }: { trace: RunTrace }) {
  const steps = trace.steps ?? [];
  const maxLatency = Math.max(1, ...steps.map((s) => s.latency_ms ?? 0));
  const totalLatency = steps.reduce((s, x) => s + (x.latency_ms ?? 0), 0);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());

  const toggle = (seq: number) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(seq)) next.delete(seq);
      else next.add(seq);
      return next;
    });

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-3 text-xs text-muted">
        <span className="font-mono text-fg">run #{trace.id}</span>
        <StatusBadge status={trace.status} />
        <span>
          tokens <span className="font-mono text-fg">{fmtNum(trace.total_tokens)}</span>
        </span>
        <span>
          cost <span className="font-mono text-fg">${trace.cost_usd?.toFixed(4) ?? '—'}</span>
        </span>
        <span>
          总耗时 <span className="font-mono text-fg">{fmtDuration(totalLatency)}</span>
        </span>
        <span className="ml-auto text-[11px]">点击步骤行展开 input / output</span>
      </div>

      {steps.length === 0 && <div className="py-4 text-center text-xs text-muted">无步骤数据</div>}

      <div className="space-y-1">
        {steps.map((s) => (
          <StepRow
            key={s.seq}
            step={s}
            maxLatency={maxLatency}
            expanded={expanded.has(s.seq)}
            onToggle={() => toggle(s.seq)}
          />
        ))}
      </div>
    </div>
  );
}

function StepRow({
  step,
  maxLatency,
  expanded,
  onToggle,
}: {
  step: RunStep;
  maxLatency: number;
  expanded: boolean;
  onToggle: () => void;
}) {
  const pct = step.latency_ms > 0 ? Math.max(1.5, (step.latency_ms / maxLatency) * 100) : 0.5;
  const hasPayload = step.input !== undefined || step.output !== undefined;
  return (
    <div className="rounded border border-border/60 bg-panel2/50">
      <button
        type="button"
        onClick={onToggle}
        disabled={!hasPayload}
        className={`flex w-full items-center gap-3 px-3 py-2 text-left text-xs ${
          hasPayload ? 'cursor-pointer hover:bg-panel2' : 'cursor-default'
        }`}
      >
        <span className="w-6 shrink-0 font-mono text-[10px] text-muted">{step.seq}</span>
        <KindBadge kind={step.kind} />
        <span className="w-44 shrink-0 truncate font-medium text-fg" title={step.name}>
          {step.name}
        </span>
        {/* 瀑布条：宽度按 latency_ms 占比 */}
        <span className="min-w-0 flex-1">
          <span className="block h-3 rounded-sm bg-bg/80">
            <span className="block h-3 rounded-sm transition-all" style={{ width: `${pct}%` }}>
              <KindBadge2 kind={step.kind} />
            </span>
          </span>
        </span>
        <span className="w-16 shrink-0 text-right font-mono tabular-nums text-muted">{fmtDuration(step.latency_ms)}</span>
        <span className="w-20 shrink-0 text-right font-mono tabular-nums text-muted">
          {step.tokens !== undefined && step.tokens !== null ? `${fmtNum(step.tokens)} tk` : '—'}
        </span>
        <span className="w-3 shrink-0 text-center text-muted">{hasPayload ? (expanded ? '−' : '+') : ''}</span>
      </button>
      {expanded && (
        <div className="grid gap-3 border-t border-border/60 p-3 lg:grid-cols-2">
          <JsonBlock label="Input" value={step.input} />
          <div className="space-y-3">
            <JsonBlock label="Output" value={step.output} />
            {step.note && (
              <div>
                <div className="mb-1 text-[11px] text-muted">Note</div>
                <div className="rounded border border-border bg-bg p-2 text-xs text-muted">{step.note}</div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

/** 瀑布条内部填充色 */
function KindBadge2({ kind }: { kind: string }) {
  const color =
    kind === 'llm'
      ? 'bg-violet-500/70'
      : kind === 'tool'
        ? 'bg-info/70'
        : kind === 'handoff'
          ? 'bg-orange-500/70'
          : kind === 'gate'
            ? 'bg-yellow-500/70'
            : 'bg-slate-500/70';
  return <span className={`block h-3 w-full rounded-sm ${color}`} />;
}

// TaskArtifacts 任务产物面板（v1.4）：报告/补丁/执行记录，点击查看内容。
function TaskArtifacts({ taskId }: { taskId: number }) {
  const list = useApi(() => api.listArtifacts(taskId), [taskId]);
  const items = list.data?.items ?? [];
  const [openId, setOpenId] = useState<number | null>(null);
  const detail = useApi(() => api.getArtifact(openId!), [openId], openId !== null);

  if (items.length === 0) return null;
  return (
    <Panel title={`产物 Artifacts（${items.length}）`} bodyClass="p-0">
      {items.map((a) => (
        <div key={a.id} className="border-b border-border/60 px-4 py-2 text-xs last:border-b-0">
          <button
            type="button"
            onClick={() => setOpenId(openId === a.id ? null : a.id)}
            className="flex w-full flex-wrap items-center gap-3 text-left"
          >
            <span className="rounded border border-info/30 bg-info/10 px-1.5 py-0.5 text-[11px] text-info">{a.kind}</span>
            <span className="font-mono text-fg">{a.name}</span>
            <span className="text-muted">{a.size}B</span>
            <span className="ml-auto font-mono text-[10px] text-muted">{fmtTime(a.created_at)}</span>
          </button>
          {openId === a.id && (
            <pre className="mt-2 max-h-64 overflow-auto rounded border border-border bg-bg p-2 font-mono text-[11px] leading-relaxed text-muted">
              {detail.data?.content ?? '加载中…'}
            </pre>
          )}
        </div>
      ))}
    </Panel>
  );
}

// CommentThread 任务评论线程（Multica 对标）：人评论 / agent 回帖（@ 唤醒）。
function CommentThread({
  comments, input, setInput, onPost, posting,
}: {
  comments: TaskComment[];
  input: string;
  setInput: (v: string) => void;
  onPost: () => void;
  posting: boolean;
}) {
  return (
    <div className="rounded-xl bg-panel px-5 py-4">
      <div className="mb-3 flex items-center gap-2 text-[13px] font-semibold text-fg">
        <MessageSquare size={14} className="text-accent" /> 评论
        <span className="text-[11px] font-normal text-muted">@成员 可唤醒其以任务上下文回帖</span>
      </div>
      <div className="max-h-72 space-y-2.5 overflow-y-auto">
        {comments.length === 0 && (
          <div className="py-4 text-center text-xs text-muted">暂无评论</div>
        )}
        {comments.map((c) => (
          <div key={c.id} className="flex gap-2.5">
            <div className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold ${
              c.author_type === 'agent' ? 'bg-accent/20 text-accent' : 'bg-white/8 text-muted'
            }`}>
              {c.author_type === 'agent' ? c.author_name.slice(0, 1) : '你'}
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline gap-2">
                <span className="text-[11px] font-medium text-fg">
                  {c.author_type === 'agent' ? `@${c.author_name}` : '你'}
                </span>
                <span className="font-mono text-[10px] text-muted">{fmtTime(c.created_at)}</span>
              </div>
              <div className="mt-0.5 whitespace-pre-wrap break-words text-xs leading-relaxed text-fg/90">
                {c.content}
              </div>
            </div>
          </div>
        ))}
      </div>
      <div className="mt-3 flex gap-2">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && onPost()}
          placeholder="@fixer 这个方案怎么看？"
          className="flex-1 rounded-lg border border-border bg-transparent px-3 py-2 text-xs text-fg outline-none placeholder:text-muted focus:border-accent"
          disabled={posting}
        />
        <Btn variant="primary" disabled={posting || !input.trim()} onClick={onPost}>
          <Send size={12} /> 发送
        </Btn>
      </div>
    </div>
  );
}
