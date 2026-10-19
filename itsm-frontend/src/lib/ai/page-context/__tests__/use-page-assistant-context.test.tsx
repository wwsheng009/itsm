/**
 * D0-03 页面上下文解析测试：匹配优先级（具体静态路由先于动态）、参数解析、
 * 未注册路由 fallback、路由联动写入 store.lastRoute。
 */
import React from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import { resolvePageAssistantContext, usePageAssistantContext } from '../use-page-assistant-context';
import { useAssistantStore } from '@/lib/store/assistant-store';

// jest.setup.js 全局把 useLocation 固定为 '/'（为不挂 Router 的组件兜底）；
// 本测试需要真实路由上下文，按 setup 注释约定还原真实 react-router 实现。
jest.mock('react-router', () => jest.requireActual('react-router'));

describe('resolvePageAssistantContext（注册表匹配）', () => {
  it('具体静态路由优先于同前缀动态路由', () => {
    expect(resolvePageAssistantContext('/tickets/create').pageKey).toBe('ticket.create');
    expect(resolvePageAssistantContext('/tickets/ai-create').pageKey).toBe('ticket.aiCreate');
    expect(resolvePageAssistantContext('/tickets/42').pageKey).toBe('ticket.detail');
    expect(resolvePageAssistantContext('/tickets').pageKey).toBe('ticket.list');

    expect(resolvePageAssistantContext('/incidents/create').pageKey).toBe('incident.create');
    expect(resolvePageAssistantContext('/incidents/7').pageKey).toBe('incident.detail');

    expect(resolvePageAssistantContext('/changes/new').pageKey).toBe('change.create');
    expect(resolvePageAssistantContext('/changes/9').pageKey).toBe('change.detail');

    expect(resolvePageAssistantContext('/knowledge/articles/new').pageKey).toBe('knowledge.new');
    expect(resolvePageAssistantContext('/knowledge').pageKey).toBe('knowledge.list');

    expect(resolvePageAssistantContext('/cmdb/cis/create').pageKey).toBe('ci.create');
    expect(resolvePageAssistantContext('/cmdb/cis/99').pageKey).toBe('ci.detail');
    expect(resolvePageAssistantContext('/cmdb/cis').pageKey).toBe('ci.list');
  });

  it('解析动态参数并保留入口语义', () => {
    const detail = resolvePageAssistantContext('/cmdb/cis/99');
    expect(detail.params.id).toBe('99');
    expect(detail.declaration?.entrypoint).toBe('ci_detail');
    expect(detail.declaration?.scene).toBe('s4-change-impact-analyst');

    const ticket = resolvePageAssistantContext('/tickets/42');
    expect(ticket.params.id).toBe('42');
    expect(ticket.declaration?.entrypoint).toBe('ticket_detail');
  });

  it('未注册路由返回 unknown 且不误匹配（declaration=null）', () => {
    const resolution = resolvePageAssistantContext('/no/such/page');
    expect(resolution.declaration).toBeNull();
    expect(resolution.pageKey).toBe('unknown');
    expect(resolution.params).toEqual({});
  });
});

describe('usePageAssistantContext（路由联动）', () => {
  beforeEach(() => {
    window.localStorage.clear();
    useAssistantStore.setState({ lastRoute: undefined });
  });

  it('随路由解析声明并写入 lastRoute', async () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <MemoryRouter initialEntries={['/incidents/12']}>{children}</MemoryRouter>
    );
    const { result } = renderHook(() => usePageAssistantContext(), { wrapper });

    expect(result.current.pageKey).toBe('incident.detail');
    expect(result.current.route).toBe('/incidents/12');
    await waitFor(() => expect(useAssistantStore.getState().lastRoute).toBe('/incidents/12'));
  });
});