import type { ReactNode } from 'react';

/* ============================================================
   全局组件 v2 —— mission control
   - Panel：只包表单与统计；数据列表用轻量 Section（无卡片壳）
   - 信号灯徽章替代色块徽章
   - 按压 0.96、行 hover 120ms（better-ui 精确值）
   ============================================================ */

// ---------- 骨架屏 ----------
export function Skeleton({ className = 'h-4' }: { className?: string }) {
  return <div className={`animate-pulse rounded bg-panel2 ${className}`} />;
}

export function SkeletonBlock({ rows = 3 }: { rows: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-9" />
      ))}
    </div>
  );
}

// ---------- 分区（替代数据列表的卡片壳：仅标题 + 内容，无边框包覆） ----------
export function Section({
  title,
  right,
  children,
  className = '',
}: {
  title?: ReactNode;
  right?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`min-w-0 ${className}`}>
      {(title || right) && (
        <header className="mb-2.5 flex h-7 items-center justify-between px-0.5">
          <h2 className="text-[13px] font-semibold text-fg">{title}</h2>
          {right}
        </header>
      )}
      {children}
    </section>
  );
}

// ---------- 面板（保留给表单/统计等真正需要分组的场景；阴影层次替代边框） ----------
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
    <div className={`rounded-xl bg-panel ${className}`}>
      {(title || right) && (
        <div className="flex h-11 items-center justify-between px-5 text-fg">
          <div className="text-[13px] font-semibold">{title}</div>
          {right}
        </div>
      )}
      <div className={`px-5 pb-5 ${bodyClass || 'pt-0'}`}>{children}</div>
    </div>
  );
}

// ---------- 信号灯（全站状态可视的锚点） ----------
export function Signal({ state }: { state: 'run' | 'ok' | 'bad' | 'idle' }) {
  return <i className={`signal signal-${state}`} aria-hidden />;
}

// ---------- 状态徽章：信号灯 + 文字（色不再是唯一通道） ----------
const STATUS_SIGNAL: Record<string, 'run' | 'ok' | 'bad' | 'idle'> = {
  queued: 'idle', running: 'run', review: 'run', done: 'ok',
  failed: 'bad', canceled: 'idle', ok: 'ok', error: 'bad', pending: 'idle',
};
const STATUS_LABEL: Record<string, string> = {
  queued: '排队', running: '运行', review: '待审', done: '完成',
  failed: '失败', canceled: '取消',
};

export function StatusBadge({ status }: { status: string }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5 text-[11px] text-muted">
      <Signal state={STATUS_SIGNAL[status] ?? 'idle'} />
      {STATUS_LABEL[status] ?? status}
    </span>
  );
}

// step 类型徽章：保持轻量文字标签
export const KIND_STYLE: Record<string, string> = {
  llm: 'text-violet-300',
  tool: 'text-info',
  handoff: 'text-accent',
  gate: 'text-[#e3d05a]',
  done: 'text-ok',
  error: 'text-bad',
};
export const KIND_BG: Record<string, string> = {
  llm: 'bg-violet-400', tool: 'bg-info', handoff: 'bg-[#f0a830]', gate: 'bg-[#e3d05a]',
};
const KIND_LABEL: Record<string, string> = {
  llm: 'LLM', tool: '工具', handoff: '交接', gate: '门禁',
};

export function KindBadge({ kind }: { kind: string }) {
  return (
    <span className={`shrink-0 font-mono text-[11px] ${KIND_STYLE[kind] ?? 'text-muted'}`}>
      {KIND_LABEL[kind] ?? kind}
    </span>
  );
}

export function PriorityBadge({ p }: { p: number }) {
  const tone = p >= 7 ? 'text-bad' : p >= 4 ? 'text-accent' : 'text-muted';
  return <span className={`shrink-0 font-mono text-[11px] tabular-nums ${tone}`}>P{p}</span>;
}

// ---------- 统计卡（保留卡片：统计确实需要分组；去渐变） ----------
export function StatCard({ label, value, hint, accent }: { label: string; value: ReactNode; hint?: string; accent?: boolean }) {
  return (
    <div className="rounded-xl bg-panel px-4 py-4">
      <div className="text-[11px] text-muted">{label}</div>
      <div className={`mt-1.5 font-mono text-2xl font-medium tracking-tight tabular-nums ${accent ? 'text-accent' : 'text-fg'}`}>{value}</div>
      {hint && <div className="mt-0.5 text-[11px] text-muted">{hint}</div>}
    </div>
  );
}

// ---------- JSON 展示 ----------
export function JsonBlock({ value, label, maxShow = 1400 }: { value: unknown; label?: string; maxShow?: number }) {
  const text = value === undefined || value === null ? '—' : safeJson(value);
  const long = text.length > maxShow;
  return (
    <div>
      {label && <div className="mb-1 text-[11px] text-muted">{label}</div>}
      <pre
        className={`overflow-auto rounded-lg bg-bg/60 p-3 font-mono text-xs leading-relaxed text-muted ${
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

// ---------- 按钮（按压反馈 + 精确缓动） ----------
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
    default: 'border-border bg-transparent text-fg hover:bg-panel2',
    primary: 'border-accent-dim bg-accent/15 text-accent-dim hover:bg-accent/25 hover:border-accent',
    danger: 'border-bad/40 bg-bad/10 text-bad hover:bg-bad/20',
    ghost: 'border-transparent bg-transparent text-muted hover:text-fg',
  };
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      className={`press inline-flex items-center gap-1 rounded border px-2.5 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${styles[variant]} ${className}`}
    >
      {children}
    </button>
  );
}

export function ErrorText({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-lg bg-bad/12 px-3.5 py-2.5 text-xs text-bad">{children}</div>
  );
}
