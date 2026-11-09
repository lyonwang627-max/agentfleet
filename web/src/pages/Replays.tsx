import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useApi } from '../hooks';
import type { ReplayMessage } from '../types';
import { Panel, SkeletonBlock, StatusBadge, fmtTime, fmtNum } from '../ui';

// 会话回放（F6-b）：左侧会话列表，右侧消息时间线 + 关联 runs（链到任务 Trace）。
export default function Replays() {
  const conversations = useApi(() => api.listConversations(), []);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const items = conversations.data?.items ?? [];

  const current = selectedId !== null ? selectedId : (items[0]?.id ?? null);
  const detail = useApi(
    () => (current !== null ? api.getConversation(current) : Promise.resolve(null)),
    [current],
    current !== null,
  );

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[360px_minmax(0,1fr)]">
      {/* 左：会话列表 */}
      <div className="self-start">
        <Panel title={`会话（${items.length}）`} bodyClass="p-0">
          {conversations.loading && <div className="p-4"><SkeletonBlock rows={4} /></div>}
          {conversations.error && (
            <div className="p-4 text-xs text-bad">{conversations.error}</div>
          )}
          {!conversations.loading && items.length === 0 && (
            <div className="px-4 py-8 text-center text-xs text-muted">
              还没有会话——去试聊台与 agent 对话一轮，这里就能回放
            </div>
          )}
          {items.map((c) => (
            <button
              key={c.id}
              type="button"
              onClick={() => setSelectedId(c.id)}
              className={`flex w-full flex-col gap-1 border-b border-border/60 px-4 py-2.5 text-left text-xs last:border-b-0 ${
                current === c.id ? 'bg-accent/10' : 'hover:bg-panel2'
              }`}
            >
              <div className="flex w-full items-center gap-2">
                <span className="font-mono text-muted">#{c.id}</span>
                <span className="font-medium text-fg">{c.agent_name ?? `agent#${c.agent_id}`}</span>
                <span className="ml-auto font-mono text-[10px] text-muted">{fmtTime(c.created_at)}</span>
              </div>
              <div className="flex w-full items-center gap-2 text-muted">
                <span className="max-w-52 truncate" title={c.title}>{c.title}</span>
                <span className="ml-auto font-mono text-[10px]">
                  💬 {c.message_count} · runs {c.run_count}
                </span>
              </div>
            </button>
          ))}
        </Panel>
      </div>

      {/* 右：回放视图 */}
      <div className="self-start">
        {!detail.loading && detail.data && <ReplayTimeline detail={detail.data} />}
        {detail.loading && current !== null && <Panel><SkeletonBlock rows={6} /></Panel>}
        {current === null && !conversations.loading && (
          <Panel><div className="py-10 text-center text-xs text-muted">从左侧选择一个会话进行回放</div></Panel>
        )}
        {detail.error && <Panel><div className="text-xs text-bad">{detail.error}</div></Panel>}
      </div>
    </div>
  );
}

function ReplayTimeline({ detail }: { detail: NonNullable<import('../types').ConversationDetail> }) {
  return (
    <div className="space-y-4">
      <Panel title={`回放 · ${detail.agent_name ?? `agent#${detail.agent_id}`} · ${detail.title}`}>
        <div className="space-y-3">
          {detail.messages.map((m: ReplayMessage) => (
            <div key={m.id} className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}>
              <div
                className={`max-w-[85%] rounded-xl px-3.5 py-2.5 text-xs leading-relaxed ${
                  m.role === 'user'
                    ? 'rounded-br-sm bg-accent/15 text-fg'
                    : 'rounded-bl-sm border border-border bg-panel2 text-fg'
                }`}
              >
                <div className="mb-1 flex items-center gap-2 text-[10px] text-muted">
                  <span>{m.role === 'user' ? '你' : (detail.agent_name ?? 'agent')}</span>
                  <span>{fmtTime(m.created_at)}</span>
                </div>
                <div className="whitespace-pre-wrap">{m.content}</div>
              </div>
            </div>
          ))}
          {detail.messages.length === 0 && (
            <div className="py-6 text-center text-xs text-muted">该会话没有消息记录</div>
          )}
        </div>
      </Panel>

      {detail.runs.length > 0 && (
        <Panel title={`关联 Runs（${detail.runs.length}）`} bodyClass="p-0">
          {detail.runs.map((r) => (
            <Link
              key={r.run_id}
              to={`/tasks/${r.task_id}`}
              className="flex flex-wrap items-center gap-3 border-b border-border/60 px-4 py-2 text-xs last:border-b-0 hover:bg-panel2"
            >
              <span className="font-mono text-muted">run#{r.run_id}</span>
              <StatusBadge status={r.status} />
              <span className="max-w-64 truncate text-muted" title={r.task_title ?? ''}>{r.task_title}</span>
              <span className="ml-auto font-mono text-[10px] text-muted">
                tokens {fmtNum(r.total_tokens)} · {fmtTime(r.started_at)}
              </span>
            </Link>
          ))}
        </Panel>
      )}
    </div>
  );
}
