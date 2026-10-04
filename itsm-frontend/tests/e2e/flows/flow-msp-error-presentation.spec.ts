/**
 * FLOW-多租户错误态 UI 呈现：把「R6 重复分配 409」「Q 超配额 422」从 HTTP 判定
 * 平移到浏览器操作层——验证错误在页面上**可见、可读、不崩页、表单可继续使用**。
 *
 * 场景：
 *   E1 重复分配 409：/msp/management 新建已存在的 (员工, 客户) 分配 → 弹窗内提示
 *      「该员工已分配至该客户」（后端 D-2 收口：409 + MSP_ALLOCATION_EXISTS），
 *      提交按钮不置灰、弹窗不关闭、页面可继续操作。
 *   E2 超配额 422：把客户 A 的 maxTicketsPerMonth 设为本月已用量（used >= limit），
 *      客户管理员在 /tickets/create 提交 → 后端 422；页面提示用户可读的中文文案
 *      （英文技术文案由 mapTicketCreateError 收口），表单保留且可继续编辑。
 *
 * 环境：
 *   - 后端需为 `saas_msp` 模式（私有/缺省模式 MSP 路由 404 → 显式 skip，不假红）；
 *   - 凭证可用 E2E_* 环境变量覆盖（与验收脚本默认一致）；
 *   - E2 结束（含失败路径）恢复配额为 0（不限），避免污染后续用例/验收。
 *
 * 标签 `@multi-tenant`：可被 CI `--grep @multi-tenant` 纳入多租户回归。
 *
 * 并行注意：E2 会**临时收紧客户A的月建单配额**（结束后恢复）；同一租户上建样本的
 * 用例（如 flow-msp-workbench-ui）不得与其并行执行——`--workers=1` 串行即可。
 */
import { test, expect, request as pwRequest, type APIRequestContext, type Page } from '@playwright/test';

const APP_URL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:3000';
const ADMIN_USER = process.env.E2E_ADMIN_USERNAME || 'admin';
const ADMIN_PASS = process.env.E2E_ADMIN_PASSWORD || 'passw0rd';
const MSP_USER = process.env.E2E_MSP_USERNAME || 'mspadmin';
const MSP_PASS = process.env.E2E_MSP_PASSWORD || 'Msp@2026Staff!';
const CUSTA_USER = process.env.E2E_CUSTA_USERNAME || 'custa_admin';
const CUSTA_PASS = process.env.E2E_CUSTA_PASSWORD || 'Cust@2026User!';
const CUSTOMER_A_CODE = process.env.E2E_MSP_CUSTOMER_A_CODE || 'MSPCUSTA';

interface LoginBody {
  code: number;
  data: { user: { id: number; username: string } };
}

interface CustomerItem {
  id: number;
  code: string;
  name: string;
}

/**
 * 冷启动预热：Playwright webServer 拉起 vite 后，代理的首个请求可能仍处于
 * 依赖预构建窗口而悬挂；显式放大超时并预热一次，避免首请求误报超时。
 */
async function warmup(ctx: APIRequestContext): Promise<void> {
  await ctx
    .get('/api/v1/health', { timeout: 120_000 })
    .catch(() => undefined);
}

/** 登录（Cookie 会话）；失败返回 null，由调用方决定 skip 或失败。 */
async function tryLogin(
  ctx: APIRequestContext,
  username: string,
  password: string
): Promise<LoginBody | null> {
  const resp = await ctx.post('/api/v1/auth/login', {
    data: { username, password },
    timeout: 120_000,
  });
  if (resp.status() !== 200) return null;
  return (await resp.json()) as LoginBody;
}

/** 取 Cookie 里的 access_token 转 Bearer（写操作免 CSRF，策略与验收脚本一致）。 */
async function bearerOf(ctx: APIRequestContext): Promise<string> {
  const state = await ctx.storageState();
  return state.cookies.find(cookie => cookie.name === 'access_token')?.value ?? '';
}

/** 浏览器上下文登录（经 vite 代理，Set-Cookie 落入浏览器 CookieJar）。 */
async function browserLogin(page: Page, username: string, password: string): Promise<void> {
  const resp = await page.request.post('/api/v1/auth/login', { data: { username, password } });
  expect(resp.status(), `浏览器会话登录失败：${username} ${await resp.text()}`).toBe(200);
}

