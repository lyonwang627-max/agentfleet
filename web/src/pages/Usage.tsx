import { useState } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import { Btn, ErrorText, Panel, SkeletonBlock, StatCard, fmtNum } from '../ui';

type Window = '24h' | '7d';

export default function Usage() {
  const [win, setWin] = useState<Window>('24h');
  const usage = useApi(() => api.usage(win), [win]);
  const agents = useApi(() => api.listAgents(), []);

  const u = usage.data;
  const agentName = (id: number) => agents.data?.items.find((a) => a.id === id)?.name ?? `agent#${id}`;
  const totalTokens = u ? u.prompt_tokens + u.completion_tokens : 0;
  const maxModelTokens = u ? Math.max(1, ...u.by_model.map((m) => m.tokens)) : 1;
  const maxAgentTokens = u ? Math.max(1, ...u.by_agent.map((a) => a.tokens)) : 1;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold">用量看板（Gateway）</h2>
        <div className="flex items-center gap-2">
          <div className="flex overflow-hidden rounded border border-border">
            {(['24h', '7d'] as Window[]).map((w) => (
              <button
                key={w}
                type="button"
                onClick={() => setWin(w)}
                className={`px-3 py-1 text-xs transition-colors ${
                  win === w ? 'bg-sky-600/25 font-medium text-sky-300' : 'bg-panel2 text-muted hover:text-fg'
                }`}
              >
                {w === '24h' ? '近 24 小时' : '近 7 天'}
              </button>
            ))}
          </div>
          <Btn onClick={() => void usage.reload()}>刷新</Btn>
        </div>
      </div>

      {usage.error && <ErrorText>{usage.error}</ErrorText>}
      {usage.loading && <SkeletonBlock rows={4} />}

      {u && (
        <>
          {/* 总量卡 */}
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatCard label="Requests" value={fmtNum(u.requests)} hint={`window ${u.window}`} />
            <StatCard label="Prompt Tokens" value={fmtNum(u.prompt_tokens)} />
            <StatCard label="Completion Tokens" value={fmtNum(u.completion_tokens)} />
            <StatCard label="Total Tokens" value={fmtNum(totalTokens)} hint="prompt + completion" accent />
          </div>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            {/* by_model */}
            <Panel title="按模型" bodyClass="p-0">
              {(u.by_model?.length ?? 0) === 0 && (
                <div className="px-4 py-6 text-center text-xs text-muted">窗口内无调用</div>
              )}
              {(u.by_model ?? []).map((m) => (
                <div key={m.model} className="border-b border-border/60 px-4 py-2.5 last:border-b-0">
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-mono text-fg">{m.model}</span>
                    <span className="text-muted">
                      {fmtNum(m.requests)} 次 · <span className="font-mono text-fg">{fmtNum(m.tokens)}</span> tokens
                    </span>
                  </div>
                  <div className="mt-1.5 h-1.5 rounded-full bg-bg">
                    <div
                      className="h-1.5 rounded-full bg-sky-500/70"
                      style={{ width: `${(m.tokens / maxModelTokens) * 100}%` }}
                    />
                  </div>
                </div>
              ))}
            </Panel>

            {/* by_agent */}
            <Panel title="按 Agent" bodyClass="p-0">
              {(u.by_agent?.length ?? 0) === 0 && (
                <div className="px-4 py-6 text-center text-xs text-muted">窗口内无调用</div>
              )}
              {(u.by_agent ?? []).map((a) => (
                <div key={a.agent_id} className="border-b border-border/60 px-4 py-2.5 last:border-b-0">
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-fg">{agentName(a.agent_id)}</span>
                    <span className="font-mono text-fg">{fmtNum(a.tokens)} tokens</span>
                  </div>
                  <div className="mt-1.5 h-1.5 rounded-full bg-bg">
                    <div
                      className="h-1.5 rounded-full bg-violet-500/70"
                      style={{ width: `${(a.tokens / maxAgentTokens) * 100}%` }}
                    />
                  </div>
                </div>
              ))}
            </Panel>
          </div>
        </>
      )}
    </div>
  );
}
