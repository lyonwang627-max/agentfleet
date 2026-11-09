import { NavLink, Outlet } from 'react-router-dom';
import { useHealth } from '../hooks';
import { fmtDuration } from '../ui';

const NAV = [
  { to: '/', label: '舰队总览' },
  { to: '/tasks', label: '任务看板' },
  { to: '/automations', label: '自动化规则' },
  { to: '/studio', label: '工作台' },
  { to: '/chat', label: '试聊台' },
  { to: '/replays', label: '会话回放' },
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
      <aside className="glass flex w-52 shrink-0 flex-col border-r border-white/6">
        <div className="px-5 pb-2 pt-6">
          <div className="text-[15px] font-semibold tracking-tight text-fg">
            agent<span className="text-accent">fleet</span>
          </div>
          <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted">
            <i className={`signal ${down ? 'signal-bad' : 'signal-ok'}`} aria-hidden />
            任务控制台
          </div>
        </div>
        <nav className="flex flex-col gap-0.5 px-3 pt-2">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.to === '/'}
              className={({ isActive }) =>
                `rounded-lg px-3 py-[7px] text-[13px] transition-colors ${
                  isActive ? 'bg-accent font-medium text-white' : 'text-muted hover:bg-white/5 hover:text-fg'
                }`
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto px-5 pb-4 text-[11px] text-muted">
          v1 · localhost
        </div>
      </aside>

      {/* 主区 */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* 顶栏健康状态 */}
        <header className="glass flex items-center gap-3 border-b border-white/6 px-6 py-3">
          <span
            className={`signal ${down ? 'signal-bad' : 'signal-ok'}`}
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
                      : 'border-accent-dim/50 bg-accent/10 text-accent'
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
          <div className="border-b border-accent-dim/50 bg-accent/10 px-5 py-2 text-xs text-accent">
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
