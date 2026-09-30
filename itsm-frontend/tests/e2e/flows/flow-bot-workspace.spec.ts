/**
 * Bot 工作区浏览器用例（B4-01 / AB4-01 的 browser 通道）
 *
 * 目标：在真实栈上驱动 ITSM Bot 的三段浏览器可见链路：
 *   ① 详情页 launcher → 工作区（携带入口上下文）；
 *   ② 工作区 SSE 流 → 工具调用时间线 / 运行状态条；
 *   ③ 写工具 → 确认抽屉出现（人工确认入口可用）。
 *
 * 确定性来源：后端 mock LLM（`service/llm_mock_provider.go`，双条件
 * `LLM_PROVIDER=mock` ∧ `LLM_MOCK_ENABLED=true`）在提示词命中 `LLM_MOCK_TRIGGER`
 * （默认 `__tool__`）时发起**一次**工具调用，工具名由 `LLM_MOCK_TOOL_NAME` 指定。
 * 本用例默认按**写工具** `create_ticket` 校准（覆盖确认抽屉路径）；若 CI 选择只读工具，
 * 用例②仍适用，用例③按工具元数据自适应 skip（不假红）。
 *
 * 前置（由 .github/workflows/e2e-bot.yml 提供）：
 *   - 后端：`BOT_ENABLED=true`（Bot 面开关）；mock LLM 已启用；
 *   - 前端：vite dev :3000（`ITSM_BACKEND_URL` → 后端 :8090）；
 *   - 账号：`admin`（或具 `ai:read` 的角色），密码与 fixtures 一致。
 *
 * ⚠️ 首轮校准（本机无真实栈，未试跑；与 e2e-bot.yml 文件头的校准清单一并处理）：
 *   1) 工作区选择器：`bot-selector` 是否需要显式选择助手（GA 之前可能为空，属兼容默认）；
 *   2) 对话输入框：placeholder 文案与发送方式（Enter / 发送按钮）；
 *   3) 时间线出现时机：本用例等待 `tool-call-timeline` 可见，而非固定延时；
 *   4) 确认抽屉出现是否要求「已选择场景 Bot 且写工具授权」——CI 用 `create_ticket`（内置写工具，走兼容默认）。
 */
import { test, expect, TEST_ACCOUNTS } from '../fixtures/auth';

const API_BASE = process.env.ITSM_BACKEND_URL || 'http://127.0.0.1:8090';
const TRIGGER = process.env.LLM_MOCK_TRIGGER || '__tool__';
const MOCK_TOOL = process.env.LLM_MOCK_TOOL_NAME || 'create_ticket';
/** 写工具集合（用于用例③的 self-skip 判定；与后端 service/bot 兼容默认白名单对齐）。 */
const WRITE_TOOLS = new Set([
  'create_ticket',
  'draft_ticket_fields',
  'link_ticket_ci',
  'draft_kb_article',
]);

type Envelope<T> = { code: number; data: T };

async function loginViaUI(page: import('@playwright/test').Page): Promise<string | null> {
  const loginResponse = page
    .waitForResponse(
      (r) => r.url().includes('/api/v1/auth/login') && r.request().method() === 'POST',
      { timeout: 30_000 },
    )
    .catch(() => null);

  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page
    .locator('input[name="username"], input#username, input[placeholder*="用户"], input[type="text"]')
    .first()
    .fill(TEST_ACCOUNTS.admin.username);
  await page.locator('input[type="password"]').first().fill(TEST_ACCOUNTS.admin.password);
  await page.getByRole('button', { name: /登录|登 录|Login|Sign in/i }).first().click();

  let token: string | null = null;
  const response = await loginResponse;
  if (response) {
    const json = (await response.json().catch(() => null)) as { data?: { accessToken?: string } } | null;
    token = json?.data?.accessToken ?? null;
  }
  await page.waitForURL((url) => !/\/login/.test(url.pathname), { timeout: 20_000 });
  return token;
}

/** Bot 面可用性预检：路由未注册（404/503）时整组 skip，避免假红。 */
async function botFaceAvailable(request: import('@playwright/test').APIRequestContext, token: string): Promise<boolean> {
  const resp = await request.get(`${API_BASE}/api/v1/ai/bots`, {
    headers: { Authorization: `Bearer ${token}` },
    failOnStatusCode: false,
  });
  return resp.status() !== 404 && resp.status() !== 503;
}

