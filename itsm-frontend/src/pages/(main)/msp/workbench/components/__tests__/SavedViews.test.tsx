/**
 * SavedViews 组件测试（IP-P2-4a 前端）。
 *
 * 覆盖：
 * - 渲染：视图下拉（默认标记 ★ / 他人分享标记）；
 * - 选择视图 → onApply（父页面负责展开为 URL query，实现复现）；
 * - 保存当前筛选为视图（名称校验 + 载荷 filters/isShared）；
 * - 编辑 / 设为默认 / 删除（仅 owner；非 owner 禁用）。
 */
import React from 'react';
import { render, screen, waitFor } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';
import SavedViews, { type SavedViewsProps } from '../SavedViews';
import type { WorkbenchView } from '@/lib/api/msp-workbench-api';

jest.setTimeout(120000);

const ownerView: WorkbenchView = {
  id: 11,
  name: 'Acme 紧急',
  filters: { customerTenantIds: [2], status: 'open', sort: 'sla' },
  isShared: true,
  isDefault: true,
  isOwner: true,
  ownerUserId: 7,
  createdAt: '2026-09-30T00:00:00Z',
  updatedAt: '2026-09-30T00:00:00Z',
};

const sharedView: WorkbenchView = {
  ...ownerView,
  id: 12,
  name: 'Beta 视图',
  isDefault: false,
  isShared: true,
  isOwner: false,
  ownerUserId: 8,
};

const currentFilters: SavedViewsProps['currentFilters'] = {
  customerTenantIds: [2],
  status: 'open',
  sort: 'sla',
};

function setup(overrides: Partial<SavedViewsProps> = {}) {
  const props: SavedViewsProps = {
    views: [ownerView, sharedView],
    activeViewId: 11,
    currentFilters,
    onApply: jest.fn(),
    onClear: jest.fn(),
    onCreate: jest.fn().mockResolvedValue(undefined),
    onUpdate: jest.fn().mockResolvedValue(undefined),
    onDelete: jest.fn().mockResolvedValue(undefined),
    onSetDefault: jest.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  render(<SavedViews {...props} />);
  return props;
}

describe('SavedViews', () => {
  it('渲染默认标记与分享标记，选择后回调 onApply', async () => {
    const props = setup();
    const selector = screen.getByTestId('workbench-view-select');
    expect(selector).toBeInTheDocument();

    await userEvent.click(selector);
    // 默认视图带 ★（选中值回显与下拉选项均含该文本）；他人分享带 （分享） 后缀
    expect((await screen.findAllByText('★ Acme 紧急')).length).toBeGreaterThan(0);
    const sharedOption = await screen.findByText('Beta 视图（分享）');
    await userEvent.click(sharedOption);

    await waitFor(() => expect(props.onApply).toHaveBeenCalledTimes(1));
    expect((props.onApply as jest.Mock).mock.calls[0][0].id).toBe(12);
  });

  it('保存当前筛选为视图：载荷含当前 filters 与分享开关', async () => {
    const props = setup();
    await userEvent.click(screen.getByTestId('workbench-view-menu'));
    await userEvent.click(await screen.findByText('保存当前筛选为视图'));

    const input = await screen.findByTestId('workbench-view-name');
    await userEvent.type(input, '我的视图');
    await userEvent.click(screen.getByTestId('workbench-view-shared'));
    // antd 双汉字按钮自动插入空格（保 存），用正则匹配可访问名。
    await userEvent.click(screen.getByRole('button', { name: /保\s*存/ }));

    await waitFor(() => expect(props.onCreate).toHaveBeenCalledTimes(1));
    expect(props.onCreate).toHaveBeenCalledWith({
      name: '我的视图',
      filters: currentFilters,
      isShared: true,
    });
  });

  it('空名称拒绝提交（本地校验，不触发 onCreate）', async () => {
    const props = setup();
    await userEvent.click(screen.getByTestId('workbench-view-menu'));
    await userEvent.click(await screen.findByText('保存当前筛选为视图'));
    await userEvent.click(screen.getByRole('button', { name: /保\s*存/ }));

    await waitFor(() => expect(screen.getByTestId('workbench-view-name')).toBeInTheDocument());
    expect(props.onCreate).not.toHaveBeenCalled();
  });

  it('owner：编辑回填名称并提交 onUpdate；设为默认回调 onSetDefault', async () => {
    const props = setup();
    await userEvent.click(screen.getByTestId('workbench-view-menu'));
    await userEvent.click(await screen.findByText('编辑视图'));

    const input = await screen.findByTestId('workbench-view-name');
    expect(input).toHaveValue('Acme 紧急');
    await userEvent.clear(input);
    await userEvent.type(input, 'Acme 紧急 v2');
    await userEvent.click(screen.getByRole('button', { name: /保\s*存/ }));

    await waitFor(() => expect(props.onUpdate).toHaveBeenCalledTimes(1));
    expect(props.onUpdate).toHaveBeenCalledWith(11, {
      name: 'Acme 紧急 v2',
      filters: currentFilters,
      isShared: true,
    });

    await userEvent.click(screen.getByTestId('workbench-view-menu'));
    await userEvent.click(await screen.findByText('设为默认视图'));
    await waitFor(() => expect(props.onSetDefault).toHaveBeenCalledWith(11));
  });

  it('owner：删除需二次确认', async () => {
    const props = setup();
    await userEvent.click(screen.getByTestId('workbench-view-menu'));
    await userEvent.click(await screen.findByText('删除视图'));

    expect((await screen.findAllByText(/删除视图「Acme 紧急」？/)).length).toBeGreaterThan(0);
    const confirmButton = screen.getAllByRole('button', { name: /删\s*除/ }).pop();
    await userEvent.click(confirmButton as Element);
    await waitFor(() => expect(props.onDelete).toHaveBeenCalledWith(11));
  });

  it('非 owner 分享视图：编辑/删除/默认均禁用', async () => {
    setup({ activeViewId: 12 });
    await userEvent.click(screen.getByTestId('workbench-view-menu'));

    const editItem = (await screen.findByText('编辑视图')).closest('li');
    const deleteItem = screen.getByText('删除视图').closest('li');
    const defaultItem = screen.getByText('设为默认视图').closest('li');
    expect(editItem).toHaveClass('ant-dropdown-menu-item-disabled');
    expect(deleteItem).toHaveClass('ant-dropdown-menu-item-disabled');
    expect(defaultItem).toHaveClass('ant-dropdown-menu-item-disabled');
  });
});
