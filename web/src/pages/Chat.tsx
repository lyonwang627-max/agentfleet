import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import type { AgentSummary } from '../types';
import { Btn, ErrorText, Panel } from '../ui';

interface ChatMsg {
  role: 'user' | 'assistant';
  text: string;
  run_id?: number;
}

export default function Chat() {
  const agents = useApi(() => api.listAgents(), []);
  const [agentId, setAgentId] = useState('');
  const [conversationId, setConversationId] = useState<number | null>(null);
  const [messages, setMessages] = useState<ChatMsg[]>([]);
  const [input, setInput] = useState('');
  const [pending, setPending] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  const items = agents.data?.items ?? [];
  // 默认选中第一个 agent
  useEffect(() => {
    if (!agentId && items.length > 0) setAgentId(String(items[0].id));
  }, [items, agentId]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, pending]);

  const send = async () => {
    const text = input.trim();
    const aid = Number(agentId);
    if (!text || !aid || pending) return;
    setInput('');
    setErr(null);
    setMessages((m) => [...m, { role: 'user', text }]);
    setPending(true);
    try {
      const res = await api.chat({ agent_id: aid, message: text, conversation_id: conversationId });
      setConversationId(res.conversation_id);
      setMessages((m) => [...m, { role: 'assistant', text: res.reply, run_id: res.run_id }]);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPending(false);
    }
  };

  const sel = items.find((a) => String(a.id) === agentId);

  return (
    <div className="mx-auto flex min-h-[calc(100vh-120px)] max-w-3xl flex-col gap-3">
      <Panel
        title="试聊台"
        right={
          <div className="flex items-center gap-2">
            {conversationId !== null && (
              <span className="font-mono text-[11px] text-muted">conv #{conversationId}</span>
            )}
            <select
              value={agentId}
              onChange={(e) => {
                setAgentId(e.target.value);
                setConversationId(null);
                setMessages([]);
              }}
              className="rounded border border-border bg-bg px-2 py-1 text-xs text-fg outline-none focus:border-sky-600/60"
            >
              {items.length === 0 && <option value="">无可用 Agent</option>}
              {items.map((a: AgentSummary) => (
                <option key={a.id} value={a.id}>
                  {a.name}（{a.enabled ? '启用' : '停用'}）
                </option>
              ))}
            </select>
          </div>
        }
        bodyClass="p-0 flex-1 flex flex-col min-h-0"
        className="flex min-h-0 flex-1 flex-col"
      >
        {/* 气泡流 */}
        <div ref={scrollRef} className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
          {messages.length === 0 && !pending && (
            <div className="py-16 text-center text-xs text-muted">
              选择 Agent 开始对话；每次回复都会产生 run，可回看 trace。
            </div>
          )}
          {messages.map((m, i) => (
            <div key={i} className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}>
              <div
                className={`max-w-[85%] rounded-lg px-3 py-2 text-xs leading-relaxed ${
                  m.role === 'user'
                    ? 'rounded-br-sm bg-sky-600/25 text-sky-100'
                    : 'rounded-bl-sm border border-border bg-panel2 text-fg'
                }`}
              >
                <div className="whitespace-pre-wrap break-words">{m.text}</div>
                {m.run_id !== undefined && (
                  <div className="mt-1.5 text-[10px] text-muted">
                    run <span className="font-mono">#{m.run_id}</span> · trace 可在任务详情回看
                  </div>
                )}
              </div>
            </div>
          ))}
          {pending && (
            <div className="flex justify-start">
              <div className="rounded-lg rounded-bl-sm border border-border bg-panel2 px-3 py-2 text-xs text-muted">
                {sel?.name ?? 'Agent'} 正在思考…
              </div>
            </div>
          )}
        </div>

        {/* 输入区 */}
        <div className="space-y-2 border-t border-border p-3">
          {err && <ErrorText>{err}</ErrorText>}
          <div className="flex gap-2">
            <input
              className="flex-1 rounded border border-border bg-bg px-3 py-2 text-xs text-fg outline-none placeholder:text-muted focus:border-sky-600/60"
              placeholder={`向 ${sel?.name ?? 'Agent'} 发送消息…`}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault();
                  void send();
                }
              }}
              disabled={pending || items.length === 0}
            />
            <Btn variant="primary" disabled={pending || !input.trim() || !agentId} onClick={() => void send()}>
              {pending ? '发送中…' : '发送'}
            </Btn>
          </div>
        </div>
      </Panel>
    </div>
  );
}
