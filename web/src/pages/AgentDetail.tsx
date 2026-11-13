import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useApi } from '../hooks';
import type { RunSummary } from '../types';
import { Btn, ErrorText, Panel, SkeletonBlock, StatCard, StatusBadge, fmtDuration, fmtNum, fmtRate, fmtTime } from '../ui';

type RunRow = RunSummary & { task_id: number; task_title: string };

export default function AgentDetail() {
  const params = useParams();
  const agentId = Number(params.id ?? 0);
  const valid = Number.isInteger(agentId) && agentId > 0;
  const agent = useApi(() => api.getAgent(agentId), [agentId], valid);

  // 近期 runs：契约无 /agents/{id}/runs，借 agent 的近期任务展开其 runs
  const recentRuns = useApi<RunRow[]>(
    async () => {
      const tasks = await api.listTasks({ agent_id: agentId, page_size: 5 });
      const lists = await Promise.all(
        tasks.items.slice(0, 5).map((t) => api.listTaskRuns(t.id).catch(() => [] as RunSummary[])),
      );
      const rows: RunRow[] = [];
      lists.forEach((rs, i) => {
        const t = tasks.items[i];
        for (const r of rs) rows.push({ ...r, task_id: t.id, task_title: t.title });
      });
      return rows.sort((a, b) => b.id - a.id).slice(0, 10);
    },
    [agentId],
    valid,
  );

  const a = agent.data;
  const [form, setForm] = useState({ name: '', role: '', system_prompt: '', model: '', tools: '', max_concurrency: 1 });
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (a) {
      // I14：有未发布草稿时表单以草稿值为准（编辑所见即草稿，发布后才落线上）
      const d = (a.draft_config ?? {}) as Record<string, unknown>;
      const str = (k: string, fb: string) => (typeof d[k] === 'string' ? (d[k] as string) : fb);
      const num = (k: string, fb: number) => (typeof d[k] === 'number' ? (d[k] as number) : fb);
      setForm({
        name: str('name', a.name),
        role: str('role', a.role),
        system_prompt: str('system_prompt', a.system_prompt ?? ''),
        model: str('model', a.model),
        tools: Array.isArray(d.tools) ? (d.tools as string[]).join(',') : (a.tools ?? []).join(','),
        max_concurrency: num('max_concurrency', a.max_concurrency),
      });
    }
  }, [a]);

  if (agent.loading) return <SkeletonBlock rows={6} />;
  if (agent.error) return <ErrorText>{agent.error}</ErrorText>;
  if (!a) return null;

  const save = async () => {
    setSaving(true);
    setErr(null);
    setSaved(false);
    try {
      await api.patchAgent(agentId, {
        name: form.name.trim(),
        role: form.role.trim(),
        system_prompt: form.system_prompt,
        model: form.model.trim(),
        tools: form.tools
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
        max_concurrency: Math.max(1, Math.floor(form.max_concurrency) || 1),
      });
      await agent.reload();
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const inputCls =
    'w-full rounded border border-border bg-bg px-2.5 py-1.5 text-xs text-fg outline-none placeholder:text-muted focus:border-accent-dim';
  const s = a.stats;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 text-xs text-muted">
        <Link to="/" className="hover:text-accent">
          舰队总览
        </Link>
        <span>/</span>
        <span className="font-mono">agent #{a.id}</span>
      </div>

      {/* 统计卡 */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatCard label="总 Runs" value={fmtNum(s?.total_runs)} />
        <StatCard label="成功率" value={fmtRate(s?.success_rate)} />
        <StatCard label="平均延迟" value={fmtDuration(s?.avg_latency_ms)} />
        <StatCard label="30d Tokens" value={fmtNum(s?.tokens_30d)} />
      </div>

      {/* 版本管理（F2）：发布/历史/回滚 */}
      <AgentVersions agentId={agentId} dirty={a.has_unpublished_changes} ramping={a.ramp_pct != null && a.ramp_pct > 0} rampPct={a.ramp_pct ?? 0} onPublished={() => void agent.reload()} />

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,520px)_minmax(0,1fr)]">
        {/* 配置表单 */}
        <Panel
          title="配置"
          right={
            <span className="flex items-center gap-2 text-[11px]">
              <span
                className={`rounded border px-1.5 py-0.5 text-[11px] ${
                  a.enabled
                    ? 'border-ok/40 bg-ok/10 text-ok'
                    : 'border-border bg-muted/10 text-muted'
                }`}
              >
                {a.enabled ? '已启用' : '已停用'}
              </span>
              {a.has_unpublished_changes && (
                <span className="rounded border border-warn/40 bg-warn/10 px-1.5 py-0.5 text-[11px] text-warn">
                  未发布变更
                </span>
              )}
              {a.ramp_pct != null && a.ramp_pct > 0 && (
                <span className="rounded border border-info/40 bg-info/10 px-1.5 py-0.5 text-[11px] text-info">
                  灰度 {a.ramp_pct}% → v{a.ramping_version}
                </span>
              )}
              {saved && <span className="text-ok">已存草稿</span>}
            </span>
          }
        >
          <div className="mb-2.5 text-[11px] text-muted">
            编辑进入草稿（在途 run 不受影响）；点「版本历史 → 发布」落线生效
          </div>
          <div className="space-y-2.5">
            <LabeledInput label="名称" value={form.name} onChange={(v) => setForm((f) => ({ ...f, name: v }))} inputCls={inputCls} />
            <LabeledInput label="角色" value={form.role} onChange={(v) => setForm((f) => ({ ...f, role: v }))} inputCls={inputCls} />
            <div>
              <div className="mb-1 text-xs text-muted">System Prompt</div>
              <textarea
                className={`${inputCls} h-36 resize-y font-mono leading-relaxed`}
                value={form.system_prompt}
                onChange={(e) => setForm((f) => ({ ...f, system_prompt: e.target.value }))}
              />
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">Model（从本机可用模型中选择）</div>
              <ModelSelect value={form.model} onChange={(v) => setForm((f) => ({ ...f, model: v }))} inputCls={inputCls} />
            </div>
            <LabeledInput
              label="最大并发"
              type="number"
              value={String(form.max_concurrency)}
              onChange={(v) => setForm((f) => ({ ...f, max_concurrency: Number(v) }))}
              inputCls={inputCls}
            />
            <LabeledInput
              label="Tools（逗号分隔）"
              value={form.tools}
              onChange={(v) => setForm((f) => ({ ...f, tools: v }))}
              inputCls={inputCls}
            />
            {err && <ErrorText>{err}</ErrorText>}
            <div className="flex items-center justify-between">
              <span className="text-[11px] text-muted">
                runner <span className="font-mono text-fg">{a.runner}</span> · 创建于 {fmtTime(a.created_at)}
              </span>
              <Btn variant="primary" disabled={saving} onClick={() => void save()}>
                {saving ? '保存中…' : '保存（PATCH）'}
              </Btn>
            </div>
          </div>
        </Panel>

        {/* 近期 runs */}
        <Panel title={`近期 Runs（${recentRuns.data?.length ?? 0}）`} bodyClass="p-0">
          {recentRuns.loading && (
            <div className="p-4">
              <SkeletonBlock rows={4} />
            </div>
          )}
          {recentRuns.error && (
            <div className="p-4">
              <ErrorText>{recentRuns.error}</ErrorText>
            </div>
          )}
          {!recentRuns.loading && !recentRuns.error && (recentRuns.data?.length ?? 0) === 0 && (
            <div className="px-4 py-6 text-center text-xs text-muted">近期无执行记录</div>
          )}
          {(recentRuns.data ?? []).map((r) => (
            <div key={r.id} className="flex flex-wrap items-center gap-3 border-b border-border/60 px-4 py-2 text-xs last:border-b-0">
              <span className="font-mono text-muted">#{r.id}</span>
              <StatusBadge status={r.status} />
              <Link to={`/tasks/${r.task_id}`} className="max-w-48 truncate hover:text-accent" title={r.task_title}>
                {r.task_title}
              </Link>
              <span className="ml-auto text-muted">
                tokens <span className="font-mono text-fg">{fmtNum(r.total_tokens)}</span>
              </span>
              <span className="text-muted">
                cost <span className="font-mono text-fg">${r.cost_usd?.toFixed(4) ?? '—'}</span>
              </span>
              <span className="w-28 text-right font-mono text-[10px] text-muted">{fmtTime(r.started_at)}</span>
            </div>
          ))}
        </Panel>
      </div>
    </div>
  );
}

