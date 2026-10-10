/**
 * ServiceCatalogFilters：创建服务按钮权限门禁回归测试。
 *
 * 背景：此前「新建服务」按钮无条件渲染，onClick=onCreateService；无 service_catalog:write
 * 的用户（如 technician 的 user01）点击后无任何反馈（无弹窗/无 toast/无请求），被报为
 * 「点击创建服务没反应」。现约定：无权限不渲染创建按钮（与空态「请联系服务目录管理员」
 * 及仓库 canManageXxx && <Button/> 惯例同口径）；刷新按钮为只读操作，恒显示。
 */
import React from 'react';
import { fireEvent, render, screen } from '@/lib/test-utils';
import { ServiceCatalogFilters } from '../ServiceCatalogFilters';

const baseProps = {
  onSearch: jest.fn(),
  onCategoryFilterChange: jest.fn(),
  onCITypeFilterChange: jest.fn(),
  onCloudServiceFilterChange: jest.fn(),
  ciTypes: [],
  cloudServices: [],
  onRefresh: jest.fn(),
};

describe('ServiceCatalogFilters · 创建按钮权限门禁', () => {
  it('无创建权限（onCreateService 未提供）：不渲染创建按钮，刷新按钮仍可用', () => {
    render(<ServiceCatalogFilters {...baseProps} />);

    expect(screen.queryByRole('button', { name: '新建服务' })).toBeNull();
    expect(screen.getByRole('button', { name: '刷新服务目录' })).toBeInTheDocument();
  });

  it('有创建权限：渲染创建按钮并触发回调', () => {
    const onCreateService = jest.fn();
    render(<ServiceCatalogFilters {...baseProps} onCreateService={onCreateService} />);

    const createBtn = screen.getByRole('button', { name: '新建服务' });
    fireEvent.click(createBtn);
    expect(onCreateService).toHaveBeenCalledTimes(1);
  });
});
