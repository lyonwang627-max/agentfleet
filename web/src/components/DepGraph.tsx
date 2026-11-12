import { useNavigate } from 'react-router-dom';
import { AlertTriangle } from 'lucide-react';
import { api } from '../api';
import { useApi } from '../hooks';
import { Panel } from '../ui';
import type { TaskDagNode } from '../types';

// 依赖图（I20）：最长路径分层 + SVG 贝塞尔连线。
// 边语义 from=任务 → to=依赖，故依赖在上层、任务在下层（数据流自上而下）。
const NODE_W = 170;
const NODE_H = 32;
const ROW_H = 72;
const COL_GAP = 18;
const MARGIN = 12;

const statusColor: Record<string, string> = {
  done: 'var(--color-ok)',
  running: 'var(--color-accent)',
  failed: 'var(--color-bad)',
  canceled: 'var(--color-muted)',
  pending_approval: 'var(--color-warn)',
  queued: 'var(--color-muted)',
};

// 像素预算截断：CJK ≈ fontSize、ASCII ≈ 0.55×fontSize（按字符数截会溢出框）
export function fitLabel(id: number, title: string, budget: number): string {
  const head = `#${id} `;
  const chars = [...(head + title)];
  let w = 0;
  for (let i = 0; i < chars.length; i++) {
    w += /[\u2e80-\uffff]/.test(chars[i]) ? 11 : 6.2;
    if (w > budget) return chars.slice(0, Math.max(i - 1, head.length)).join('').trimEnd() + '…';
  }
  return chars.join('');
}

export default function DepGraph({ taskId }: { taskId: number }) {
  const dag = useApi(() => api.getTaskDag(taskId), [taskId], taskId > 0);
  const navigate = useNavigate();

  if (dag.loading) return null;
  if (dag.error) return null; // 依赖文字列表是兜底，图挂了不打扰主流程
  const nodes = dag.data?.nodes ?? [];
  const edges = dag.data?.edges ?? [];
  if (nodes.length === 0) return null; // 无依赖不渲染面板

  const byId = new Map<number, TaskDagNode>(nodes.map((n) => [n.id, n]));
  const depsOf = new Map<number, number[]>();
  for (const e of edges) {
    if (byId.has(e.from) && byId.has(e.to)) {
      depsOf.set(e.from, [...(depsOf.get(e.from) ?? []), e.to]);
    }
  }

  // 分层：level=0 无依赖；否则 1+max(level(dep))。环时用深度兜底防递归炸栈。
  const level = new Map<number, number>();
  const lv = (id: number, depth = 0): number => {
    if (level.has(id)) return level.get(id) as number;
    if (depth > nodes.length) return 0;
    let m = 0;
    for (const d of depsOf.get(id) ?? []) m = Math.max(m, lv(d, depth + 1) + 1);
    level.set(id, m);
    return m;
  };

  // 行布局：每行内水平居中
  const rows: TaskDagNode[][] = [];
  for (const n of nodes) {
    const l = lv(n.id);
    (rows[l] ??= []).push(n);
  }
  const canvasW = Math.max(...rows.map((r) => (r?.length ?? 0))) * (NODE_W + COL_GAP) - COL_GAP + MARGIN * 2;
  const canvasH = rows.filter(Boolean).length * ROW_H + MARGIN * 2;
  const pos = new Map<number, { x: number; y: number }>();
  rows.forEach((row, l) => {
    if (!row) return;
    const rowW = row.length * (NODE_W + COL_GAP) - COL_GAP;
    row.forEach((n, i) => {
      pos.set(n.id, { x: (canvasW - rowW) / 2 + i * (NODE_W + COL_GAP), y: MARGIN + l * ROW_H });
    });
  });

  const cycle = dag.data?.cycle ?? false;

  return (
    <Panel title="依赖图" right={
      cycle ? (
        <span className="flex items-center gap-1.5 text-[11px] text-bad">
          <AlertTriangle size={12} /> 检测到环
        </span>
      ) : (
        <span className="text-[11px] text-muted">{nodes.length} 个节点</span>
      )
    }>
      {cycle && (
        <div className="mb-2 rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-[11px] text-bad">
          依赖链上存在环——被环困住的任务不会被调度（依赖闸门等待 done）。
        </div>
      )}
      <div className="overflow-x-auto">
        <svg width={canvasW} height={canvasH} className="min-w-full">
          <defs>
            <marker id="dg-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
              <path d="M0,0 L8,4 L0,8 z" fill="var(--color-border)" />
            </marker>
            <marker id="dg-arrow-bad" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
              <path d="M0,0 L8,4 L0,8 z" fill="var(--color-bad)" />
            </marker>
          </defs>
          {edges.map((e, i) => {
            const a = pos.get(e.from);
            const b = pos.get(e.to);
            if (!a || !b) return null;
            const x1 = a.x + NODE_W / 2;
            const y1 = a.y + NODE_H; // 依赖方（下层任务）底部出发
            const x2 = b.x + NODE_W / 2;
            const y2 = b.y; // 落到上层依赖顶部
            const my = (y1 + y2) / 2;
            const sameLevel = level.get(e.from) === level.get(e.to); // 环边
            return (
              <path
                key={i}
                d={`M${x1},${y1} C${x1},${my} ${x2},${my} ${x2},${y2}`}
                fill="none"
                stroke={sameLevel ? 'var(--color-bad)' : 'var(--color-border)'}
                strokeWidth={1.5}
                strokeDasharray={sameLevel ? '4 3' : undefined}
                markerEnd={sameLevel ? 'url(#dg-arrow-bad)' : 'url(#dg-arrow)'}
              />
            );
          })}
          {nodes.map((n) => {
            const p = pos.get(n.id);
            if (!p) return null;
            const color = statusColor[n.status] ?? 'var(--color-muted)';
            const label = fitLabel(n.id, n.title, NODE_W - 36);
            return (
              <g
                key={n.id}
                className="cursor-pointer"
                onClick={() => void navigate(`/tasks/${n.id}`)}
                style={{ transition: 'opacity .15s' }}
              >
                <title>{`#${n.id} ${n.title} · ${n.status}`}</title>
                <rect
                  x={p.x}
                  y={p.y}
                  width={NODE_W}
                  height={NODE_H}
                  rx={8}
                  style={{ fill: 'var(--color-panel2)', stroke: color, strokeWidth: 1.5 }}
                />
                {/* 状态色点 */}
                <circle cx={p.x + 14} cy={p.y + NODE_H / 2} r={3.5} style={{ fill: color }} />
                <text
                  x={p.x + 26}
                  y={p.y + NODE_H / 2 + 4}
                  fontSize={11}
                  style={{ fill: 'var(--color-fg)', fontFamily: 'var(--font-mono)' }}
                >
                  {label}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
    </Panel>
  );
}
