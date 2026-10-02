/**
 * FLOW-邀请：邀请 → 落地页激活 → 首登（IP-P1-4c e2e 收口）。
 *
 * 链路（真实后端 + 前端 dev server）：
 *   1. admin 登录 → `POST /api/v1/users/invitations` 创建邀请（SMTP 未配置时 inviteUrl 直返）；
 *   2. 打开后端生成的**路径式**链接 `/invite/<token>`（验证路由契约，不在测试里改写形态）；
 *   3. 落地页回显（邮箱脱敏）→ 设置密码 → 激活成功；
 *   4. 新账号首登（API 登录取得 accessToken），验证建号与凭据生效。
 *
 * 标签 `@multi-tenant` 以便 CI `--grep @multi-tenant` 纳入隔离回归。
 * 依赖环境：后端可用；邀请服务未启用时用例显式 skip（不误报失败）。
 */
import { test, expect } from '@playwright/test';

const PASSWORD = 'Invite!E2e2026';
const apiURL = process.env.VITE_API_URL || 'http://localhost';
const ADMIN_USERNAME = process.env.E2E_ADMIN_USERNAME || 'admin';
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD || 'AdminProd2026!';

interface RoleLite {
  id: number;
  code?: string;
  name?: string;
}

/** 兼容 roles 接口的不同包装（数组 / list / items / roles）。 */
function pickInvitableRole(payload: unknown): RoleLite | undefined {
  const data = (payload as { data?: unknown } | undefined)?.data;
  const obj = data as Record<string, unknown> | undefined;
  const list = Array.isArray(data) ? data : obj?.list ?? obj?.items ?? obj?.roles;
  if (!Array.isArray(list)) return undefined;
  const roles = list as RoleLite[];
  return roles.find(role => role.code && !['super_admin', 'sysadmin'].includes(role.code)) ?? roles[0];
}

test.describe('@multi-tenant 邀请 → 落地 → 首登（IP-P1-4c）', () => {
  test('邀请创建 → 路径式落地页设置密码 → 首登成功', async ({
    page,
    request,
  }) => {
    // 管理端会话：兼容 Cookie 会话（当前 8090 后端：data.user + Set-Cookie，无 accessToken）
    // 与 Bearer token 两种形态，统一走 page.request（共享浏览器上下文 cookie）。
    const api = page.request;
    const adminLogin = await api.post(`${apiURL}/api/v1/auth/login`, {
      data: { username: ADMIN_USERNAME, password: ADMIN_PASSWORD },
    });
    if (adminLogin.status() !== 200) {
      test.skip(
        true,
        `admin 登录失败（${adminLogin.status()}）；本机可用 E2E_ADMIN_PASSWORD 覆盖（当前 seeder 口令见 itsm-backend/.env）`
      );
    }
    const adminLoginJson = await adminLogin.json();
    const accessToken: string | undefined =
      adminLoginJson?.data?.accessToken ?? adminLoginJson?.data?.access_token;
    const authHeaders = accessToken ? { Authorization: `Bearer ${accessToken}` } : {};

    const rolesResp = await api.get(`${apiURL}/api/v1/roles`, { headers: authHeaders });
    expect(rolesResp.status()).toBe(200);
    const role = pickInvitableRole(await rolesResp.json());
    test.skip(!role, 'roles 列表为空，无可邀请角色');

    const stamp = Date.now();
    const username = `e2e.invite.${stamp}`;
    const email = `${username}@example.com`;

    const createdResp = await api.post(`${apiURL}/api/v1/users/invitations`, {
      headers: authHeaders,
      data: { email, roleId: role!.id },
    });
    const createdStatus = createdResp.status();
    const createdText = await createdResp.text();
    // 运行中的后端可能早于 IP-P1-4b（无邀请路由）→ 显式 skip，不假红。
    test.skip(
      [404, 501, 503].includes(createdStatus),
      `邀请路由不可用（HTTP ${createdStatus}）：后端构建早于 IP-P1-4b 或邀请服务未装配`
    );
    expect(createdStatus, createdText).toBe(200);
    const created = JSON.parse(createdText) as {
      code: number;
      data: { status: string; inviteUrl: string; emailSent: boolean };
    };

    expect(created.code).toBe(0);
    expect(created.data.status).toBe('pending');
    const inviteUrl = created.data.inviteUrl;
    const token = inviteUrl.substring(inviteUrl.lastIndexOf('/') + 1);
    expect(token.length).toBeGreaterThan(0);

    // 2) 路径式落地页（后端 inviteUrl 原样形态）
    await page.goto(`/invite/${token}`);
    await expect(page.getByText(/邀请邮箱/)).toBeVisible({ timeout: 15000 });
    await expect(page.getByText(email.replace(/(.{1}).*@/, '$1***@'))).toBeVisible();

    await page.getByLabel('姓名（选填）').fill('E2E Invitee');
    await page.getByLabel('设置密码').fill(PASSWORD);
    await page.getByLabel('确认密码').fill(PASSWORD);
    await page.getByRole('button', { name: '设置密码并激活账号' }).click();
    await expect(page.getByText('账号已激活')).toBeVisible({ timeout: 15000 });

    // 3) 首登：新账号可用
    const login = await request.post(`${apiURL}/api/v1/auth/login`, {
      data: { username, password: PASSWORD },
    });
    const loginText = await login.text();
    expect(login.status(), loginText).toBe(200);
    const loginJson = JSON.parse(loginText) as {
      data?: { accessToken?: string; access_token?: string; user?: { username?: string } };
    };
    expect(loginJson.data?.user?.username).toBe(username);
  });
});
