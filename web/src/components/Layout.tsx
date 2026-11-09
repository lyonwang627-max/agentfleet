import { NavLink, Outlet } from 'react-router-dom';
import { LayoutDashboard, KanbanSquare, Timer, Wrench, MessagesSquare, Users, FlaskConical, Gauge, FolderKanban, History, Moon, Sun } from 'lucide-react';
import { useState } from 'react';
import { useHealth } from '../hooks';
import { fmtDuration } from '../ui';

const NAV = [
  { to: '/', label: '舰队总览', icon: LayoutDashboard },
  { to: '/tasks', label: '任务看板', icon: KanbanSquare },
  { to: '/projects', label: '项目', icon: FolderKanban },
  { to: '/automations', label: '自动化', icon: Timer },
  { to: '/studio', label: '工作台', icon: Wrench },
  { to: '/chat', label: '试聊台', icon: MessagesSquare },
  { to: '/room', label: '群聊会话', icon: Users },
  { to: '/replays', label: '会话回放', icon: History },
  { to: '/evals', label: '评测中心', icon: FlaskConical },
  { to: '/usage', label: '用量看板', icon: Gauge },
];

export default function Layout() {
  const health = useHealth();
  const down = health.error !== null;
  const h = health.data;

  // I9 主题切换：data-theme 驱动 token 覆盖，localStorage 记忆（首帧前 index.html 内联脚本已初始化）
  const [theme, setTheme] = useState<'dark' | 'light'>(() =>
    document.documentElement.dataset.theme === 'light' ? 'light' : 'dark');
  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    localStorage.setItem('af-theme', next);
    setTheme(next);
  };

  return (
    <div className="flex h-full min-h-screen">
      {/* 侧边栏 */}
      <aside className="glass flex w-52 shrink-0 flex-col border-r border-[var(--hairline)]">
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
                `flex items-center gap-2.5 rounded-lg px-3 py-[7px] text-[13px] transition-colors ${
                  isActive ? 'bg-accent font-medium text-white' : 'text-muted hover:bg-[var(--row-hover-bg)] hover:text-fg'
                }`
              }
            >
              <n.icon size={15} strokeWidth={2} className="shrink-0" />
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
        <header className="glass flex items-center gap-3 border-b border-[var(--hairline)] px-6 py-3">
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
                      ? theme === 'dark'
                        ? 'border-violet-500/40 bg-violet-500/10 text-violet-300'
                        : 'border-violet-600/40 bg-violet-500/10 text-violet-700'
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
          <button
            onClick={toggleTheme}
            aria-label={theme === 'dark' ? '切换到亮色主题' : '切换到暗色主题'}
            title={theme === 'dark' ? '亮色主题' : '暗色主题'}
            className="ml-auto rounded-lg p-1.5 text-muted transition-colors hover:bg-[var(--row-hover-bg)] hover:text-fg"
          >
            {theme === 'dark' ? <Sun size={15} strokeWidth={2} /> : <Moon size={15} strokeWidth={2} />}
          </button>
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