async function findCustomerA(
  ctx: APIRequestContext,
  headers: Record<string, string>
): Promise<CustomerItem | null> {
  const resp = await ctx.get('/api/v1/msp/customers', { headers });
  if (resp.status() === 404) return null; // 非 saas_msp 模式
  expect(resp.status(), await resp.text()).toBe(200);
  const body = (await resp.json()) as { data: { customers: CustomerItem[] } };
  return body.data.customers.find(c => c.code === CUSTOMER_A_CODE) ?? null;
}

test.describe('@multi-tenant MSP 错误态 UI 呈现', () => {
  test('E1 重复分配 409 → 弹窗内错误提示且页面可用（R6 的 UI 呈现）', async ({ page }) => {
    test.setTimeout(240_000);

    const api = await pwRequest.newContext({ baseURL: APP_URL, timeout: 120_000 });
    try {
      await warmup(api);
      const msp = await tryLogin(api, MSP_USER, MSP_PASS);
      test.skip(!msp, `${MSP_USER} 登录失败：后端未就绪或凭据不同（可用 E2E_MSP_PASSWORD 覆盖）`);
      const headers = { Authorization: `Bearer ${await bearerOf(api)}` };

      const custA = await findCustomerA(api, headers);
      if (custA === null) {
        test.skip(true, 'MSP 路由不可用（HTTP 404）：后端需以 DEPLOYMENT_MODE=saas_msp 运行');
      }

      // 前置（幂等）：确保 (mspadmin, 客户A) 分配已存在；已存在时后端返回 409 也接受。
      const allocResp = await api.post('/api/v1/msp/allocations', {
        headers,
        data: { mspUserId: msp!.data.user.id, customerTenantId: custA!.id, role: 'primary' },
      });
      expect([200, 409], await allocResp.text()).toContain(allocResp.status());

      // ---------- UI：分配管理页发起重复分配 ----------
      await browserLogin(page, MSP_USER, MSP_PASS);
      await page.goto('/msp/management');
      await expect(page.getByText('MSP 分配管理')).toBeVisible({ timeout: 30_000 });

      await page.getByRole('button', { name: '新建分配' }).click();
      // 以可访问角色定位（a11y 快照：dialog "创建 MSP 分配"），不依赖 AntD 内部 class 结构。
      const modal = page.getByRole('dialog', { name: '创建 MSP 分配' });
      await expect(modal).toBeVisible({ timeout: 20_000 });

      // 选择 MSP 员工：直接点选（该 Select 声明 optionFilterProp="children" 但使用
      // options 数据源，输入过滤会命中空 children → 空结果；避免依赖搜索行为）。
      const selects = modal.locator('.ant-select');
      await selects.nth(0).click();
      await page
        .locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option')
        .filter({ hasText: MSP_USER })
        .first()
        .click();
      // AntD 选中后浮层收起有 ~200ms 动画：等其真正隐藏（+稳定帧），避免遮挡下一个 Select 的点击。
      await expect(page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden)')).toHaveCount(0, {
        timeout: 10_000,
      });
      await page.waitForTimeout(300);

      // 选择客户租户（同样直接点选）
      await selects.nth(1).click();
      await page
        .locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option')
        .filter({ hasText: CUSTOMER_A_CODE })
        .first()
        .click();
      await expect(page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden)')).toHaveCount(0, {
        timeout: 10_000,
      });
      await page.waitForTimeout(300);

      // 提交（AntD 两字中文按钮可访问名含空格：「创 建」）
      await modal.getByRole('button', { name: /创\s*建/ }).click();

      // ---------- 断言：409 的业务文案可见；弹窗与页面保持可用 ----------
      await expect(page.getByText(/该员工已分配至该客户/)).toBeVisible({ timeout: 15_000 });
      await expect(modal.getByText('创建 MSP 分配')).toBeVisible();
      await expect(modal.getByRole('button', { name: /创\s*建/ })).toBeEnabled();

      // 关闭弹窗后页面仍可正常交互（表格在、可再次打开创建弹窗）
      await modal.getByRole('button', { name: /取\s*消/ }).click();
      await expect(modal).toBeHidden();
      await page.getByRole('button', { name: '新建分配' }).click();
      await expect(page.getByRole('dialog', { name: '创建 MSP 分配' })).toBeVisible();
    } finally {
      await api.dispose();
    }
  });

  test('E2 超配额 422 → 创建工单页提示用户可读文案且表单可用（Q 的 UI 呈现）', async ({
    page,
  }) => {
    test.setTimeout(240_000);

    const api = await pwRequest.newContext({ baseURL: APP_URL, timeout: 120_000 });
    const custaApi = await pwRequest.newContext({ baseURL: APP_URL, timeout: 120_000 });
    let quotaTenantId = 0;
    let quotaSet = false;
    try {
      await warmup(api);
      await warmup(custaApi);
      // ---------- 0. API 前置：定位客户A + 设配额（limit = 本月已用量 → 下一次必然超限） ----------
      const msp = await tryLogin(api, MSP_USER, MSP_PASS);
      test.skip(!msp, `${MSP_USER} 登录失败：后端未就绪或凭据不同`);
      const mspHeaders = { Authorization: `Bearer ${await bearerOf(api)}` };
      const custA = await findCustomerA(api, mspHeaders);
      if (custA === null) {
        test.skip(true, 'MSP 路由不可用（HTTP 404）：后端需以 DEPLOYMENT_MODE=saas_msp 运行');
      }

      const custa = await tryLogin(custaApi, CUSTA_USER, CUSTA_PASS);
      test.skip(!custa, `${CUSTA_USER} 登录失败：跳过（客户侧样本无法准备）`);
      const custaHeaders = { Authorization: `Bearer ${await bearerOf(custaApi)}` };

      // 保证本月至少 1 条工单：为 0 时先补一条（否则 limit=0 语义是「不限」，无法构造超限）。
      const admin = await tryLogin(api, ADMIN_USER, ADMIN_PASS);
      test.skip(!admin, `${ADMIN_USER} 登录失败：跳过`);
      const adminHeaders = { Authorization: `Bearer ${await bearerOf(api)}` };

      const usageResp = await api.get(`/api/v1/tenants/${custA!.id}/usage`, {
        headers: adminHeaders,
      });
      expect(usageResp.status(), await usageResp.text()).toBe(200);
      let used = ((await usageResp.json()) as { data: { used: { ticketsThisMonth: number } } }).data
        .used.ticketsThisMonth;

      if (used === 0) {
        const seed = await custaApi.post('/api/v1/tickets', {
          headers: custaHeaders,
          data: {
            title: `[E2E-QUOTA-SEED-${Date.now()}] 配额前置样本`,
            description: 'quota seed',
            priority: 'low',
            type: 'incident',
          },
        });
        expect(seed.status(), await seed.text()).toBe(200);
        used = 1;
      }

      const put = await api.put(`/api/v1/tenants/${custA!.id}`, {
        headers: adminHeaders,
        data: { quota: { maxTicketsPerMonth: used } },
      });
      expect(put.status(), await put.text()).toBe(200);
      quotaSet = true;
      quotaTenantId = custA!.id;

      // ---------- UI：客户管理员在创建工单页提交 → 后端 422 → 页面提示 ----------
      await browserLogin(page, CUSTA_USER, CUSTA_PASS);
      await page.goto('/tickets/create');
      await expect(page.getByTestId('ticket-create-header')).toBeVisible({ timeout: 30_000 });

      const title = `[E2E-QUOTA-${Date.now()}] 超配额样本`;
      await page.getByTestId('ticket-title-input').fill(title);
      await page
        .getByTestId('ticket-description-input')
        .fill('配额超限 UI 呈现验收：期望后端 422 + 页面中文提示，且表单可继续编辑。');

      const respPromise = page.waitForResponse(
        r => r.url().includes('/api/v1/tickets') && r.request().method() === 'POST'
      );
      await page.getByTestId('ticket-submit-button').click();
      const resp = await respPromise;
      expect(resp.status()).toBe(422);

      await expect(page.getByText(/已达租户配额上限/)).toBeVisible({ timeout: 15_000 });
      // 表单可用：标题保留、提交按钮可再次点击
      await expect(page.getByTestId('ticket-title-input')).toHaveValue(title);
      await expect(page.getByTestId('ticket-submit-button')).toBeEnabled();
    } finally {
      // 恢复配额（0 = 不限），避免污染后续用例/验收。
      if (quotaSet && quotaTenantId > 0) {
        try {
          const admin = await tryLogin(api, ADMIN_USER, ADMIN_PASS);
          if (admin) {
            const adminHeaders = { Authorization: `Bearer ${await bearerOf(api)}` };
            await api.put(`/api/v1/tenants/${quotaTenantId}`, {
              headers: adminHeaders,
              data: { quota: { maxTicketsPerMonth: 0 } },
            });
          }
        } catch {
          // 清理失败不掩盖用例结论；脚本/验收可再收敛。
        }
      }
      await custaApi.dispose();
      await api.dispose();
    }
  });
});
