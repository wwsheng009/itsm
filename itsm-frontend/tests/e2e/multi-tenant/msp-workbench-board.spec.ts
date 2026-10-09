/**
 * MSP 工作台跨租户面板 E2E（IP-P2-16 前端入口实弹）。
 *
 * 覆盖（provider 员工 admin-demo01 登录，tenant 10）：
 * 1. `?board=tasks` 深链 → 待办任务投影面板渲染 + 任务接口 200 + 零控制台错误；
 * 2. 面板切换（工单 → 待办任务 → 跨租户单据）→ URL `board` 持久化 + 面板可见；
 * 3. 跨租户单据面板 → 三类 Segmented 可见 + 单据接口 200 + 行内动作按钮渲染口径（allowedActions）。
 *
 * 说明：仓库共享助手 `auth-utils.loginAndReturn` 会把 baseURL 的端口剥掉
 * （`http://localhost:3000` → `http://localhost`）导致登录请求打到 80 端口 502；
 * 本用例内联等价登录（相对 URL 走 vite `/api` 代理），不改动共享助手以免影响既有用例。
 *
 * 运行：npx playwright test tests/e2e/multi-tenant/msp-workbench-board.spec.ts --project=chrome --workers=1
 */
import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const PROVIDER_USER = process.env.E2E_MSP_USER || 'admin-demo01';
const PROVIDER_PASS = process.env.E2E_MSP_PASS || 'Passw0rd@1234';

/**
 * provider 员工登录（返回 HTTP 状态码；成功时进入目标页）。
 *
 * 登录响应通过 HttpOnly cookie（access_token/refresh_token）下发会话，`page.request`
 * 与浏览器上下文共享 cookie jar，因此 POST 之后直接进入目标页即可；
 * 真实用户信息（含 mspRole/permissions）由路由守卫内的 `bootstrapSession()` → `/auth/me` 回填。
 *
 * 该用户由本地/联调环境种子提供（`.dev` 服务 + seed）；CI 等无种子的环境返回非 2xx，
 * 由调用方 `test.skip` 兜底，避免误红。
 */
async function loginAsProvider(page: Page, landingPath: string): Promise<number> {
  await page.context().clearCookies();

  const loginResponse = await page.request.post('/api/v1/auth/login', {
    data: { username: PROVIDER_USER, password: PROVIDER_PASS },
    timeout: 30_000,
  });
  const status = loginResponse.status();
  if (!loginResponse.ok()) return status;
  const loginJson = await loginResponse.json();
  if (!loginJson?.data?.user?.id) {
    return 200; // 结构异常：不进入目标页，由调用方 skip（防御性，正常不会发生）
  }

  await page.goto(landingPath, { waitUntil: 'domcontentloaded' });
  return status;
}

/** 登录；环境不具备 provider 种子时 skip 当前用例（本地实弹环境必过）。 */
async function loginOrSkip(page: Page, landingPath: string): Promise<void> {
  const status = await loginAsProvider(page, landingPath);
  test.skip(
    status !== 200,
    `provider 用户 ${PROVIDER_USER} 不可用（HTTP ${status}）：需本地实弹环境（.dev 服务 + seed）`
  );
}

