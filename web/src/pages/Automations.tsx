import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import type { Automation } from '../types';
import { Btn, ErrorText, Panel, SkeletonBlock, fmtTime } from '../ui';

// Automations：cron 定时建任务的管理页（F1-c）。
// 服务端持有调度状态（next_run 由后端按 cron 计算），前端只做 CRUD 与观察。
export default function Automations() {
  const agents = useApi(() => api.listAgents(), []);
  const autos = useApi(() => api.listAutomations(), []);
  const [form, setForm] = useState({ name: '', cron_expr: '', title: '', prompt: '', priority: 5 });
  const [agentId, setAgentId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const agentItems = agents.data?.items ?? [];
  useEffect(() => {
    if (!agentId && agentItems.length > 0) setAgentId(String(agentItems[0].id));
  }, [agentItems, agentId]);

  // 轻量轮询：next_run 会随时间推进，15s 刷新足够
  useEffect(() => {
    const t = setInterval(() => void autos.reload(), 15000);
    return () => clearInterval(t);
  }, [autos.reload]);

  const submit = async () => {
    if (!agentId || !form.name.trim() || !form.title.trim() || !form.cron_expr.trim()) {
      setErr('name / cron_expr / title / agent 均必填');
      return;
    }
    setSubmitting(true);
    setErr(null);
    setNotice(null);
    try {
      const res = await api.createAutomation({ ...form, agent_id: Number(agentId) });
      setNotice(`已创建，下次触发：${fmtTime(res.next_run)}`);
      setForm({ name: '', cron_expr: '', title: '', prompt: '', priority: 5 });
      await autos.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const toggle = async (a: Automation) => {
    try {
      await api.setAutomationEnabled(a.id, !a.enabled);
      await autos.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  const remove = async (a: Automation) => {
    try {
      await api.deleteAutomation(a.id);
      await autos.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="space-y-4">
      {notice && (
        <div className="rounded border border-ok/40 bg-ok/10 px-4 py-2 text-xs text-ok">
          {notice}
        </div>
      )}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[380px_minmax(0,1fr)]">
        {/* 左：新建规则 */}
        <div className="self-start">
          <Panel title="新建自动化规则">
            <div className="space-y-2.5">
              <Field label="名称">
                <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="晨会摘要" className={inputCls} />
              </Field>
              <Field label="Agent">
                <select value={agentId} onChange={(e) => setAgentId(e.target.value)} className={inputCls}>
                  {agentItems.length === 0 && <option value="">无可用 Agent</option>}
                  {agentItems.map((a) => (
                    <option key={a.id} value={a.id}>{a.name}</option>
                  ))}
                </select>
              </Field>
              <Field label="Cron 表达式（分 时 日 月 周）">
                <input value={form.cron_expr} onChange={(e) => setForm({ ...form, cron_expr: e.target.value })}
                  placeholder="30 9 * * 1-5" className={`${inputCls} font-mono`} />
              </Field>
              <Field label="任务标题">
                <input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })}
                  placeholder="生成今日晨会摘要" className={inputCls} />
              </Field>
              <Field label="任务说明（prompt）">
                <textarea value={form.prompt} onChange={(e) => setForm({ ...form, prompt: e.target.value })}
                  rows={3} placeholder="任务的详细说明…" className={inputCls} />
              </Field>
              <Field label={`优先级 ${form.priority}`}>
                <input type="range" min={1} max={9} value={form.priority}
                  onChange={(e) => setForm({ ...form, priority: Number(e.target.value) })} className="w-full" />
              </Field>
              {err && <ErrorText>{err}</ErrorText>}
              <Btn variant="primary" disabled={submitting} onClick={() => void submit()} className="w-full justify-center">
                {submitting ? '创建中…' : '创建规则（POST /automations）'}
              </Btn>
              <div className="text-[11px] leading-relaxed text-muted">
                到期后调度器自动创建任务并派给所选 Agent；cron 校验与 next_run 计算都在服务端。
              </div>
            </div>
          </Panel>
        </div>

        {/* 右：规则列表 */}
        <div className="self-start">
          <Panel title={`规则列表（${autos.data?.items.length ?? 0}）`} bodyClass="p-0">
            {autos.error && <div className="p-4"><ErrorText>{autos.error}</ErrorText></div>}
            {autos.loading && <div className="p-4"><SkeletonBlock rows={5} /></div>}
            {!autos.loading && !autos.error && (autos.data?.items.length ?? 0) === 0 && (
              <div className="px-4 py-8 text-center text-xs text-muted">
                还没有自动化规则——左侧创建一条，或查看 seed 自带的「晨会摘要」
              </div>
            )}
            {(autos.data?.items ?? []).map((a) => (
              <div key={a.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border/60 px-4 py-3 text-xs last:border-b-0">
                <span className="font-mono text-muted">#{a.id}</span>
                <span className={`h-1.5 w-1.5 rounded-full ${a.enabled ? 'bg-ok' : 'bg-slate-500'}`} aria-hidden />
                <span className="font-medium text-fg">{a.name}</span>
                <span className="rounded border border-border px-1.5 py-px font-mono text-[11px] text-[#f0a830]">{a.cron_expr}</span>
                <span className="text-muted">→ {a.agent_name ?? `agent#${a.agent_id}`} · {a.title}</span>
                <span className="ml-auto flex items-center gap-3 font-mono text-[11px] tabular-nums">
                  <span className="text-muted">上次 {a.last_run ? fmtTime(a.last_run) : '—'}</span>
                  <span className="text-[#f0a830]">下次 {a.next_run ? fmtTime(a.next_run) : '—'}</span>
                </span>
                <span className="flex w-full items-center justify-end gap-2">
                  <Btn onClick={() => void toggle(a)}>{a.enabled ? '停用' : '启用'}</Btn>
                  <Btn variant="danger" onClick={() => void remove(a)}>删除</Btn>
                </span>
              </div>
            ))}
          </Panel>
        </div>
      </div>
    </div>
  );
}

const inputCls =
  'w-full rounded border border-border bg-bg px-2.5 py-1.5 text-xs text-fg outline-none focus:border-accent-dim';

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <div className="mb-1 text-xs text-muted">{label}</div>
      {children}
    </div>
  );
}