function LabeledInput({
  label,
  value,
  onChange,
  inputCls,
  type = 'text',
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  inputCls: string;
  type?: string;
}) {
  return (
    <div>
      <div className="mb-1 text-xs text-muted">{label}</div>
      <input
        type={type}
        className={inputCls}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        min={type === 'number' ? 1 : undefined}
      />
    </div>
  );
}

// ModelSelect 模型可选化（v1.4）：从 /models 动态发现本机可用模型。
// 后端不可达时降级为自由输入（不阻塞配置编辑）。
function ModelSelect({ value, onChange, inputCls }: { value: string; onChange: (v: string) => void; inputCls: string }) {
  const models = useApi(() => api.models(), []);
  const items = models.data?.models ?? [];

  if (models.error || items.length === 0) {
    return <input className={inputCls} value={value} onChange={(e) => onChange(e.target.value)} placeholder="模型名（如 qwen3.5）" />;
  }
  const known = items.some((m) => m.name === value);
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} className={inputCls}>
      {!known && value && <option value={value}>{value}（当前值，列表中未发现）</option>}
      {items.map((m) => (
        <option key={m.provider + m.name} value={m.name} disabled={!m.available}>
          {m.name} · {m.provider}{m.available ? '' : '（不可用）'}
        </option>
      ))}
    </select>
  );
}