test.describe('@multi-tenant / MSP workbench cross-tenant board', () => {
  test('待办任务投影：深链 board=tasks 渲染 + 接口 200 + 零控制台错误', async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });
    // 面板相关 API 失败（HTTP ≥400）单独捕获：这是本用例真正要守护的口径。
    const failedBoardRequests: string[] = [];
    page.on('response', response => {
      const url = response.url();
      if (url.includes('/api/v1/msp/') && response.status() >= 400) {
        failedBoardRequests.push(`${response.status()} ${url}`);
      }
    });

    const tasksResponse = page.waitForResponse(
      response => response.url().includes('/api/v1/msp/workbench/tasks'),
      { timeout: 30_000 }
    );
    await loginOrSkip(page, '/msp/workbench?board=tasks');

    const panel = page.getByTestId('cross-tenant-board-tasks');
    await expect(panel).toBeVisible({ timeout: 15_000 });
    await expect(panel.getByText('待办任务投影')).toBeVisible();
    await expect(panel.locator('.ant-table')).toBeVisible();

    const response = await tasksResponse;
    expect(response.status()).toBe(200);

    // 白名单外无 error 级日志。已知既有噪音（与本面板无关，工作台页面固有）：
    //  - antd 组件弃用告警（Drawer width → size）；
    //  - 自定义视图开关未开启（WORKBENCH_VIEWS_ENABLED，SavedViews 既有行为）；
    //  - 静态资源 404（dev server 缺失资源，非 API）。
    const knownNoise = ['[antd: Drawer]', 'WORKBENCH_VIEWS_ENABLED', 'status of 404'];
    const relevantErrors = consoleErrors.filter(
      text => !knownNoise.some(token => text.includes(token))
    );
    expect(relevantErrors).toEqual([]);
    // 面板 API 零失败。
    expect(failedBoardRequests).toEqual([]);
  });

  test('面板切换：URL board 参数持久化 + 各面板渲染', async ({ page }) => {
    await loginOrSkip(page, '/msp/workbench');

    const switchCard = page.getByTestId('workbench-board-switch');
    await expect(switchCard).toBeVisible({ timeout: 15_000 });

    // 工单（默认）→ 待办任务
    await switchCard.getByText('待办任务', { exact: true }).click();
    await expect(page).toHaveURL(/board=tasks/);
    await expect(page.getByTestId('cross-tenant-board-tasks')).toBeVisible();

    // 待办任务 → 跨租户单据
    const recordsResponse = page.waitForResponse(
      response => response.url().includes('/api/v1/msp/workbench/changes'),
      { timeout: 30_000 }
    );
    await switchCard.getByText('跨租户单据', { exact: true }).click();
    await expect(page).toHaveURL(/board=records/);
    const recordsPanel = page.getByTestId('cross-tenant-board-records');
    await expect(recordsPanel).toBeVisible();
    await expect(recordsPanel.locator('.ant-table')).toBeVisible();

    const response = await recordsResponse;
    expect(response.status()).toBe(200);

    // 三类 Segmented（变更/问题/服务请求）可见
    for (const label of ['变更', '问题', '服务请求']) {
      await expect(recordsPanel.getByText(label, { exact: true }).first()).toBeVisible();
    }

    // 回到工单：URL 参数清理
    await switchCard.getByText('工单', { exact: true }).click();
    await expect(page).not.toHaveURL(/board=/);
  });

  test('跨租户单据：状态推进按钮渲染口径（allowedActions 只读消费）', async ({ page }) => {
    const recordsResponse = page.waitForResponse(
      response => response.url().includes('/api/v1/msp/workbench/changes'),
      { timeout: 30_000 }
    );
    await loginOrSkip(page, '/msp/workbench?board=records');
    expect((await recordsResponse).status()).toBe(200);

    const recordsPanel = page.getByTestId('cross-tenant-board-records');
    await expect(recordsPanel).toBeVisible({ timeout: 15_000 });

    // 数据面存在（P2-③ 实弹已建变更 CHG-*）：行内动作按钮必须存在且来自服务端 allowedActions。
    const rows = recordsPanel.locator('.ant-table-tbody tr.ant-table-row');
    const rowCount = await rows.count();
    if (rowCount === 0) {
      await expect(recordsPanel.getByText('暂无跨租户单据')).toBeVisible();
      return;
    }
    const statusButtons = recordsPanel.locator('[data-testid^="record-status-"]');
    expect(await statusButtons.count()).toBeGreaterThan(0);
    const first = statusButtons.first();
    await expect(first).toBeVisible();
    await expect(first).toHaveAttribute('type', 'button');
  });
});
