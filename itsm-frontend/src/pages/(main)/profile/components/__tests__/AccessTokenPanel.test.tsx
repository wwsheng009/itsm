import { configure, fireEvent, render, screen, waitFor } from '@/lib/test-utils';
import { AccessTokenPanel } from '../AccessTokenPanel';
import { SecurityEventsPanel } from '../SecurityEventsPanel';
import type { PersonalAccessToken, UsagePage } from '@/lib/api/personal-access-token-api';

/**
 * AccessTokenPanel / SecurityEventsPanel 测试（T9）：
 * 列表渲染 / 创建一次性明文（保存闸门 + 关闭清除）/ 吊销 → 重取 / 明文不入缓存。
 * API 依赖注入（仿 InvitationManagementModal 测试），不 mock 网络层。
 */

jest.setTimeout(60000);
configure({ asyncUtilTimeout: 30000 });

const activeToken = (over: Partial<PersonalAccessToken> = {}): PersonalAccessToken => ({
  id: 1,
  name: 'CI 发布脚本',
  tokenPrefix: 'itsm_pat_5Yk8mQ2v',
  maskedToken: 'itsm_pat_5Yk8mQ2v****',
  status: 'active',
  expiresAt: new Date(Date.now() + 30 * 86400000).toISOString(),
  useCount: 3,
  lastUsedAt: new Date().toISOString(),
  lastUsedIp: '10.0.0.8',
  createdAt: new Date().toISOString(),
  ...over,
});

const makeApi = (tokens: PersonalAccessToken[] = [activeToken()]) => ({
  listTokens: jest.fn().mockResolvedValue(tokens),
  createToken: jest.fn().mockResolvedValue({
    token: activeToken({ id: 2, name: '新令牌' }),
    plaintextToken: 'itsm_pat_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQIDBA=',
  }),
  rotateToken: jest.fn(),
  revokeToken: jest.fn().mockResolvedValue(activeToken({ status: 'revoked' as const })),
  revokeAllTokens: jest.fn().mockResolvedValue({ revokedCount: 2 }),
  listUsages: jest.fn().mockResolvedValue({
    items: [
      { id: 9, at: new Date().toISOString(), action: 'auth.token_used', method: 'POST', path: '/api/v1/tickets', statusCode: 200, ip: '10.0.0.8', source: 'pat' },
    ],
    total: 1,
    page: 1,
    pageSize: 20,
    summary: { useCount: 3, lastUsedAt: new Date().toISOString(), lastUsedIp: '10.0.0.8' },
  } as UsagePage),
});

describe('AccessTokenPanel', () => {
  it('渲染令牌列表（掩码，无明文）', async () => {
    const api = makeApi();
    render(<AccessTokenPanel api={api} />);
    await waitFor(() => expect(api.listTokens).toHaveBeenCalled());
    expect(await screen.findByText('CI 发布脚本')).toBeInTheDocument();
    expect(screen.getByText('itsm_pat_5Yk8mQ2v****')).toBeInTheDocument();
  });

  it('创建令牌 → 一次性明文 →「我已保存」闸门 → 关闭后 state 清除', async () => {
    const plaintext = 'itsm_pat_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQIDBA=';
    const api = makeApi();
    render(<AccessTokenPanel api={api} />);

    fireEvent.click(await screen.findByTestId('pat-create'));
    fireEvent.change(await screen.findByTestId('pat-name-input'), { target: { value: '新令牌' } });
    // 模态框内确认按钮（页头也有「创建令牌」入口按钮，取弹窗 footer 的 OK）
    const okButtons = await screen.findAllByRole('button', { name: /创建令牌|Create Token/ });
    fireEvent.click(okButtons[okButtons.length - 1]);

    const shown = await screen.findByTestId('pat-plaintext');
    await waitFor(() => expect(shown.textContent).toContain(plaintext));
    expect(api.createToken).toHaveBeenCalledWith(expect.objectContaining({ name: '新令牌' }));

    // 关闭按钮在勾选前禁用（防误关丢失）
    expect(screen.getByTestId('pat-reveal-done')).toBeDisabled();
    fireEvent.click(screen.getByTestId('pat-saved-check'));
    expect(screen.getByTestId('pat-reveal-done')).toBeEnabled();
    fireEvent.click(screen.getByTestId('pat-reveal-done'));

    // 明文从 DOM 移除（state 清除，AC-41 前端卫生）
    await waitFor(() => expect(screen.queryByTestId('pat-plaintext')).not.toBeInTheDocument());
    expect(document.body.innerHTML).not.toContain(plaintext);
  });

  it('吊销（Popconfirm）→ 调用 revokeToken 并重取列表', async () => {
    const api = makeApi();
    render(<AccessTokenPanel api={api} />);
    fireEvent.click(await screen.findByTestId('pat-revoke-1'));
    fireEvent.click(await screen.findByRole('button', { name: /确定吊销|Revoke$/ }));
    await waitFor(() => expect(api.revokeToken).toHaveBeenCalledWith(1));
    await waitFor(() => expect(api.listTokens).toHaveBeenCalledTimes(2));
  });

  it('空态展示', async () => {
    const api = makeApi([]);
    render(<AccessTokenPanel api={api} />);
    expect(await screen.findByText(/暂无访问令牌|No access tokens/)).toBeInTheDocument();
  });
});

describe('SecurityEventsPanel', () => {
  it('渲染本人安全事件（登录 + 令牌来源）', async () => {
    const api = {
      listSecurityEvents: jest.fn().mockResolvedValue({
        items: [
          { id: 1, at: new Date().toISOString(), action: 'auth.login', source: 'login', statusCode: 200, ip: '10.0.0.1' },
          { id: 2, at: new Date().toISOString(), action: 'auth.token_used', source: 'pat', statusCode: 200, ip: '10.0.0.8', tokenId: 42 },
        ],
        total: 2,
        page: 1,
        pageSize: 10,
      }),
    };
    render(<SecurityEventsPanel api={api} />);
    await waitFor(() => expect(api.listSecurityEvents).toHaveBeenCalledWith({ page: 1, pageSize: 10 }));
    // 「登录」出现在 action 列（auth.login → 登录）与来源 Tag 两处
    expect(await screen.findAllByText('登录')).toHaveLength(2);
    expect(screen.getByText('#42')).toBeInTheDocument();
  });
});
