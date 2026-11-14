import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

// mock api（Usage 页数据拉取）
vi.mock('../api', () => ({
  api: {
    usage: vi.fn().mockResolvedValue({
      window: '24h',
      requests: 42,
      prompt_tokens: 1200,
      completion_tokens: 3400,
      cost_usd: 0,
      by_model: [{ model: 'qwen3.5', requests: 42, tokens: 4600 }],
      by_agent: [{ agent_id: 1, tokens: 4600 }],
      rate_limit: { enabled: false, per_minute: 0, rejected_total: 0 },
    }),
    health: vi.fn().mockResolvedValue({ status: 'ok', db: true, scheduler: true, llm: { mode: 'ollama', model: 'qwen3.5' } }),
  },
}));

const Usage = () => import('../pages/Usage').then((m) => m.default);

describe('Usage 页渲染', () => {
  it('统计卡与成本字段渲染（F10 本地零成本）', async () => {
    const Comp = await Usage();
    const { container } = render(
      <MemoryRouter>
        <Comp />
      </MemoryRouter>,
    );
    // 轮询等待异步数据渲染（useApi 收敛）
    let shown = false;
    for (let i = 0; i < 20; i++) {
      await new Promise((r) => setTimeout(r, 50));
      if (container.textContent.includes('4.6k')) {
        shown = true;
        break;
      }
    }
    expect(shown).toBe(true); // tokens 合计
    expect(container.textContent).toContain('$0.0000');
    expect(screen.getByText('刷新')).toBeTruthy();
  }, 3000);
});
