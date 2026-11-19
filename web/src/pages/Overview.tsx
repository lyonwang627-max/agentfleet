import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useApi, useHealth } from '../hooks';
import type { AgentSummary, FleetEvent } from '../types';
import { Btn, ErrorText, Panel, SkeletonBlock, fmtTime } from '../ui';

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

        {/* 名册（去卡片化）：行条 + 信号灯列 + mono 数据列 */}
        <div className="divide-y divide-[var(--hairline)] rounded-lg border border-[var(--hairline)] bg-panel">
          <div className="flex items-center gap-3 px-4 py-1.5 text-[10px] lowercase tracking-wide text-muted">
            <span className="w-2">·</span>
            <span className="w-36">agent</span>
            <span className="w-24">model</span>
            <span className="w-16">runner</span>
            <span className="w-20 text-right">run / queue</span>
            <span className="ml-auto">controls</span>
          </div>
          {items.map((a) => (
            <div key={a.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 transition-colors hover:bg-[var(--row-hover-bg)]">
              {/* 信号灯列 */}
              <span
                className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                  !a.enabled ? 'bg-muted' : (a.running ?? 0) > 0 ? 'bg-ok animate-pulse' : 'bg-[var(--hairline)]'
                }`}
                aria-hidden
              />
              <Link to={`/agents/${a.id}`} className="w-36 truncate text-sm font-medium text-fg hover:text-accent">
                {a.name}
              </Link>
              <span className="w-24 truncate font-mono text-[11px] text-muted">{a.model}</span>
              <span className="w-16 font-mono text-[11px] text-muted">{a.runner}</span>
              <span className="w-20 text-right font-mono text-xs tabular-nums">
                <span className="text-ok">{a.running ?? 0}</span>
                <span className="text-muted"> / </span>
                <span className="text-muted">{a.queued ?? 0}</span>
              </span>
              <span className="ml-auto flex items-center gap-2">
                {!a.enabled && (
                  <span className="rounded border border-border px-1.5 py-0.5 text-[10px] text-muted">已停用</span>
                )}
                <Btn disabled={busyId === a.id || down} onClick={() => void clone(a)}>
                  克隆
                </Btn>
                <Btn
                  variant={a.enabled ? 'danger' : 'primary'}
                  disabled={busyId === a.id || down}
                  onClick={() => void toggle(a)}
                >
                  {busyId === a.id ? '…' : a.enabled ? '停用' : '启用'}
                </Btn>
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* 右：实时事件流 */}
      <EventFeed />
    </div>
  );
}

// 读数条（去卡片化）：mono 大数字 + 小写标签 + 底 hairline——无框无底色
function MiniStat({ label, value, sub, accent }: { label: string; value: string | number; sub?: string; accent?: boolean }) {
  return (
    <div className="border-b border-[var(--hairline)] pb-2.5">
      <div className="text-[11px] lowercase tracking-wide text-muted">{label}</div>
      <div className={`mt-1 font-mono text-2xl font-medium tabular-nums ${accent ? 'text-ok' : 'text-fg'}`}>{value}</div>
      {sub && <div className="mt-0.5 text-[10px] text-muted">{sub}</div>}
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
          <span className="font-mono text-xs lowercase tracking-wide">event stream</span>
          <span className={`inline-flex h-1.5 w-1.5 rounded-full ${connected ? 'bg-ok animate-pulse' : 'bg-muted'}`} />
          <span className="font-mono text-[10px] text-muted">{connected ? 'LIVE' : 'OFFLINE'}</span>
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
          <div key={`${e.at}-${i}`} className="border-b border-[var(--hairline)] px-3 py-1.5 last:border-b-0">
            <div className="flex items-center gap-2 font-mono text-[10px]">
              <span className="text-muted">{String(MAX_EVENTS - i).padStart(2, '0')}</span>
              <span className={e.ev.kind.startsWith('task') ? 'text-ok' : e.ev.kind.includes('step') ? 'text-info' : 'text-warn'}>
                {e.ev.kind}
              </span>
              <span className="ml-auto text-muted">{fmtTime(new Date(e.at).toISOString())}</span>
            </div>
            <div className="mt-0.5 line-clamp-2 break-all font-mono text-[10px] leading-relaxed text-muted">
              {e.ev.data ? JSON.stringify(e.ev.data) : '—'}
            </div>
          </div>
        ))}
      </div>
    </Panel>
  );
}
