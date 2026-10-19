/**
 * P2 LLM 动态建议数据源测试：成功/失败/未启用回退、未注册页面零请求、会话级缓存。
 */
import { renderHook, waitFor } from '@testing-library/react';

import { useDynamicSuggestions, clearDynamicSuggestionCache } from '../use-dynamic-suggestions';
import { aiGetDynamicSuggestions } from '@/lib/api/ai-api';
import type { PageAssistantDeclaration } from '../types';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetDynamicSuggestions: jest.fn(),
}));

const mockedGet = aiGetDynamicSuggestions as jest.MockedFunction<typeof aiGetDynamicSuggestions>;

const declaration: PageAssistantDeclaration = {
  pageKey: 'ticket.detail',
  pattern: '/tickets/:id',
  title: '工单详情',
  tasks: [
    { id: 'triage_plan', kind: 'task', label: '分诊建议', prompt: '给出分诊建议', readOnly: true },
  ],
  questions: [],
};

beforeEach(() => {
  clearDynamicSuggestionCache();
  mockedGet.mockReset();
});

describe('useDynamicSuggestions（P2）', () => {
  it('生成成功：返回 LLM 建议并标记 source=llm', async () => {
    mockedGet.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      generated: true,
      cached: false,
      model: 'gpt-4o',
      suggestions: [
        { id: 'dyn:0', kind: 'task', label: '先看 SLA 风险', prompt: '看看这张单的 SLA 风险', readOnly: true, source: 'llm' },
      ],
    });

    const { result } = renderHook(() => useDynamicSuggestions(declaration, 'ticket.detail', '/tickets/42'));

    await waitFor(() => expect(result.current.items).toHaveLength(1));
    expect(result.current.source).toBe('llm');
    expect(result.current.items[0]).toEqual({
      id: 'dyn:0',
      kind: 'task',
      label: '先看 SLA 风险',
      prompt: '看看这张单的 SLA 风险',
    });
    expect(mockedGet).toHaveBeenCalledWith({ pageKey: 'ticket.detail', route: '/tickets/42' });
  });

  it('未生成/未启用：空数组（回退静态基线，不抛错）', async () => {
    mockedGet.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      generated: false,
      cached: false,
      reason: 'disabled',
      suggestions: [],
    });

    const { result } = renderHook(() => useDynamicSuggestions(declaration, 'ticket.detail', '/tickets/42'));
    await waitFor(() => expect(mockedGet).toHaveBeenCalled());
    expect(result.current.items).toEqual([]);
    expect(result.current.source).toBe('none');
  });

  it('请求失败（503/网络）：静默降级为空', async () => {
    mockedGet.mockRejectedValue({ response: { status: 503 } });

    const { result } = renderHook(() => useDynamicSuggestions(declaration, 'ticket.detail', '/tickets/42'));
    await waitFor(() => expect(mockedGet).toHaveBeenCalled());
    expect(result.current.items).toEqual([]);
    expect(result.current.source).toBe('none');
  });

  it('未注册页面（unknown/无声明）：零请求', async () => {
    const { result } = renderHook(() => useDynamicSuggestions(null, 'unknown', '/nowhere'));
    await Promise.resolve();
    expect(result.current.items).toEqual([]);
    expect(mockedGet).not.toHaveBeenCalled();
  });

  it('会话级缓存：同键二次挂载不重复请求', async () => {
    mockedGet.mockResolvedValue({
      pageKey: 'ticket.detail',
      known: true,
      generated: true,
      cached: false,
      model: 'gpt-4o',
      suggestions: [
        { id: 'dyn:0', kind: 'task', label: '建议', prompt: '提示词', readOnly: true, source: 'llm' },
      ],
    });

    const first = renderHook(() => useDynamicSuggestions(declaration, 'ticket.detail', '/tickets/42'));
    await waitFor(() => expect(first.result.current.items).toHaveLength(1));
    first.unmount();

    const second = renderHook(() => useDynamicSuggestions(declaration, 'ticket.detail', '/tickets/42'));
    await waitFor(() => expect(second.result.current.items).toHaveLength(1));
    expect(mockedGet).toHaveBeenCalledTimes(1);
  });
});