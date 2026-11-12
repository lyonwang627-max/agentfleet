import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { fitLabel } from '../components/DepGraph';

// mock api 模块（组件内部 useApi 拉取）——factory 内联数据（vi.mock 提升限制）
const dag = {
  nodes: [
    { id: 1, title: '前置任务', status: 'done' },
    { id: 2, title: '主任务', status: 'queued' },
  ],
  edges: [{ from: 2, to: 1 }],
  cycle: false,
};

vi.mock('../api', () => ({
  api: {
    getTaskDag: vi.fn().mockResolvedValue({
      nodes: [
        { id: 1, title: '前置任务', status: 'done' },
        { id: 2, title: '主任务', status: 'queued' },
      ],
      edges: [{ from: 2, to: 1 }],
      cycle: false,
    }),
  },
}));

const DepGraph = () => {
  // 延迟 import（vi.mock 提升后正常解析）
  return import('../components/DepGraph').then((m) => m.default);
};

describe('fitLabel 像素截断', () => {
  it('CJK 按 11px 计宽截断', () => {
    const out = fitLabel(1, '很长的中文标题'.repeat(5), 60);
    expect(out.endsWith('…')).toBe(true);
    expect(out.length).toBeLessThan(12);
  });
  it('短标签不截断', () => {
    expect(fitLabel(1, '短', 134)).toBe('#1 短');
  });
});

describe('DepGraph 渲染', () => {
  it('渲染 SVG 节点与标题', async () => {
    const Comp = await DepGraph();
    const { container } = render(
      <MemoryRouter>
        <Comp taskId={99} />
      </MemoryRouter>,
    );
    // 等待 useApi 异步收敛
    await new Promise((r) => setTimeout(r, 30));
    expect(container.querySelectorAll('rect').length).toBe(2);
    expect(screen.getByText('依赖图')).toBeTruthy();
    expect(container.textContent).toContain('2 个节点');
  }, 3000);

  it('cycle=true 显示警示', async () => {
    const { api } = await import('../api');
    (api.getTaskDag as ReturnType<typeof vi.fn>).mockResolvedValue({ ...dag, cycle: true });
    const mod = await import('../components/DepGraph');
    const Comp = mod.default;
    const { container } = render(
      <MemoryRouter>
        <Comp taskId={98} />
      </MemoryRouter>,
    );
    await new Promise((r) => setTimeout(r, 30));
    expect(container.textContent).toContain('检测到环');
  }, 3000);
});