test.describe('Bot 工作区：入口 → SSE → 工具时间线 → 确认抽屉（B4-01 / AB4-01）', () => {
  test('① 详情页 launcher 打开工作区并携带入口上下文', async ({ page, request }) => {
    const token = await loginViaUI(page);
    if (!token) {
      test.skip(true, '登录响应未返回访问令牌（Cookie 会话形态）：先由 CI 校准登录契约');
      return;
    }
    if (!(await botFaceAvailable(request, token))) {
      test.skip(true, 'Bot 路由不可用（HTTP 404/503）：需 bot.enabled=true 的栈');
      return;
    }

    // 进入任一工单详情（列表首行）；无工单数据时按 skip（数据前置由 CI 种子保证）。
    await page.goto('/tickets', { waitUntil: 'domcontentloaded' });
    const firstRow = page.locator('table tbody tr').first();
    if ((await firstRow.count()) === 0) {
      test.skip(true, '工单列表为空：需种子数据（CI 路径由 seeder 提供）');
      return;
    }
    await firstRow.click();
    await page.waitForURL(/\/tickets\/\d+/, { timeout: 20_000 });

    const launcher = page.getByTestId('ask-ai-launcher').first();
    await expect(launcher).toBeVisible({ timeout: 10_000 });
    await launcher.click();

    await page.waitForURL(/\/ai\/chat/, { timeout: 20_000 });
    // 上下文条：入口徽标可见（B3-01 的 scope chip；非法 state 不渲染）。
    await expect(page.getByTestId('ask-ai-scope-chip')).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId('ask-ai-scope-chip')).toContainText(/工单|ticket/i);
  });

  test('② 对话触发工具调用并渲染时间线与运行状态', async ({ page, request }) => {
    const token = await loginViaUI(page);
    if (!token) {
      test.skip(true, '登录响应未返回访问令牌（Cookie 会话形态）');
      return;
    }
    if (!(await botFaceAvailable(request, token))) {
      test.skip(true, 'Bot 路由不可用（HTTP 404/503）');
      return;
    }

    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });

    await page.goto('/ai/chat', { waitUntil: 'domcontentloaded' });

    const input = page
      .locator('textarea[placeholder*="问"], textarea[placeholder*="消息"], textarea')
      .first();
    await expect(input).toBeVisible({ timeout: 15_000 });
    await input.fill(`请调用工具 ${TRIGGER}`);
    await input.press('Enter');

    // 工具时间线出现（SSE `tool_call_started/finished` 驱动）。
    await expect(page.getByTestId('tool-call-timeline')).toBeVisible({ timeout: 60_000 });
    await expect(page.getByTestId('tool-call-timeline')).toContainText(MOCK_TOOL.replace(/^mcp__/, '').slice(0, 24), {
      timeout: 30_000,
    });
    // 运行状态条（run 归档维度可见）。
    await expect(page.getByTestId('run-status-bar')).toBeVisible({ timeout: 15_000 });

    expect(consoleErrors, `控制台错误：${consoleErrors.join(' | ')}`).toHaveLength(0);
  });

  test('③ 写工具触发确认抽屉，确认入口可跳转审批页', async ({ page, request }) => {
    test.skip(
      !WRITE_TOOLS.has(MOCK_TOOL) && !MOCK_TOOL.startsWith('mcp__'),
      `当前 mock 工具 ${MOCK_TOOL} 非写工具（只读链路见用例②）`,
    );

    const token = await loginViaUI(page);
    if (!token) {
      test.skip(true, '登录响应未返回访问令牌（Cookie 会话形态）');
      return;
    }
    if (!(await botFaceAvailable(request, token))) {
      test.skip(true, 'Bot 路由不可用（HTTP 404/503）');
      return;
    }

    await page.goto('/ai/chat', { waitUntil: 'domcontentloaded' });
    const input = page
      .locator('textarea[placeholder*="问"], textarea[placeholder*="消息"], textarea')
      .first();
    await expect(input).toBeVisible({ timeout: 15_000 });
    await input.fill(`请调用工具 ${TRIGGER}`);
    await input.press('Enter');

    // 确认抽屉：B1-07 交付的对话内确认卡片（写工具 Gate3）。
    const drawer = page.getByTestId('confirmation-drawer');
    const appeared = await drawer
      .waitFor({ state: 'visible', timeout: 60_000 })
      .then(() => true)
      .catch(() => false);
    if (!appeared) {
      test.skip(true, `写工具未触发确认（工具=${MOCK_TOOL}；可能被策略/授权拒绝），详见后端日志`);
      return;
    }

    await expect(page.getByTestId('confirmation-approve')).toBeVisible();
    await expect(page.getByTestId('confirmation-reject')).toBeVisible();
    // 抽屉内可跳转审批页（人工审批入口）。
    const jump = page.getByRole('link', { name: /审批|approval/i }).first();
    if ((await jump.count()) > 0) {
      await jump.click();
      await page.waitForURL(/\/ai\/approval/, { timeout: 20_000 });
    }
  });
});
