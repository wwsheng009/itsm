/**
 * FLOW-工作台 UI：跨客户工作台真实点击链（IP-P0-7/8 · IP-P1-6b · IP-P2-4b/4c）。
 *
 * 承接 API 层业务验收（scripts/msp/acceptance/run-msp-business-acceptance.ps1，G0–G12），
 * 把判据平移到浏览器操作层——覆盖只有真实点击才能暴露的链路：
 *   0. API 前置（幂等）：mspadmin 分配自身→客户A；custa_admin 建 2 条工单样本；
 *   1. mspadmin Cookie 会话打开 /msp/workbench（/auth/me 引导出权限与 mspRole）；
 *   2. 顶栏 CustomerFilter：勾选客户A → URL `customerTenantIds` 同步（只改视图不改会话）；
 *   3. 行内回复（弹窗 → 发送 → 成功提示）；行内改状态（下拉 → 处理中 → 行内徽标更新）；
 *   4. 批量回复：勾选 2 行 → 客户分布确认 → 提交 → 逐条结果（成功 2）；
 *   5. 看板/分组视图冒烟（SlaRiskBoard / CustomerUsageBoard / 分组计数）。
 *
 * 环境：
 *   - 后端需为 `saas_msp` 模式（私有/缺省模式 MSP 路由 404 → 显式 skip，不假红）；
 *   - vite dev server 由 Playwright webServer 自动拉起（`/api` 代理到 ITSM_BACKEND_URL，默认 8090）；
 *   - 凭证可用 E2E_MSP_PASSWORD / E2E_CUSTA_PASSWORD 覆盖（与验收脚本默认一致）。
 *
 * 标签 `@multi-tenant`：可被 CI `--grep @multi-tenant` 纳入多租户回归。
 */
import { test, expect, request as pwRequest, type APIRequestContext } from '@playwright/test';

const APP_URL = process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:3000';
const MSP_USER = process.env.E2E_MSP_USERNAME || 'mspadmin';
const MSP_PASS = process.env.E2E_MSP_PASSWORD || 'Msp@2026Staff!';
const CUSTA_USER = process.env.E2E_CUSTA_USERNAME || 'custa_admin';
const CUSTA_PASS = process.env.E2E_CUSTA_PASSWORD || 'Cust@2026User!';
const CUSTOMER_A_CODE = process.env.E2E_MSP_CUSTOMER_A_CODE || 'MSPCUSTA';

interface LoginBody {
  code: number;
  data: { user: { id: number; username: string; role?: string; mspRole?: string } };
}

/** 登录（Cookie 会话）；失败返回 null，由调用方决定 skip 或失败。 */
async function tryLogin(
  ctx: APIRequestContext,
  username: string,
  password: string
): Promise<LoginBody | null> {
  const resp = await ctx.post('/api/v1/auth/login', { data: { username, password } });
  if (resp.status() !== 200) return null;
  return (await resp.json()) as LoginBody;
}

/**
 * 取登录会话的 access_token（Cookie 形态）转 Bearer。
 * 前置写操作统一走 Bearer：后端 CSRF 中间件对 `Authorization: Bearer` 明确豁免
 * （middleware/csrf.go:154-159），与验收脚本 `Invoke-Api` 的策略一致。
 */
async function bearerOf(ctx: APIRequestContext): Promise<string> {
  const state = await ctx.storageState();
  return state.cookies.find(cookie => cookie.name === 'access_token')?.value ?? '';
}

