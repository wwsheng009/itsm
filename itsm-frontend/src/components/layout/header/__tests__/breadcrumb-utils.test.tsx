/**
 * 回归（2026-10-04 UI 验收实锤）：/auth/menus 某分组为 null 时面包屑不得崩页。
 *
 * 背景：provider 用户（mspadmin）的 `/api/v1/auth/menus` 返回 `admin: null`，
 * `useBuildBreadcrumb → collectMenuLabels` 直接 `for...of null` 抛 TypeError，
 * Header 触发整页 ErrorBoundary——provider 用户无法进入工作台（全站白屏）。
 *
 * 契约：后端已修正为恒返回 `[]`（service/menu_service.go buildMenuTree），
 * 前端此处锁定「任一组缺失/null 仍可渲染」的双保险。
 */
import React from 'react';
import { render, screen } from '@/lib/test-utils';

jest.mock('@/lib/hooks/useUserMenusQuery', () => ({
  useUserMenusQuery: jest.fn(),
}));

import { useUserMenusQuery } from '@/lib/hooks/useUserMenusQuery';
import type { MenuTreeResponse } from '@/lib/api/menu-api';
import { useBuildBreadcrumb } from '../breadcrumb-utils';

const mockedUseUserMenusQuery = useUserMenusQuery as jest.MockedFunction<typeof useUserMenusQuery>;

function mockMenus(data: unknown) {
  mockedUseUserMenusQuery.mockReturnValue({
    data,
  } as unknown as ReturnType<typeof useUserMenusQuery>);
}

function Probe({ path }: { path: string }) {
  const items = useBuildBreadcrumb(path);
  return <div data-testid="crumb">{items.map(item => String(item.title)).join(' / ')}</div>;
}

describe('useBuildBreadcrumb 菜单形状防御', () => {
  beforeEach(() => {
    mockedUseUserMenusQuery.mockReset();
  });

  it('admin 为 null 时使用 main 标签且不抛错（provider 用户场景）', () => {
    mockMenus({
      main: [
        {
          id: 1,
          name: '客户管理',
          path: '/msp',
          children: [
            { id: 2, name: '跨客户工作台', path: '/msp/workbench', children: [] },
          ],
        },
      ],
      admin: null,
    } as unknown as MenuTreeResponse);

    render(<Probe path="/msp/workbench" />);

    const crumb = screen.getByTestId('crumb');
    expect(crumb).toHaveTextContent('首页');
    expect(crumb).toHaveTextContent('跨客户工作台');
  });

  it('main/admin 均为 null 时回退路由段语义映射', () => {
    mockMenus({ main: null, admin: null } as unknown as MenuTreeResponse);

    render(<Probe path="/incidents/12/edit" />);

    const crumb = screen.getByTestId('crumb');
    expect(crumb).toHaveTextContent('首页');
    expect(crumb).toHaveTextContent('事件管理');
    expect(crumb).toHaveTextContent('详情 #12');
    expect(crumb).toHaveTextContent('编辑');
  });
});
