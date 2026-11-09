import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import type { AgentSummary } from '../types';
import { Btn, ErrorText, Panel } from '../ui';

interface RoomMsg {
  role: string;
  agent_name?: string;
  text: string;
}

// 群聊会话（I31）：共享上下文，@任意 agent 唤醒其回帖；agent 互 @ 链式接力。
export default function Room() {
  const agents = useApi(() => api.listAgents(), []);
  const [conversationId, setConversationId] = useState<number | null>(null);
  const [messages, setMessages] = useState<RoomMsg[]>([]);
  const [input, setInput] = useState('');
  const [pending, setPending] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [lastWoken, setLastWoken] = useState<number | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  const items = agents.data?.items ?? [];
  const enabled = items.filter((a) => a.enabled);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, pending]);

  const send = async () => {
    const text = input.trim();
    if (!text || pending) return;
    setInput('');
    setErr(null);
    setMessages((m) => [...m, { role: 'user', text }]);
    setPending(true);
    try {
      const res = await fetch('/api/v1/chat/room', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ conversation_id: conversationId, message: text }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const out = await res.json();
      setConversationId(out.conversation_id);
      setLastWoken(out.woken);
      for (const rep of out.replies ?? []) {
        setMessages((m) => [...m, { role: 'agent', agent_name: rep.agent_name, text: rep.content }]);
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPending(false);
    }
  };

  const agentColor = (name?: string) => {
    if (!name) return 'text-muted';
    const palette = ['text-info', 'text-accent', 'text-ok', 'text-bad', 'text-warn'];
    let h = 0;
    for (const c of name) h = (h * 31 + c.charCodeAt(0)) % 997;
    return palette[h % palette.length];
  };

  const insertMention = (name: string) => {
    setInput((v) => (v ? `${v} @${name} ` : `@${name} `));
  };

  return (
    <div className="mx-auto flex min-h-[calc(100vh-100px)] max-w-4xl flex-col gap-3">
      <Panel
        title="群聊会话"
        right={
          <span className="flex items-center gap-2 text-[11px] text-muted">
            {conversationId !== null && <span className="font-mono">room #{conversationId}</span>}
            {lastWoken !== null && <span>唤醒 {lastWoken}</span>}
            <span>深度≤2 · 单轮≤4</span>
          </span>
        }
        bodyClass="p-0 flex-1 flex flex-col min-h-0"
        className="flex min-h-0 flex-1 flex-col"
      >
        {/* @ 快捷条 */}
        {enabled.length > 0 && (
          <div className="flex flex-wrap gap-1.5 border-b border-white/6 px-4 py-2">
            {enabled.map((a: AgentSummary) => (
              <button
                key={a.id}
                type="button"
                onClick={() => insertMention(a.name)}
                className="press rounded-full border border-border px-2.5 py-0.5 text-[11px] text-muted transition-colors hover:border-accent-dim hover:text-accent"
              >
                @{a.name}
              </button>
            ))}
            <button
              type="button"
              onClick={() => setInput((v) => `${v} @all `)}
              className="press rounded-full border border-accent/40 px-2.5 py-0.5 text-[11px] text-accent"
            >
              @all
            </button>
          </div>
        )}

        {/* 共享时间线 */}
        <div ref={scrollRef} className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
          {messages.length === 0 && !pending && (
            <div className="py-14 text-center text-xs text-muted">
              在一条共享时间线上 @ 任意成员，他们会以自己的人格回帖——成员之间也能互 @ 接力。
            </div>
          )}
          {messages.map((m, i) => (
            <div key={i} className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}>
              <div
                className={`max-w-[85%] rounded-2xl px-3.5 py-2.5 text-xs leading-relaxed ${
                  m.role === 'user'
                    ? 'rounded-br-md bg-accent text-white'
                    : 'rounded-bl-md bg-panel2 text-fg'
                }`}
              >
                <div className="mb-1 flex items-center gap-2 text-[10px]">
                  <span className={m.role === 'user' ? 'text-white/70' : agentColor(m.agent_name)}>
                    {m.role === 'user' ? '你' : `@${m.agent_name ?? 'agent'}`}
                  </span>
                </div>
                <div className="whitespace-pre-wrap break-words">{m.text}</div>
              </div>
            </div>
          ))}
          {pending && (
            <div className="flex justify-start">
              <div className="rounded-2xl rounded-bl-md bg-panel2 px-3.5 py-2.5 text-xs text-muted">
                成员正在读时间线并回帖…
              </div>
            </div>
          )}
        </div>

        {/* 输入 */}
        <div className="space-y-2 border-t border-white/6 p-3">
          {err && <ErrorText>{err}</ErrorText>}
          <div className="flex gap-2">
            <input
              className="flex-1 rounded-lg border border-border bg-transparent px-3 py-2 text-xs text-fg outline-none placeholder:text-muted focus:border-accent"
              placeholder="@fixer 评估这个方案，@reviewer 复核（支持中文名 / @all）"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault();
                  void send();
                }
              }}
              disabled={pending}
            />
            <Btn variant="primary" disabled={pending || !input.trim()} onClick={() => void send()}>
              {pending ? '协作中…' : '发送'}
            </Btn>
          </div>
        </div>
      </Panel>
    </div>
  );
}