test.describe('@multi-tenant MSP 工作台 UI 点击链', () => {
  test('过滤器 → 行内回复/改状态 → 批量确认 → 看板/分组', async ({ page }) => {
    test.setTimeout(240_000); // 冷启动 vite 编译本页较慢（实测 >60s），放宽

    const mspApi = await pwRequest.newContext({ baseURL: APP_URL });
    const custaApi = await pwRequest.newContext({ baseURL: APP_URL });

    try {
      // ---------- 0. API 前置 ----------
      const msp = await tryLogin(mspApi, MSP_USER, MSP_PASS);
      test.skip(!msp, `${MSP_USER} 登录失败：后端未就绪或凭据不同（可用 E2E_MSP_PASSWORD 覆盖）`);
      const mspHeaders = { Authorization: `Bearer ${await bearerOf(mspApi)}` };

      const customersResp = await mspApi.get('/api/v1/msp/customers', { headers: mspHeaders });
      test.skip(
        customersResp.status() === 404,
        'MSP 路由不可用（HTTP 404）：后端需以 DEPLOYMENT_MODE=saas_msp 运行'
      );
      expect(customersResp.status(), await customersResp.text()).toBe(200);
      const customersBody = (await customersResp.json()) as {
        data?: { customers?: Array<{ id: number; code: string; name: string }> };
      };
      const customers = customersBody.data?.customers ?? [];
      const customerA = customers.find(item => item.code === CUSTOMER_A_CODE) ?? customers[0];
      test.skip(!customerA, '当前 mspadmin 无任何已分配客户，无法执行工作台链路');
      const custA = customerA!;

      // 分配本人至客户A（幂等：已存在返回 409 MSP_ALLOCATION_EXISTS）
      const allocResp = await mspApi.post('/api/v1/msp/allocations', {
        headers: mspHeaders,
        data: { mspUserId: msp!.data.user.id, customerTenantId: custA.id, role: 'primary' },
      });
      expect([200, 409]).toContain(allocResp.status());

      const custa = await tryLogin(custaApi, CUSTA_USER, CUSTA_PASS);
      test.skip(!custa, `${CUSTA_USER} 登录失败：跳过（客户侧样本无法准备）`);
      const custaHeaders = { Authorization: `Bearer ${await bearerOf(custaApi)}` };

      const stamp = Date.now();
      const sampleTitles = [`[E2E-UI-${stamp}] 工作台样本一`, `[E2E-UI-${stamp}] 工作台样本二`];
      const ticketIds: number[] = [];
      for (const title of sampleTitles) {
        const created = await custaApi.post('/api/v1/tickets', {
          headers: custaHeaders,
          data: { title, description: 'MSP workbench UI e2e', priority: 'high', type: 'incident' },
        });
        expect(created.status(), await created.text()).toBe(200);
        const createdBody = (await created.json()) as { data?: { id?: number } };
        expect(createdBody.data?.id, `建单未返回 id：${title}`).toBeGreaterThan(0);
        ticketIds.push(createdBody.data!.id!);
      }
      const [ticketOne, ticketTwo] = ticketIds;

      // 浏览器上下文登录（经 vite 代理，Set-Cookie 落入浏览器 CookieJar）
      const uiLogin = await page.request.post('/api/v1/auth/login', {
        data: { username: MSP_USER, password: MSP_PASS },
      });
      expect(uiLogin.status()).toBe(200);

      // 显式携带 customerTenantIds=all：跳过「服务端偏好水合」分支，
      // 避免上一次运行保存的 subset 偏好与本用例的勾选点击竞态。
      await page.goto('/msp/workbench?customerTenantIds=all');
      await expect(page.getByTestId('msp-workbench-page')).toBeVisible({ timeout: 30_000 });

      // ---------- U1 样本可见（默认全部客户） ----------
      await expect(page.getByText(sampleTitles[0])).toBeVisible({ timeout: 20_000 });
      await expect(page.getByText(sampleTitles[1])).toBeVisible();

      // ---------- U2 顶栏过滤器：勾选客户A → URL 同步 ----------
      const filterTrigger = page.getByTestId('customer-filter-trigger');
      await expect(filterTrigger).toBeVisible({ timeout: 15_000 });
      await filterTrigger.click();
      await expect(page.getByTestId('customer-filter-panel')).toBeVisible();
      await page.getByTestId(`customer-checkbox-${custA.id}`).click();
      await expect
        .poll(() => page.url(), { timeout: 10_000 })
        .toContain(`customerTenantIds=${custA.id}`);
      await page.keyboard.press('Escape');
      // 过滤后样本仍在（属于客户A）
      await expect(page.getByText(sampleTitles[0])).toBeVisible();

      // ---------- U3 行内回复（弹窗 → 发送 → 成功提示） ----------
      await page.getByTestId(`action-reply-${ticketOne}`).click();
      await expect(page.getByTestId('reply-content')).toBeVisible();
      await page.getByTestId('reply-content').fill(`[E2E-UI-${stamp}] 行内回复`);
      // 注意：AntD 对两字中文按钮会插入空格（可访问名为「发 送」），用宽松正则匹配。
      await page.getByRole('button', { name: /发\s*送/ }).click();
      await expect(page.getByText('回复已发送')).toBeVisible({ timeout: 15_000 });

      // ---------- U4 行内改状态（下拉 → 处理中 → 行内徽标更新） ----------
      await expect(page.locator(`tr[data-row-key="${ticketOne}"]`)).toBeVisible();
      await page.getByTestId(`action-status-${ticketOne}`).click();
      // 只取「可见」下拉里的菜单项：页面上还存在状态筛选 Select 的同名隐藏项，
      // 直接 page.locator('.ant-dropdown') 会命错元素导致点击被拦截。
      const statusMenuItem = page
        .locator('.ant-dropdown:not(.ant-dropdown-hidden) .ant-dropdown-menu-item')
        .filter({ hasText: '处理中' });
      await expect(statusMenuItem).toBeVisible({ timeout: 10_000 });
      // Dropdown 默认 hover 触发 + 表格固定列/表头遮挡，Playwright 真实点击会被
      // 「拦截/元素不稳定」反复拒绝；DOM 级 click 仍会走 React 合成事件，更稳。
      await statusMenuItem.evaluate(el => (el as HTMLElement).click());
      await expect(page.getByText('状态已更新')).toBeVisible({ timeout: 15_000 });
      await expect(page.locator(`tr[data-row-key="${ticketOne}"]`)).toContainText('处理中');

      // ---------- U5 批量回复：选择 → 分布确认 → 逐条结果 ----------
      for (const id of ticketIds) {
        await page.locator(`tr[data-row-key="${id}"] .ant-checkbox-wrapper`).first().click();
      }
      await expect(page.getByTestId('batch-selected-count')).toHaveText('已选 2 条');
      await page.getByTestId('batch-reply').click();
      await expect(page.getByTestId('batch-reply-content')).toBeVisible();
      await page.getByTestId('batch-reply-content').fill(`[E2E-UI-${stamp}] 批量回复`);
      await page.getByRole('button', { name: '下一步' }).click();

      await expect(page.getByTestId('batch-confirm')).toBeVisible();
      await expect(page.getByTestId('batch-confirm')).toContainText(custA.name);
      await page.getByRole('button', { name: /提交（2 条）/ }).click();

      await expect(page.getByTestId('batch-result')).toBeVisible({ timeout: 20_000 });
      await expect(page.getByTestId('batch-result')).toContainText('成功 2');
      await page.getByTestId('batch-result-close').click();

      // ---------- U6 看板与分组视图冒烟 ----------
      await expect(page.getByTestId('sla-risk-board')).toBeVisible();
      await expect(page.getByTestId('customer-usage-board')).toBeVisible();
      await page.getByTestId('workbench-view-mode').getByText('按客户分组').click();
      await expect(page.getByTestId('workbench-group-view')).toBeVisible({ timeout: 15_000 });
      await expect(page.getByTestId(`group-count-${custA.id}`)).toBeVisible();
    } finally {
      await mspApi.dispose();
      await custaApi.dispose();
    }
  });
});
