import { useEffect, useMemo, useRef, useState } from 'react';
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
// 输入框内键入 @ 弹出成员补全浮层（↑↓ 选择 / Tab 或 Enter 补全 / Esc 关闭）。
export default function Room() {
  const agents = useApi(() => api.listAgents(), []);
  const [conversationId, setConversationId] = useState<number | null>(null);
  const [messages, setMessages] = useState<RoomMsg[]>([]);
  const [input, setInput] = useState('');
  const [pending, setPending] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [lastWoken, setLastWoken] = useState<number | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // ---- @ 补全状态 ----
  const [mentionQuery, setMentionQuery] = useState<string | null>(null); // null=关闭；''=刚键入@
  const [mentionIdx, setMentionIdx] = useState(0);

  const items = agents.data?.items ?? [];
  const enabled = useMemo(() => items.filter((a) => a.enabled), [items]);

  // 候选：按已输入前缀过滤；始终含 all
  const mentionCandidates = useMemo(() => {
    if (mentionQuery === null) return [];
    const q = mentionQuery.toLowerCase();
    const agentNames = enabled
      .filter((a) => a.name.toLowerCase().includes(q))
      .map((a) => a.name);
    const extra = ['all'].filter((n) => n.includes(q));
    return [...agentNames, ...extra];
  }, [mentionQuery, enabled]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, pending]);

  useEffect(() => setMentionIdx(0), [mentionQuery]);

  // 从输入框解析「正在输入的 @ 前缀」：光标左侧最近的未闭合 @token
  const detectMention = (value: string, caret: number) => {
    const before = value.slice(0, caret);
    const at = before.lastIndexOf('@');
    if (at === -1) {
      setMentionQuery(null);
      return;
    }
    const token = before.slice(at + 1);
    // token 里出现空白/另一个 @ 即视为已闭合
    if (/[\s@]/.test(token)) {
      setMentionQuery(null);
      return;
    }
    setMentionQuery(token);
  };

  const applyMention = (name: string) => {
    if (mentionQuery === null || !inputRef.current) return;
    const caret = inputRef.current.selectionStart ?? input.length;
    const before = input.slice(0, caret);
    const at = before.lastIndexOf('@');
    if (at === -1) return;
    const next = input.slice(0, at) + '@' + name + ' ' + input.slice(caret);
    setInput(next);
    setMentionQuery(null);
    // 光标移到补全之后
    requestAnimationFrame(() => {
      const pos = at + name.length + 2;
      inputRef.current?.focus();
      inputRef.current?.setSelectionRange(pos, pos);
    });
  };

  const onInputChange = (v: string) => {
    setInput(v);
    const caret = inputRef.current?.selectionStart ?? v.length;
    detectMention(v, caret);
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    // 补全浮层打开时接管按键
    if (mentionQuery !== null && mentionCandidates.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setMentionIdx((i) => (i + 1) % mentionCandidates.length);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setMentionIdx((i) => (i - 1 + mentionCandidates.length) % mentionCandidates.length);
        return;
      }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing)) {
        e.preventDefault();
        applyMention(mentionCandidates[mentionIdx]);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        setMentionQuery(null);
        return;
      }
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void send();
    }
  };

  const send = async () => {
    const text = input.trim();
    if (!text || pending) return;
    setInput('');
    setMentionQuery(null);
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
    inputRef.current?.focus();
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
              onClick={() => insertMention('all')}
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
              键入 @ 可弹出成员补全。
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

        {/* 输入（含 @ 补全浮层） */}
        <div className="space-y-2 border-t border-white/6 p-3">
          {err && <ErrorText>{err}</ErrorText>}
          <div className="relative flex gap-2">
            {mentionQuery !== null && mentionCandidates.length > 0 && (
              <div className="elev-2 absolute bottom-full left-0 mb-2 w-56 overflow-hidden rounded-xl bg-panel py-1">
                {mentionCandidates.slice(0, 6).map((name, i) => (
                  <button
                    key={name}
                    type="button"
                    onMouseEnter={() => setMentionIdx(i)}
                    onMouseDown={(e) => {
                      e.preventDefault(); // 不让 input 失焦
                      applyMention(name);
                    }}
                    className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors ${
                      i === mentionIdx ? 'bg-accent text-white' : 'text-fg hover:bg-white/5'
                    }`}
                  >
                    <span className={i === mentionIdx ? 'text-white/70' : 'text-muted'}>@</span>
                    {name}
                    {name === 'all' && <span className="ml-auto text-[10px] opacity-60">全员</span>}
                  </button>
                ))}
                <div className="border-t border-white/6 px-3 py-1 text-[10px] text-muted">
                  ↑↓ 选择 · Tab/Enter 补全 · Esc 关闭
                </div>
              </div>
            )}
            <input
              ref={inputRef}
              className="flex-1 rounded-lg border border-border bg-transparent px-3 py-2 text-xs text-fg outline-none placeholder:text-muted focus:border-accent"
              placeholder="键入 @ 唤醒成员（支持中文名 / @all）"
              value={input}
              onChange={(e) => onInputChange(e.target.value)}
              onKeyDown={onKeyDown}
              onBlur={() => {
                // 延迟关闭让 mousedown 先触发
                setTimeout(() => setMentionQuery(null), 150);
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
