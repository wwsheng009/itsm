/**
 * PermissionGate（入口级权限门禁）用例：
 * - 有权限渲染入口；无权限 fail-closed 隐藏（默认）；
 * - mode=disable：保留占位但禁用；
 * - 判定与后端 checkPermissionMatch 同语义（resource:admin 蕴含）。
 */
import React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: jest.fn(),
}));

import { useAuthStore } from '@/lib/store/auth-store';
import { PermissionGate } from '../PermissionGate';

const setPermissions = (permissions: string[] | undefined) => {
  (useAuthStore as unknown as jest.Mock).mockImplementation(
    (selector: (s: { user?: { permissions?: string[] } }) => unknown) =>
      selector({ user: { permissions } })
  );
};

describe('PermissionGate', () => {
  beforeEach(() => jest.clearAllMocks());

  it('有权限时渲染入口', () => {
    setPermissions(['knowledge:write']);
    render(
      <PermissionGate anyOf={['knowledge:write']}>
        <button>新建文章</button>
      </PermissionGate>
    );
    expect(screen.getByText('新建文章')).toBeInTheDocument();
  });

  it('无权限时隐藏入口（fail-closed）', () => {
    setPermissions(['knowledge:read']);
    render(
      <PermissionGate anyOf={['knowledge:write']}>
        <button>新建文章</button>
      </PermissionGate>
    );
    expect(screen.queryByText('新建文章')).toBeNull();
  });

  it('无权限且 mode=disable 时禁用入口', () => {
    setPermissions([]);
    render(
      <PermissionGate anyOf={['knowledge:write']} mode="disable">
        <button>新建文章</button>
      </PermissionGate>
    );
    expect(screen.getByText('新建文章')).toBeDisabled();
  });

  it('resource:admin 蕴含（与后端同语义）', () => {
    setPermissions(['knowledge:admin']);
    render(
      <PermissionGate anyOf={['knowledge:write']}>
        <button>新建文章</button>
      </PermissionGate>
    );
    expect(screen.getByText('新建文章')).toBeInTheDocument();
  });

  it('anyOf 任一命中即放行', () => {
    setPermissions(['ai:write']);
    render(
      <PermissionGate anyOf={['knowledge:write', 'ai:write']}>
        <button>AI 创建</button>
      </PermissionGate>
    );
    expect(screen.getByText('AI 创建')).toBeInTheDocument();
  });
});
