import type { ReactNode } from 'react';

// ---------- 骨架屏 ----------
export function Skeleton({ className = 'h-4' }: { className?: string }) {
  return <div className={`animate-pulse rounded bg-panel2 ${className}`} />;
}

export function SkeletonBlock({ rows = 3 }: { rows?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-12" />
      ))}
    </div>
  );
}

// ---------- 面板 ----------
export function Panel({
  title,
  right,
  children,
  className = '',
  bodyClass = '',
}: {
  title?: ReactNode;
  right?: ReactNode;
  children: ReactNode;
  className?: string;
  bodyClass?: string;
}) {
  return (
    <div className={`card-sheen card-lift rounded-lg border border-border bg-panel ${className}`}>
      {(title || right) && (
        <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
          <div className="text-[13px] font-semibold tracking-wide text-fg">{title}</div>
          {right}
        </div>
      )}
      <div className={`p-4 ${bodyClass}`}>{children}</div>
    </div>
  );
}

// ---------- 徽章 ----------
const STATUS_STYLE: Record<string, string> = {
  queued: 'bg-slate-500/15 text-slate-300 border-slate-500/30',
  running: 'bg-sky-500/15 text-sky-300 border-sky-500/30',
  review: 'bg-amber-500/15 text-amber-300 border-amber-500/30',
  done: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30',
  failed: 'bg-rose-500/15 text-rose-300 border-rose-500/30',
  canceled: 'bg-zinc-500/15 text-zinc-400 border-zinc-500/30',
  ok: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30',
  error: 'bg-rose-500/15 text-rose-300 border-rose-500/30',
  pending: 'bg-slate-500/15 text-slate-300 border-slate-500/30',
};
const STATUS_LABEL: Record<string, string> = {
  queued: '排队中',
  running: '运行中',
  review: '待审核',
  done: '已完成',
  failed: '失败',
  canceled: '已取消',
};

export function StatusBadge({ status }: { status: string }) {
  const style = STATUS_STYLE[status] ?? 'bg-slate-500/15 text-slate-300 border-slate-500/30';
  return (
    <span className={`inline-flex shrink-0 items-center gap-1.5 rounded border px-1.5 py-0.5 text-[11px] font-medium ${style}`}>
      {status === 'running' && <i className="pulse-dot h-1.5 w-1.5 rounded-full bg-sky-400" aria-hidden />}
      {status === 'review' && <i className="h-1.5 w-1.5 rounded-full bg-amber-400" aria-hidden />}
      {STATUS_LABEL[status] ?? status}
    </span>
  );
}

// step 类型徽章：llm 紫 / tool 蓝 / handoff 橙 / gate 黄
export const KIND_STYLE: Record<string, string> = {
  llm: 'bg-violet-500/15 text-violet-300 border-violet-500/40',
  tool: 'bg-blue-500/15 text-blue-300 border-blue-500/40',
  handoff: 'bg-orange-500/15 text-orange-300 border-orange-500/40',
  gate: 'bg-yellow-500/15 text-yellow-300 border-yellow-500/40',
  done: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/40',
  error: 'bg-rose-500/15 text-rose-300 border-rose-500/40',
};
export const KIND_BG: Record<string, string> = {
  llm: 'bg-violet-500',
  tool: 'bg-blue-500',
  handoff: 'bg-orange-500',
  gate: 'bg-yellow-500',
};
const KIND_LABEL: Record<string, string> = {
  llm: 'LLM',
  tool: '工具',
  handoff: '交接',
  gate: '门禁',
};

export function KindBadge({ kind }: { kind: string }) {
  const style = KIND_STYLE[kind] ?? 'bg-slate-500/15 text-slate-300 border-slate-500/40';
  return (
    <span className={`inline-flex w-14 shrink-0 items-center justify-center rounded border px-1 py-0.5 font-mono text-[11px] ${style}`}>
      {KIND_LABEL[kind] ?? kind}
    </span>
  );
}

