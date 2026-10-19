/**
 * D1-02 L2 页面数据注册 hook：挂载注册 / 卸载清理 / 数据更新 / 后挂载页面优先。
 */
import { renderHook } from '@testing-library/react';

import { useRegisterAssistantPageData } from '../use-register-assistant-page-data';
import { useAssistantContextStore } from '@/lib/store/assistant-context-store';

beforeEach(() => {
  useAssistantContextStore.setState({ pageData: null, owner: null });
});

describe('useRegisterAssistantPageData（D1-02 L2 运行时数据）', () => {
  it('挂载注册、卸载清理', () => {
    const { unmount } = renderHook(() =>
      useRegisterAssistantPageData({ targetType: 'ticket', targetId: 42, summary: '无法登录' })
    );

    expect(useAssistantContextStore.getState().pageData).toEqual({
      targetType: 'ticket',
      targetId: 42,
      summary: '无法登录',
    });

    unmount();
    expect(useAssistantContextStore.getState().pageData).toBeNull();
  });

  it('数据更新（加载完成）重新注册；无效数据不注册', () => {
    type PageDataArg = Parameters<typeof useRegisterAssistantPageData>[0];
    const { rerender } = renderHook(
      (props: { data: PageDataArg }) => useRegisterAssistantPageData(props.data),
      { initialProps: { data: null as PageDataArg } }
    );
    expect(useAssistantContextStore.getState().pageData).toBeNull();

    rerender({ data: { targetType: 'incident', targetId: 7, summary: '登录故障' } });
    expect(useAssistantContextStore.getState().pageData).toEqual({
      targetType: 'incident',
      targetId: 7,
      summary: '登录故障',
    });

    // id 非法（<=0）视为未注册。
    rerender({ data: { targetType: 'incident', targetId: 0 } });
    expect(useAssistantContextStore.getState().pageData).toBeNull();
  });

  it('后挂载页面优先：旧页面卸载不清掉新页面数据', () => {
    const first = renderHook(() =>
      useRegisterAssistantPageData({ targetType: 'ticket', targetId: 1 })
    );
    const second = renderHook(() =>
      useRegisterAssistantPageData({ targetType: 'ticket', targetId: 2 })
    );
    expect(useAssistantContextStore.getState().pageData?.targetId).toBe(2);

    first.unmount();
    expect(useAssistantContextStore.getState().pageData?.targetId).toBe(2);

    second.unmount();
    expect(useAssistantContextStore.getState().pageData).toBeNull();
  });
});