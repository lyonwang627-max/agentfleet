import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useApi, useHealth } from '../hooks';
import type { AgentSummary, FleetEvent } from '../types';
import { Btn, ErrorText, KindBadge, Panel, SkeletonBlock, fmtTime } from '../ui';

export default function Overview() {
  const agents = useApi(() => api.listAgents(), []);
  const health = useHealth();
  const [busyId, setBusyId] = useState<number | null>(null);
  const down = agents.error !== null;

  const toggle = async (a: AgentSummary) => {
    setBusyId(a.id);
    try {
      await api.setAgentEnabled(a.id, !a.enabled);
      await agents.reload();
      await health.reload();
    } finally {
      setBusyId(null);
    }
  };

  // 克隆派生变体（I3-b）：同配置新 agent，名称自动去重
  const clone = async (a: AgentSummary) => {
    setBusyId(a.id);
    try {
      await api.cloneAgent(a.id);
      await agents.reload();
    } finally {
      setBusyId(null);
    }
  };

  const items = agents.data?.items ?? [];
  const runningTotal = items.reduce((s, a) => s + (a.running ?? 0), 0);
  const queuedTotal = items.reduce((s, a) => s + (a.queued ?? 0), 0);
  const enabledCount = items.filter((a) => a.enabled).length;

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
      {/* 左：健康条 + agent 卡片墙 */}
      <div className="min-w-0 space-y-4">
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <MiniStat label="Agent 总数" value={items.length} sub={`${enabledCount} 启用`} />
          <MiniStat label="运行中" value={runningTotal} sub="并发执行" accent />
          <MiniStat label="排队" value={queuedTotal} sub="等待调度" />
          <MiniStat
            label="LLM 模式"
            value={health.data?.llm.mode ?? (health.error ? '—' : '…')}
            sub={health.data?.llm.model ?? undefined}
          />
        </div>

        {down && agents.error && <ErrorText>{agents.error}</ErrorText>}
        {agents.loading && <SkeletonBlock rows={4} />}
        {!down && !agents.loading && items.length === 0 && (
          <Panel>
            <div className="text-sm text-muted">暂无 Agent，先通过 API 创建（POST /agents）。</div>
          </Panel>
        )}

        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
          {items.map((a) => (
            <div key={a.id} className="rounded-lg border border-border bg-panel p-4 transition-colors hover:border-border/50">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <Link to={`/agents/${a.id}`} className="truncate text-sm font-semibold text-fg hover:text-[#f0a830]">
                    {a.name}
                  </Link>
                  <div className="truncate text-xs text-muted">{a.role || '—'}</div>
                </div>
                <span
                  className={`inline-flex shrink-0 items-center rounded border px-1.5 py-0.5 text-[11px] ${
                    a.enabled
                      ? 'border-ok/40 bg-ok/10 text-ok'
                      : 'border-border bg-muted/10 text-muted'
                  }`}
                >
                  {a.enabled ? '已启用' : '已停用'}
                </span>
              </div>
              <div className="mt-3 flex items-center gap-3 text-[11px] text-muted">
                <span className="font-mono">{a.model}</span>
                <span className="rounded border border-border px-1 py-px font-mono">{a.runner}</span>
                <span>并发 {a.max_concurrency}</span>
              </div>
              <div className="mt-3 flex items-end justify-between">
                <div className="flex gap-4 text-xs">
                  <span className="text-[#f0a830]">
                    运行 <span className="font-mono text-sm tabular-nums">{a.running ?? 0}</span>
                  </span>
                  <span className="text-[#f0a830]">
                    排队 <span className="font-mono text-sm tabular-nums">{a.queued ?? 0}</span>
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Btn disabled={busyId === a.id || down} onClick={() => void clone(a)}>
                    克隆
                  </Btn>
                  <Btn
                    variant={a.enabled ? 'danger' : 'primary'}
                    disabled={busyId === a.id || down}
                    onClick={() => void toggle(a)}
                  >
                    {busyId === a.id ? '处理中…' : a.enabled ? '停用' : '启用'}
                  </Btn>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* 右：实时事件流 */}
      <EventFeed />
    </div>
  );
}

function MiniStat({ label, value, sub, accent }: { label: string; value: string | number; sub?: string; accent?: boolean }) {
  return (
    <div className="rounded-lg border border-border bg-panel px-4 py-3">
      <div className="text-xs text-muted">{label}</div>
      <div className={`mt-0.5 text-xl font-semibold tabular-nums ${accent ? 'text-[#f0a830]' : 'text-fg'}`}>{value}</div>
      {sub && <div className="text-[11px] text-muted">{sub}</div>}
    </div>
  );
}

const MAX_EVENTS = 50;

function EventFeed() {
  const [events, setEvents] = useState<{ ev: FleetEvent; at: number }[]>([]);
  const [connected, setConnected] = useState(false);
  const [kindFilter, setKindFilter] = useState<string>('all');

  // 已出现的 kind（动态生成筛选项）
  const kinds = Array.from(new Set(events.map((e) => e.ev.kind))).slice(0, 8);

  useEffect(() => {
    const es = new EventSource('/api/v1/events');
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (e: MessageEvent<string>) => {
      let ev: FleetEvent;
      try {
        ev = JSON.parse(e.data) as FleetEvent;
      } catch {
        ev = { kind: 'raw', data: { text: e.data } };
      }
      setEvents((prev) => [{ ev, at: Date.now() }, ...prev].slice(0, MAX_EVENTS));
    };
    return () => es.close();
  }, []);

  const shown = kindFilter === 'all' ? events : events.filter((e) => e.ev.kind === kindFilter);

  return (
    <Panel
      title={
        <span className="flex items-center gap-2">
          实时事件流
          <span className={`inline-flex h-1.5 w-1.5 rounded-full ${connected ? 'bg-ok' : 'bg-muted'}`} />
          <span className="text-[11px] font-normal text-muted">SSE {connected ? '已连接' : '未连接'}</span>
        </span>
      }
      right={
        <span className="flex items-center gap-1.5">
          <select
            value={kindFilter}
            onChange={(e) => setKindFilter(e.target.value)}
            className="rounded border border-border bg-bg px-1.5 py-0.5 text-[11px] text-fg outline-none focus:border-accent-dim"
          >
            <option value="all">全部</option>
            {kinds.map((k) => (
              <option key={k} value={k}>{k}</option>
            ))}
          </select>
          <span className="text-[11px] text-muted">{shown.length}/{MAX_EVENTS}</span>
        </span>
      }
      bodyClass="p-0"
      className="self-start"
    >
      <div className="max-h-[calc(100vh-160px)] overflow-y-auto">
        {shown.length === 0 && (
          <div className="px-4 py-8 text-center text-xs text-muted">
            {kindFilter === 'all' ? '等待事件…（任务 / Run 状态变化会推送到这里）' : `暂无 ${kindFilter} 事件`}
          </div>
        )}
        {shown.map((e, i) => (
          <div key={`${e.at}-${i}`} className="border-b border-border/60 px-3 py-2 last:border-b-0">
            <div className="flex items-center gap-2">
              <KindBadge kind={e.ev.kind} />
              <span className="ml-auto font-mono text-[10px] text-muted">{fmtTime(new Date(e.at).toISOString())}</span>
            </div>
            <div className="mt-1 line-clamp-2 break-all font-mono text-[11px] leading-relaxed text-muted">
              {e.ev.data ? JSON.stringify(e.ev.data) : '—'}
            </div>
          </div>
        ))}
      </div>
    </Panel>
  );
}