// AgentVersions 版本链（I14）：编辑进草稿、发布=草稿落线+版本快照；回滚=旧快照覆盖+清草稿（链只前进，可追溯）。
export function AgentVersions({ agentId, dirty, ramping, rampPct, onPublished }: { agentId: number; dirty?: boolean; ramping?: boolean; rampPct?: number; onPublished?: () => void }) {
  const versions = useApi(() => api.listAgentVersions(agentId), [agentId]);
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const act = async (fn: () => Promise<string>) => {
    setBusy(true);
    setErr(null);
    setMsg(null);
    try {
      setMsg(await fn());
      await versions.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const items = versions.data?.items ?? [];

  return (
    <Panel
      title={`版本历史（${items.length}）`}
      right={
        <span className="flex items-center gap-2">
          {msg && <span className="text-ok">{msg}</span>}
          <input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="发布备注（可选）"
            className="w-40 rounded border border-border bg-bg px-2 py-1 text-xs text-fg outline-none focus:border-accent-dim"
          />
          {dirty && (
            <span className="rounded border border-warn/40 bg-warn/10 px-1.5 py-0.5 text-[11px] text-warn">
              有草稿待发布
            </span>
          )}
          {ramping && (
            <span className="rounded border border-info/40 bg-info/10 px-1.5 py-0.5 text-[11px] text-info">
              灰度 {rampPct}%
            </span>
          )}
          <Btn variant="primary" disabled={busy} onClick={() => void act(async () => {
            const r = await api.publishAgent(agentId, note.trim());
            setNote('');
            onPublished?.();
            return `已发布 v${r.version}${dirty ? '（草稿已落线）' : '（快照）'}`;
          })}>
            {dirty ? '发布草稿' : '发布当前配置'}
          </Btn>
          {ramping && (
            <Btn disabled={busy} onClick={() => void act(async () => {
              // promote：灰度收敛（后端 promote_ramp）
              const r = await fetch(`/api/v1/agents/${agentId}/publish`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ promote_ramp: true }),
              }).then((x) => x.json());
              onPublished?.();
              return `灰度已全量（v${r.promoted_version}）`;
            })}>
              全量
            </Btn>
          )}
        </span>
      }
      bodyClass="p-0"
    >
      {versions.loading && <div className="p-4"><SkeletonBlock rows={3} /></div>}
      {versions.error && <div className="p-4"><ErrorText>{versions.error}</ErrorText></div>}
      {!versions.loading && !versions.error && items.length === 0 && (
        <div className="px-4 py-6 text-center text-xs text-muted">
          还没有发布过版本——编辑配置进草稿后点「发布」落线并建立 v1
        </div>
      )}
      {items.map((v) => (
        <div key={v.id} className="flex flex-wrap items-center gap-3 border-b border-border/60 px-4 py-2.5 text-xs last:border-b-0">
          <span className="rounded border border-accent-dim/50 bg-sky-500/10 px-1.5 py-0.5 font-mono text-[11px] text-accent">
            v{v.version}
          </span>
          <span className="text-fg">{v.note || '（无备注）'}</span>
          <span className="max-w-md truncate font-mono text-[11px] text-muted" title={String(v.config?.system_prompt ?? '')}>
            {String(v.config?.system_prompt ?? '').slice(0, 60)}
          </span>
          <span className="ml-auto font-mono text-[10px] text-muted">{fmtTime(v.created_at)}</span>
          <Btn disabled={busy} onClick={() => void act(async () => {
            const r = await api.rollbackAgent(agentId, v.version);
            return `已从 v${r.rolled_back_from} 回滚，生成 v${r.new_version}`;
          })}>
            回滚到此版
          </Btn>
        </div>
      ))}
      {err && <div className="px-4 py-2"><ErrorText>{err}</ErrorText></div>}
    </Panel>
  );
}
