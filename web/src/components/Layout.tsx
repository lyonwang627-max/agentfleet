import { NavLink, Outlet } from 'react-router-dom';
import { useHealth } from '../hooks';
import { fmtDuration } from '../ui';

const NAV = [
  { to: '/', label: '舰队总览' },
  { to: '/tasks', label: '任务看板' },
  { to: '/automations', label: '自动化规则' },
  { to: '/chat', label: '试聊台' },
  { to: '/evals', label: '评测中心' },
  { to: '/usage', label: '用量看板' },
];

export default function Layout() {
  const health = useHealth();
  const down = health.error !== null;
  const h = health.data;

  return (
    <div className="flex h-full min-h-screen">
      {/* 侧边栏 */}
      <aside className="flex w-44 shrink-0 flex-col border-r border-border bg-panel/80 backdrop-blur">
        <div className="flex items-center gap-2.5 border-b border-border px-4 py-4">
          <span
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-gradient-to-br from-sky-400 to-indigo-600 text-[13px] font-bold text-white shadow-[0_0_14px_rgba(56,189,248,0.35)]"
            aria-hidden
          >
            ⚡
          </span>
          <div className="min-w-0">
            <div className="brand-text truncate text-sm font-bold tracking-wide">agentfleet</div>
            <div className="mt-0.5 text-[11px] text-muted">多 Agent 任务管控台</div>
          </div>
        </div>
        <nav className="flex flex-col gap-0.5 p-2">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.to === '/'}
              className={({ isActive }) =>
                `rounded px-3 py-2 text-sm transition-colors ${
                  isActive ? 'bg-sky-600/15 font-medium text-sky-300' : 'text-muted hover:bg-panel2 hover:text-fg'
                }`
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto p-3 text-[10px] leading-relaxed text-muted">
          API v1 · localhost:8090
          <br />
          Vite + React 18 + Tailwind v4
        </div>
      </aside>

      {/* 主区 */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* 顶栏健康状态 */}
        <header className="flex items-center gap-3 border-b border-border bg-panel/70 px-5 py-2.5 backdrop-blur">
          <span
            className={`inline-flex h-2 w-2 rounded-full ${down ? 'bg-rose-500' : 'pulse-dot bg-emerald-500'}`}
            aria-hidden
          />
          <span className="text-[13px] font-medium">{down ? '后端离线' : `后端 ${h?.status ?? '…'}`}</span>
          {h && (
            <span className="flex flex-wrap items-center gap-2 text-[11px] text-muted">
              <span>db {h.db ? '正常' : '异常'}</span>
              <span>·</span>
              <span>scheduler {h.scheduler ? '正常' : '异常'}</span>
              <span>·</span>
              <span>
                LLM{' '}
                <span
                  className={`rounded border px-1 py-px font-mono ${
                    h.llm.mode === 'ollama'
                      ? 'border-violet-500/40 bg-violet-500/10 text-violet-300'
                      : 'border-amber-500/40 bg-amber-500/10 text-amber-300'
                  }`}
                >
                  {h.llm.mode}
                </span>{' '}
                {h.llm.model}
              </span>
              <span>·</span>
              <span>运行 {fmtDuration(h.uptime_s * 1000)}</span>
            </span>
          )}
        </header>

        {/* 后端不可达横幅（优雅降级，不白屏） */}
        {down && (
          <div className="border-b border-amber-600/40 bg-amber-600/10 px-5 py-2 text-xs text-amber-300">
            后端未启动：<span className="font-mono">make run</span>（监听 http://localhost:8090 ）。页面已降级，显示骨架屏；后端恢复后自动重试。
          </div>
        )}

        <main className="min-h-0 flex-1 overflow-y-auto p-5">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