export function PriorityBadge({ p }: { p: number }) {
  const style =
    p >= 7
      ? 'bg-rose-500/15 text-rose-300 border-rose-500/30'
      : p >= 4
        ? 'bg-amber-500/15 text-amber-300 border-amber-500/30'
        : 'bg-slate-500/15 text-slate-300 border-slate-500/30';
  return (
    <span className={`inline-flex items-center rounded border px-1.5 py-0.5 font-mono text-[11px] ${style}`}>
      P{p}
    </span>
  );
}

// ---------- 统计卡 ----------
export function StatCard({ label, value, hint, accent }: { label: string; value: ReactNode; hint?: string; accent?: boolean }) {
  return (
    <div className="card-sheen card-lift rounded-lg border border-border bg-panel px-4 py-3.5">
      <div className="text-[11px] font-medium uppercase tracking-wider text-muted">{label}</div>
      <div className={`mt-1.5 text-2xl font-semibold tabular-nums ${accent ? 'brand-text' : 'text-fg'}`}>{value}</div>
      {hint && <div className="mt-0.5 text-[11px] text-muted">{hint}</div>}
    </div>
  );
}

// ---------- JSON 展示（超长折叠） ----------
export function JsonBlock({ value, label, maxShow = 1400 }: { value: unknown; label?: string; maxShow?: number }) {
  const text = value === undefined || value === null ? '—' : safeJson(value);
  const long = text.length > maxShow;
  return (
    <div>
      {label && <div className="mb-1 text-[11px] font-medium text-muted">{label}</div>}
      <pre
        className={`overflow-auto rounded border border-border bg-bg p-2 font-mono text-xs leading-relaxed text-slate-300 ${
          long ? 'max-h-40' : 'max-h-96'
        }`}
      >
        {text}
      </pre>
      {long && <div className="mt-1 text-[11px] text-muted">内容过长已折叠（{text.length} 字符）</div>}
    </div>
  );
}

function safeJson(v: unknown): string {
  try {
    return typeof v === 'string' ? v : JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

// ---------- 格式化 ----------
export function fmtTime(s?: string | null): string {
  if (!s) return '—';
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

export function fmtNum(n?: number | null): string {
  if (n === undefined || n === null) return '—';
  if (Math.abs(n) >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (Math.abs(n) >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/** 成功率/通过率：兼容 0-1 与 0-100 两种口径 */
export function fmtRate(v?: number | null): string {
  if (v === undefined || v === null) return '—';
  const pct = v > 0 && v <= 1 ? v * 100 : v;
  return `${pct.toFixed(1)}%`;
}

export function fmtDuration(ms?: number | null): string {
  if (ms === undefined || ms === null) return '—';
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

// ---------- 按钮 ----------
export function Btn({
  children,
  onClick,
  variant = 'default',
  disabled,
  className = '',
  type = 'button',
}: {
  children: ReactNode;
  onClick?: () => void;
  variant?: 'default' | 'primary' | 'danger' | 'ghost';
  disabled?: boolean;
  className?: string;
  type?: 'button' | 'submit';
}) {
  const styles: Record<string, string> = {
    default: 'border-border bg-panel2 text-fg hover:border-border-hover hover:bg-panel',
    primary:
      'border-sky-500/60 bg-gradient-to-b from-sky-500/25 to-sky-600/15 text-sky-200 hover:from-sky-500/35 hover:to-sky-600/25 hover:border-sky-400/70',
    danger: 'border-rose-600/50 bg-rose-600/15 text-rose-300 hover:bg-rose-600/25',
    ghost: 'border-transparent bg-transparent text-muted hover:text-fg',
  };
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex items-center gap-1 rounded border px-2.5 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${styles[variant]} ${className}`}
    >
      {children}
    </button>
  );
}

export function ErrorText({ children }: { children: ReactNode }) {
  return (
    <div className="rounded border border-rose-600/40 bg-rose-600/10 px-3 py-2 text-xs text-rose-300">{children}</div>
  );
}
