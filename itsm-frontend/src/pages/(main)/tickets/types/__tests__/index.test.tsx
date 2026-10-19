/**
 * /tickets/types 预设入口权限门控回归（2026-10-06）：
 * - 无 ticket_type:install_preset 的角色不渲染「从预设安装」入口（点击必 403 的按钮不可见）；
 * - 有权限时入口可见，安装调用 TicketTypeApi.installPreset（服务端 RequirePermission 仍是最终闸门）。
 */
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { App } from 'antd';

import TicketTypesPage from '../index';
import { TicketTypeApi } from '@/lib/api/ticketTypeApi';

let mockHasPermission: (...args: unknown[]) => boolean = () => false;

jest.mock('@/lib/hooks/use-permissions', () => ({
  usePermissions: () => ({
    hasPermission: (...args: unknown[]) => mockHasPermission(...args),
  }),
}));

jest.mock('@/components/business/TicketTypeFormModal', () => ({
  TicketTypeFormModal: () => null,
}));

jest.mock('@/lib/api/ticketTypeApi', () => ({
  TicketTypeApi: {
    list: jest.fn(),
    listPresets: jest.fn(),
    installPreset: jest.fn(),
  },
}));

const mockedApi = TicketTypeApi as jest.Mocked<typeof TicketTypeApi>;

const renderPage = () => render(<App><TicketTypesPage /></App>);

beforeEach(() => {
  jest.clearAllMocks();
  mockHasPermission = () => false;
  mockedApi.list.mockResolvedValue({ types: [], total: 0, page: 1, pageSize: 100 } as never);
  mockedApi.listPresets.mockResolvedValue([
    {
      id: 'general-incident',
      name: '通用事件',
      category: 'ITSM',
      description: '示例预设',
      definition: { code: 'general_incident', name: '通用事件' },
    },
  ] as never);
  mockedApi.installPreset.mockResolvedValue({ id: 1, code: 'general_incident', name: '通用事件' } as never);
});

describe('TicketTypesPage 预设入口权限门控', () => {
  it('无 install_preset 权限（持有 manage）：不渲染「从预设安装」，保留「新建类型」', async () => {
    mockHasPermission = (...args) => args[0] === 'ticket_type' && args[1] === 'manage';
    renderPage();
    await waitFor(() => expect(mockedApi.list).toHaveBeenCalled());

    expect(screen.queryByRole('button', { name: '从预设安装' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /新建类型/ })).toBeInTheDocument();
  });

  it('有 install_preset 权限：入口可见，安装调用 installPreset', async () => {
    mockHasPermission = (...args) => args[0] === 'ticket_type' && args[1] === 'install_preset';
    const user = userEvent.setup();
    renderPage();

    const entry = await screen.findByRole('button', { name: '从预设安装' });
    await user.click(entry);

    const installBtn = await screen.findByRole('button', { name: '安装' });
    await user.click(installBtn);

    await waitFor(() => expect(mockedApi.installPreset).toHaveBeenCalledWith('general-incident'));
  });

  it('已安装的预设显示「已安装」且不可点击（避免重复安装 409）', async () => {
    mockHasPermission = (...args) => args[0] === 'ticket_type' && args[1] === 'install_preset';
    mockedApi.list.mockResolvedValue({
      types: [{ id: 1, code: 'general_incident', name: '通用事件' }],
      total: 1,
      page: 1,
      pageSize: 100,
    } as never);
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole('button', { name: '从预设安装' }));

    const installedBtn = await screen.findByRole('button', { name: '已安装' });
    expect(installedBtn).toBeDisabled();
    await user.click(installedBtn);
    expect(mockedApi.installPreset).not.toHaveBeenCalled();
  });
});
