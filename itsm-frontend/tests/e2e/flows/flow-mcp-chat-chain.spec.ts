/**
 * MCP 对话链路浏览器用例（M2-05 / A2-05 的核心一环）
 *
 * 目标：在真实栈上驱动「对话 → 工具调用 → 执行 → 时间线」这一段浏览器可见链路。
 * 确定性来源：后端 `service/llm_mock_provider.go`（双条件启用：`llm.provider=mock` ∧
 * `LLM_MOCK_ENABLED=true`），提示词命中触发词 `__tool__` 时发起**一次**工具调用，工具名由
 * `LLM_MOCK_TOOL_NAME` 指定（本用例约定为 `mcp__e2e-mcp-chat__list_issues`，与下方建服务名一致）。
 *
 * 前置（由 .github/workflows/e2e-mcp.yml 提供）：
 *   - 后端 MCP 开关：`MCP_ENABLED=true`、`MCP_WRITE_ENABLED=true`、`MCP_ENCRYPTION_KEY` 已配置；
 *   - mock MCP 服务器：`cmd/mcp-mockserver -addr :19090`；
 *   - mock LLM：`LLM_PROVIDER=mock`、`LLM_MOCK_ENABLED=true`、`LLM_MOCK_TRIGGER=__tool__`、
 *     `LLM_MOCK_TOOL_NAME=mcp__e2e-mcp-chat__list_issues`；
 *   - 前端：vite dev :3000（`ITSM_BACKEND_URL` 指向后端 :8090）。
 *
 * ⚠️ 校准点（本机无真实栈，无法试跑；与 e2e-mcp.yml 文件头的校准清单一并处理）：
 *   1) 对话页输入框选择器（placeholder 文案）与发送方式（Enter / 发送按钮）；
 *   2) 时间线在流式过程中出现的时机（本用例用「等待文本包含工具名」而非固定延时）；
 *   3) 若前端要求选择会话/助手，需补一次下拉选择。
 */
import { test, expect, TEST_ACCOUNTS } from '../fixtures/auth';

const MOCK_MCP_URL = process.env.MCP_E2E_SERVER_URL || 'http://127.0.0.1:19090/mcp';
const API_BASE = process.env.ITSM_BACKEND_URL || 'http://127.0.0.1:8090';
/** 固定服务器名：使工具投影名可确定（`mcp__e2e-mcp-chat__list_issues`），与 CI 的 LLM_MOCK_TOOL_NAME 对齐。 */
const SERVER_NAME = 'e2e-mcp-chat';
const CALLABLE = `mcp__${SERVER_NAME}__list_issues`;
const TRIGGER = process.env.LLM_MOCK_TRIGGER || '__tool__';
const MOCK_REPLY_MARKER = 'mock LLM';

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

/** 幂等准备：确保存在一台启用的 `e2e-mcp-chat` 服务器，且其工具全部启用（默认拒绝语义：新工具默认不启用）。 */
async function ensureEnabledServer(
  token: string,
  apiGet: (t: string, p: string) => Promise<any>,
  apiPost: (t: string, p: string, b?: any) => Promise<any>,
): Promise<number> {
  const listed = (await apiGet(token, '/api/v1/ai/mcp-servers')).data as Envelope<{
    items: Array<{ id: number; name: string }>;
  }>;
  const existing = listed.data?.items?.find((item) => item.name === SERVER_NAME);
  if (existing) {
    await apiPost(token, `/api/v1/ai/mcp-servers/${existing.id}/disable`, {});
    await fetch(`${API_BASE}/api/v1/ai/mcp-servers/${existing.id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${token}` },
    }).catch(() => undefined);
  }

  const created = await apiPost(token, '/api/v1/ai/mcp-servers', {
    name: SERVER_NAME,
    display_name: 'E2E 对话链路',
    transport: 'streamable',
    url: MOCK_MCP_URL,
    credential_type: 'none',
  });
  const serverId = (created.data as Envelope<{ id: number }>).data.id;

  await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/enable`, {});
  for (let attempt = 0; attempt < 60; attempt += 1) {
    const detail = (await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}`)).data as Envelope<{
      status: string;
    }>;
    if (detail.data?.status === 'healthy') break;
    await new Promise((r) => setTimeout(r, 500));
  }
  await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/tools/bulk`, { enabled: true });
  return serverId;
}

test.describe('MCP 对话链路：工具调用与时间线（A2-05 / M2-05）', () => {
  test('对话触发 MCP 只读工具调用并出现在时间线', async ({ page, apiGet, apiPost }) => {
    const token = await loginViaUI(page);
    if (!token) {
      test.skip(true, '登录响应未返回访问令牌（Cookie 会话形态）：无法做 API 前置，先由 CI 校准登录契约');
      return;
    }

    const probe = await apiGet(token, '/api/v1/ai/mcp-servers');
    if (probe.status === 404 || probe.status === 503) {
      test.skip(true, `MCP 管理路由不可用（HTTP ${probe.status}）：需 mcp.enabled=true 的栈`);
      return;
    }

    let serverId: number | undefined;
    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });

    try {
      serverId = await ensureEnabledServer(token, apiGet, apiPost);

      await page.goto('/ai/chat', { waitUntil: 'domcontentloaded' });
      const input = page
        .locator('textarea:visible, input[type="text"]:visible, [contenteditable="true"]:visible')
        .last();
      await expect(input).toBeVisible({ timeout: 30_000 });
      await input.click();
      await input.fill(`请调用工具 ${TRIGGER} 查询打开的问题`);
      await input.press('Enter');

      // 1) 助手回复到达（mock provider 的固定回复标记）。
      await expect(page.getByText(new RegExp(MOCK_REPLY_MARKER, 'i')).first()).toBeVisible({ timeout: 60_000 });

      // 2) 工具调用时间线出现，且包含 MCP 工具的投影名（D3 前缀）。
      const timeline = page.locator('[data-testid="tool-call-timeline"]').first();
      await expect(timeline).toBeVisible({ timeout: 60_000 });
      await expect(timeline).toContainText(CALLABLE, { timeout: 60_000 });

      // 3) 证据截图。
      await page.screenshot({ path: test.info().outputPath('mcp-chat-tool-call.png'), fullPage: true });
      expect(consoleErrors, `对话页出现 console error：${consoleErrors.join(' | ')}`).toEqual([]);
    } finally {
      if (serverId) {
        await fetch(`${API_BASE}/api/v1/ai/mcp-servers/${serverId}`, {
          method: 'DELETE',
          headers: { Authorization: `Bearer ${token}` },
        }).catch(() => undefined);
      }
    }
  });
});
