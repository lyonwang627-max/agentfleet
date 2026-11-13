import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

// mock api 模块（AgentVersions 内部 useApi 拉版本链 + publish/rollback 调用）
vi.mock('../api', () => ({
  api: {
    listAgentVersions: vi.fn().mockResolvedValue({ items: [], total: 0 }),
    publishAgent: vi.fn().mockResolvedValue({ agent_id: 1, version: 2 }),
    rollbackAgent: vi.fn().mockResolvedValue({ rolled_back_from: 1, new_version: 3 }),
  },
}));

const AgentVersions = () =>
  import('../pages/AgentDetail').then((m) => m.AgentVersions);

function renderAV(props: Record<string, unknown>) {
  return AgentVersions().then((Comp) =>
    render(
      <MemoryRouter>
        <Comp agentId={1} {...props} />
      </MemoryRouter>,
    ),
  );
}

describe('AgentVersions 状态渲染（I38/I14）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('干净态：显示「发布当前配置」，无灰度/草稿徽章', async () => {
    const { container } = await renderAV({ agentId: 1, dirty: false, ramping: false });
    expect(screen.getByText('发布当前配置')).toBeTruthy();
    expect(container.textContent).not.toContain('有草稿待发布');
    expect(container.textContent).not.toContain('灰度');
  }, 3000);

  it('dirty 态：草稿徽章 + 按钮文案切「发布草稿」', async () => {
    await renderAV({ agentId: 1, dirty: true, ramping: false });
    expect(screen.getByText('有草稿待发布')).toBeTruthy();
    expect(screen.getByText('发布草稿')).toBeTruthy();
  }, 3000);

  it('ramping 态：灰度徽章 + 「全量」按钮', async () => {
    const { container } = await renderAV({ agentId: 1, dirty: false, ramping: true, rampPct: 50 });
    expect(container.textContent).toContain('灰度 50%');
    expect(screen.getByText('全量')).toBeTruthy();
  }, 3000);
});
