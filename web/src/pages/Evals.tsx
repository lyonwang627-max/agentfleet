import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import type { EvalDataset, EvalSummary } from '../types';
import { Btn, ErrorText, Panel, SkeletonBlock, StatusBadge, fmtRate, fmtTime } from '../ui';

export default function Evals() {
  const agents = useApi(() => api.listAgents(), []);
  const datasets = useApi(() => api.listDatasets(), []);
  const evals = useApi(() => api.listEvals(), []);
  const [agentId, setAgentId] = useState('');
  const [datasetId, setDatasetId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);

  const agentItems = agents.data?.items ?? [];
  const datasetItems = datasets.data ?? [];

  useEffect(() => {
    if (!agentId && agentItems.length > 0) setAgentId(String(agentItems[0].id));
  }, [agentItems, agentId]);
  useEffect(() => {
    if (!datasetId && datasetItems.length > 0) setDatasetId(String(datasetItems[0].id));
  }, [datasetItems, datasetId]);

  const submit = async () => {
    if (!agentId || !datasetId) {
      setErr('请选择 agent 与 dataset');
      return;
    }
    setSubmitting(true);
    setErr(null);
    try {
      const res = await api.createEval({ agent_id: Number(agentId), dataset_id: Number(datasetId) });
      setSelectedId(res.id);
      await evals.reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const agentName = (id: number) => agentItems.find((a) => a.id === id)?.name ?? `#${id}`;
  const datasetName = (id: number) => datasetItems.find((d) => d.id === id)?.name ?? `#${id}`;

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[380px_minmax(0,1fr)]">
      {/* 左：发起表单 + 评测列表 */}
      <div className="space-y-4 self-start">
        <Panel title="发起评测">
          <div className="space-y-2.5">
            <div>
              <div className="mb-1 text-xs text-muted">Agent</div>
              <select
                value={agentId}
                onChange={(e) => setAgentId(e.target.value)}
                className="w-full rounded border border-border bg-bg px-2.5 py-1.5 text-xs text-fg outline-none focus:border-sky-600/60"
              >
                {agentItems.length === 0 && <option value="">无可用 Agent</option>}
                {agentItems.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <div className="mb-1 text-xs text-muted">Dataset</div>
              <select
                value={datasetId}
                onChange={(e) => setDatasetId(e.target.value)}
                className="w-full rounded border border-border bg-bg px-2.5 py-1.5 text-xs text-fg outline-none focus:border-sky-600/60"
              >
                {datasetItems.length === 0 && <option value="">无可用数据集</option>}
                {datasetItems.map((d: EvalDataset) => (
                  <option key={d.id} value={d.id}>
                    {d.name}（{d.cases} cases）
                  </option>
                ))}
              </select>
            </div>
            {err && <ErrorText>{err}</ErrorText>}
            <Btn variant="primary" disabled={submitting} onClick={() => void submit()} className="w-full justify-center">
              {submitting ? '发起中…' : '发起评测（POST /evals）'}
            </Btn>
          </div>
        </Panel>

        <Panel title={`评测列表（${evals.data?.length ?? 0}）`} bodyClass="p-0">
          {evals.error && (
            <div className="p-4">
              <ErrorText>{evals.error}</ErrorText>
            </div>
          )}
          {evals.loading && (
            <div className="p-4">
              <SkeletonBlock rows={4} />
            </div>
          )}
          {!evals.loading && !evals.error && (evals.data?.length ?? 0) === 0 && (
            <div className="px-4 py-6 text-center text-xs text-muted">尚无评测记录</div>
          )}
          {(evals.data ?? [])
            .slice()
            .sort((a, b) => b.id - a.id)
            .map((ev: EvalSummary) => (
              <button
                key={ev.id}
                type="button"
                onClick={() => setSelectedId(ev.id)}
                className={`flex w-full flex-wrap items-center gap-2 border-b border-border/60 px-4 py-2.5 text-left text-xs last:border-b-0 ${
                  selectedId === ev.id ? 'bg-sky-600/10' : 'hover:bg-panel2'
                }`}
              >
                <span className="font-mono text-muted">#{ev.id}</span>
                <span className="font-medium text-fg">{agentName(ev.agent_id)}</span>
                <span className="text-muted">· {datasetName(ev.dataset_id)}</span>
                <StatusBadge status={ev.status} />
                <span className="ml-auto flex items-center gap-2">
                  <span className="font-mono tabular-nums text-fg">
                    {ev.score_avg !== null && ev.score_avg !== undefined ? ev.score_avg.toFixed(2) : '—'}
                  </span>
                  <span className="font-mono text-[10px] text-muted">{fmtTime(ev.created_at)}</span>
                </span>
              </button>
            ))}
        </Panel>
      </div>

      {/* 右：报告视图 */}
      <div className="self-start">
        {selectedId === null ? (
          <Panel>
            <div className="py-10 text-center text-xs text-muted">从左侧选择一条评测查看报告</div>
          </Panel>
        ) : (
          <EvalReportView evalId={selectedId} agentName={agentName} />
        )}
      </div>
    </div>
  );
}

function EvalReportView({ evalId, agentName }: { evalId: number; agentName: (id: number) => string }) {
  const detail = useApi(() => api.getEval(evalId), [evalId]);

  // running 时 3s 轮询直到出报告
  useEffect(() => {
    if (detail.data?.status !== 'running') return;
    const t = setInterval(() => void detail.reload(), 3000);
    return () => clearInterval(t);
  }, [detail.data?.status, detail.reload]);

  if (detail.loading) return <Panel><SkeletonBlock rows={6} /></Panel>;
  if (detail.error) return <Panel><ErrorText>{detail.error}</ErrorText></Panel>;
  const d = detail.data;
  if (!d) return null;
  const report = d.report;

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <BigStat label="评测 ID" value={`#${d.id}`} />
        <BigStat label="状态" value={<StatusBadge status={d.status} />} />
        <BigStat label="Score Avg" value={report ? report.score_avg.toFixed(2) : '…'} accent />
        <BigStat label="Pass Rate" value={report ? fmtRate(report.pass_rate) : '…'} accent />
      </div>

      {d.status === 'running' && (
        <Panel>
          <div className="py-6 text-center text-xs text-muted">评测运行中，每 3s 自动刷新…（报告就绪后自动展示）</div>
        </Panel>
      )}

      {report && (
        <Panel title={`Cases（${report.cases?.length ?? 0}）`} bodyClass="p-0">
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="border-b border-border text-muted">
                  <th className="px-4 py-2 font-medium">case_id</th>
                  <th className="px-4 py-2 font-medium">Score</th>
                  <th className="px-4 py-2 font-medium">Pass</th>
                  <th className="px-4 py-2 font-medium">Judge Comment</th>
                </tr>
              </thead>
              <tbody>
                {(report.cases ?? []).map((c) => (
                  <tr key={c.case_id} className="border-b border-border/50 last:border-b-0">
                    <td className="px-4 py-2 font-mono text-muted">{c.case_id}</td>
                    <td className="px-4 py-2 font-mono tabular-nums">{c.score?.toFixed(2) ?? '—'}</td>
                    <td className="px-4 py-2">
                      <span
                        className={`inline-flex rounded border px-1.5 py-0.5 text-[11px] ${
                          c.pass
                            ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                            : 'border-rose-500/30 bg-rose-500/10 text-rose-300'
                        }`}
                      >
                        {c.pass ? '通过' : '未通过'}
                      </span>
                    </td>
                    <td className="px-4 py-2 leading-relaxed text-slate-300">{c.judge_comment || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}

      <div className="text-[11px] text-muted">Agent：{agentName(evalId)} · tokens 合计见用量看板</div>
    </div>
  );
}

function BigStat({ label, value, accent }: { label: string; value: ReactNode; accent?: boolean }) {
  return (
    <div className="rounded-lg border border-border bg-panel px-4 py-4">
      <div className="text-xs text-muted">{label}</div>
      <div className={`mt-1 text-3xl font-semibold tabular-nums ${accent ? 'text-sky-300' : 'text-fg'}`}>{value}</div>
    </div>
  );
}
