/**
 * 通知中心页面测试：偏好写面门禁（B7 观察③回归）
 * 无 notification:write 时偏好开关必须禁用（只读展示），而非「可点但无保存入口」。
 */

import React from 'react';
import { render, screen, waitFor } from '@/lib/test-utils';

const mockPush = jest.fn();
jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useNavigate: () => mockPush,
}));

// 全局 setup 把 dayjs 换成了 getter 桩，本页的 RangePicker（@rc-component/picker）
// 需要真实 dayjs（setter/插件语义），本文件内恢复真实实现。
jest.mock('dayjs', () => jest.requireActual('dayjs'));

jest.mock('@/lib/api/ticket-notification-api', () => ({
  TicketNotificationApi: {
    getUserNotifications: jest.fn().mockResolvedValue({ notifications: [], total: 0 }),
    getNotificationPreferences: jest.fn().mockResolvedValue({
      // eventType 值为后端事件类型枚举（外部协议值，与页面 EVENT_TYPES 同源），非 API 字段命名。
      preferences: [
        { eventType: 'ticket_created', emailEnabled: true, inAppEnabled: true, smsEnabled: false },
        { eventType: 'ticket_assigned', emailEnabled: false, inAppEnabled: true, smsEnabled: false },
      ],
    }),
    updateNotificationPreferences: jest.fn().mockResolvedValue({ preferences: [] }),
    resetNotificationPreferences: jest.fn().mockResolvedValue({ reset: true }),
  },
  toTicketNotification: (input: unknown) => input,
}));

import { useAuthStore } from '@/lib/store/auth-store';

const baseUser = {
  id: 1,
  username: 'probe',
  email: '',
  name: '',
  tenantId: 1,
  homeTenantId: 1,
  createdAt: '',
  updatedAt: '',
};

function setUserPermissions(permissions: string[]) {
  useAuthStore.setState({ user: { ...baseUser, permissions } } as never);
}

let NotificationsPage: React.FC;

beforeAll(async () => {
  const module = await import('../index');
  NotificationsPage = module.default;
});

describe('NotificationsPage 偏好写面门禁', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  async function openPreferencesTab() {
    render(<NotificationsPage />);
    await waitFor(() => {
      expect(screen.getByText('通知偏好')).toBeInTheDocument();
    });
    // 页头标题与 Tab 同名，取 Tab 项（最后一个「通知偏好」为 Tab label）
    const tabTargets = screen.getAllByText('通知偏好');
    tabTargets[tabTargets.length - 1].click();
    await waitFor(() => {
      expect(screen.getByText('事件类型')).toBeInTheDocument();
    });
  }

  it('无 notification:write 时偏好开关禁用且无保存/重置入口', async () => {
    setUserPermissions(['notification:read']);
    await openPreferencesTab();

    // 只读提示可见
    expect(screen.getByText(/只读展示/)).toBeInTheDocument();

    // 开关全部禁用（antd Switch → role=switch）
    const switches = screen.getAllByRole('switch');
    expect(switches.length).toBeGreaterThan(0);
    switches.forEach(sw => expect(sw).toBeDisabled());

    // 写入口不渲染
    expect(screen.queryByText('保存设置')).not.toBeInTheDocument();
    expect(screen.queryByText('重置为默认')).not.toBeInTheDocument();
  });

  it('有 notification:write 时偏好开关可用且保存/重置入口存在', async () => {
    setUserPermissions(['notification:read', 'notification:write']);
    await openPreferencesTab();

    expect(screen.queryByText(/只读展示/)).not.toBeInTheDocument();

    const switches = screen.getAllByRole('switch');
    expect(switches.length).toBeGreaterThan(0);
    switches.forEach(sw => expect(sw).toBeEnabled());

    expect(screen.getByText('保存设置')).toBeInTheDocument();
    expect(screen.getByText('重置为默认')).toBeInTheDocument();
  });
});
