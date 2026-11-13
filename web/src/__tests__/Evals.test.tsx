import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

// mock api（Evals 页数据拉取）
vi.mock('../api', () => ({
  api: {
    listEvals: vi.fn().mockResolvedValue([
      { id: 1, dataset_id: 1, agent_id: 1, status: 'done', score_avg: 7.5,
        dataset_version: 3, created_at: '2026-11-14T10:00:00Z' },
      { id: 2, dataset_id: 1, agent_id: 1, status: 'running', score_avg: null,
        created_at: '2026-11-14T10:05:00Z' },
    ]),
    listDatasets: vi.fn().mockResolvedValue([{ id: 1, name: '评测集A', description: '', case_count: 8, version: 3, created_at: 'x' }]),
    listAgents: vi.fn().mockResolvedValue({ items: [{ id: 1, name: 'ea' }], total: 1 }),
    createEval: vi.fn(),
  },
}));

const Evals = () => import('../pages/Evals').then((m) => m.default);

describe('Evals 页渲染（I40 前端收尾）', () => {
  it('eval run 行显示 ds vN 版本徽章', async () => {
    const Comp = await Evals();
    const { container } = render(
      <MemoryRouter>
        <Comp />
      </MemoryRouter>,
    );
    // 等待 useApi 异步收敛
    await new Promise((r) => setTimeout(r, 50));
    expect(container.textContent).toContain('ds v3');
  }, 3000);
});
