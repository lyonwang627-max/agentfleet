import { useCallback, useState } from 'react';
import type { ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useApi, useEventSource, useThrottledReload } from '../hooks';
import type { RunStep, RunTrace, RunSummary } from '../types';
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

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 text-xs text-muted">
        <Link to="/tasks" className="hover:text-sky-300">
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
            <Link to={`/agents/${t.agent_id}`} className="hover:text-sky-300">
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
              <span className="text-amber-300">#{t.blocked_by.join(', #')}</span>
            </Field>
          )}
        </div>
        <div className="mt-3">
          <div className="mb-1 text-[11px] text-muted">Prompt</div>
          <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded border border-border bg-bg p-2 font-mono text-xs text-slate-300">
            {t.prompt || '—'}
          </pre>
        </div>
        {t.result !== null && t.result !== undefined && (
          <div className="mt-3">
            <div className="mb-1 text-[11px] text-emerald-400/80">Result</div>
            <JsonBlock value={t.result} />
          </div>
        )}
        {t.error && (
          <div className="mt-3">
            <div className="mb-1 text-[11px] text-rose-400/80">Error</div>
            <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded border border-rose-600/40 bg-rose-600/10 p-2 font-mono text-xs text-rose-300">
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
                  className="flex items-center gap-1.5 rounded border border-border bg-panel2 px-2 py-1 text-xs hover:border-slate-500"
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
      <Btn variant={open ? 'default' : 'primary'} className="ml-auto" onClick={onToggle}>
        {open ? '收起 Trace' : '查看 Trace'}
      </Btn>
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
                <div className="rounded border border-border bg-bg p-2 text-xs text-slate-300">{step.note}</div>
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
        ? 'bg-blue-500/70'
        : kind === 'handoff'
          ? 'bg-orange-500/70'
          : kind === 'gate'
            ? 'bg-yellow-500/70'
            : 'bg-slate-500/70';
  return <span className={`block h-3 w-full rounded-sm ${color}`} />;
}
