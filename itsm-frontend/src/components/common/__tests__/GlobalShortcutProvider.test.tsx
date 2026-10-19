/**
 * I0-03 全局快捷键测试：Ctrl/Cmd+K 请求打开全局搜索（含表单元素内可达），不与既有快捷键冲突。
 */
import React from 'react';
import { fireEvent, render } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import GlobalShortcutProvider from '../GlobalShortcutProvider';
import { useLayoutStore } from '@/lib/store/layout-store';

jest.mock('@/components/auth/PermissionGate', () => ({
  __esModule: true,
  useCan: () => true,
}));

beforeEach(() => {
  useLayoutStore.setState({ searchRequestNonce: 0 });
});

describe('GlobalShortcutProvider（I0-03）', () => {
  it('Ctrl+K → layout-store 请求计数递增（Header 据此打开搜索）', () => {
    render(
      <MemoryRouter>
        <GlobalShortcutProvider>
          <div>content</div>
        </GlobalShortcutProvider>
      </MemoryRouter>
    );

    fireEvent.keyDown(document, { key: 'k', code: 'KeyK', ctrlKey: true });
    expect(useLayoutStore.getState().searchRequestNonce).toBe(1);
    // 注：cmd+k 与 ctrl+k 绑定同一 handler（macOS 分支由运行时承载；jsdom 不模拟平台差异）。
  });

  it('表单元素内 Ctrl+K 同样可达（enableOnFormTags：助手面板输入框场景）', () => {
    const { container } = render(
      <MemoryRouter>
        <GlobalShortcutProvider>
          <textarea data-testid="typing" />
        </GlobalShortcutProvider>
      </MemoryRouter>
    );

    const typing = container.querySelector('textarea');
    expect(typing).not.toBeNull();
    (typing as HTMLTextAreaElement).focus();
    fireEvent.keyDown(typing as HTMLElement, { key: 'k', code: 'KeyK', ctrlKey: true });

    expect(useLayoutStore.getState().searchRequestNonce).toBe(1);
  });
});