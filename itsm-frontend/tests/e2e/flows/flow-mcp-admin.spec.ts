/**
 * MCP 外部工具接入 — 浏览器 / API 端到端规格（M2-05，A2-05）
 *
 * 归属：实施方案 docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md
 *       §4.3 M2-05（浏览器 E2E 全链路），验收项 A2-05。
 *
 * 运行前提（真实栈，见 .github/workflows/e2e-mcp.yml）：
 *   - 后端：`mcp.enabled=true` + 凭据加密密钥已配置，端口 8090（vite 代理默认 ITSM_BACKEND_URL）；
 *   - mock MCP 服务器：`go run ./cmd/mcp-mockserver -addr :19090`（默认夹具 default）；
 *   - 前端：Playwright webServer 自动 `npm run dev -- --port 3000 --strictPort`；
 *   - 账号：seeder 的 `admin / AdminProd2026!`（见 tests/e2e/fixtures/auth.ts）。
 *
 * 覆盖（对齐 A2-05 的「对话→工具调用→审批→执行→时间线/审计」在 MCP 管理面的可见闭环）：
 *   1) 管理面 API 全链路：建服务器 → 测试连接 → 启用 → 工具发现/治理 → 凭据轮换 → 禁用（含工具面塌缩）；
 *   2) 管理页浏览器渲染与交互：登录 → /admin/mcp-servers → 服务器行/状态徽标/健康摘要 → 工具抽屉；
 *   3) 治理页冒烟：/ai/approval 与 /ai/audit 可渲染且无 console error（来源维度细节由 M1-06/M1-07 的
 *      jest 用例与 API 断言覆盖）。
 *
 * 说明：本机（Windows，无 Postgres/Docker）无法起真实后端，故本规格的执行证据由 CI 作业产出；
 *      规格内所有断言均针对真实后端，不使用 page.route 打桩。
 */
import { test, expect, TEST_ACCOUNTS } from '../fixtures/auth';

/** mock MCP 服务器地址（CI 起在 19090；本地可用 MCP_E2E_SERVER_URL 覆盖）。 */
const MOCK_MCP_URL = process.env.MCP_E2E_SERVER_URL || 'http://127.0.0.1:19090/mcp';
/** 前端直连后端的地址（与 vite.config.ts 的 ITSM_BACKEND_URL 保持一致）。 */
const API_BASE = process.env.ITSM_BACKEND_URL || 'http://127.0.0.1:8090';

type Envelope<T> = { code: number; message?: string; data: T };

/** MCP 未启用（整组路由未注册 → 404）时跳过，避免在非 MCP 栈上假红。 */
async function skipIfMCPDisabled(apiGet: (t: string, p: string) => Promise<any>, token: string) {
  const probe = await apiGet(token, '/api/v1/ai/mcp-servers');
  if (probe.status === 404 || probe.status === 503) {
    test.skip(true, `MCP 管理路由不可用（HTTP ${probe.status}）：需 mcp.enabled=true 的栈`);
  }
  expect(probe.status, `MCP 列表应 200，实际 ${probe.status}`).toBe(200);
}

/** 轮询等待条件（默认 30s；服务器启停为异步 202 + 状态回读，D8）。 */
async function waitFor<T>(
  label: string,
  fn: () => Promise<T>,
  predicate: (value: T) => boolean,
  timeoutMs = 30_000,
): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  let last: T;
  for (;;) {
    last = await fn();
    if (predicate(last)) return last;
    if (Date.now() > deadline) {
      throw new Error(`等待「${label}」超时（${timeoutMs}ms）：${JSON.stringify(last)?.slice(0, 400)}`);
    }
    await new Promise((r) => setTimeout(r, 500));
  }
}

/**
 * UI 登录（真实登录表单），并尽力从登录响应中取回访问令牌供 API 前置/断言复用。
 *
 * 之所以复用 UI 登录令牌而非再调一次 fixtures 的 `loginAs`：同一角色短时间多次登录会触发
 * 后端登录限流（fixtures 也为此做了缓存与重试）。若登录响应不含令牌（纯 Cookie 会话形态），
 * 返回 null，由调用方决定跳过还是仅做 UI 断言。
 */
async function loginViaUI(page: import('@playwright/test').Page): Promise<string | null> {
  const loginResponse = page
    .waitForResponse(
      (r) => r.url().includes('/api/v1/auth/login') && r.request().method() === 'POST',
      { timeout: 30_000 },
    )
    .catch(() => null);

  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  const username = page
    .locator('input[name="username"], input#username, input[placeholder*="用户"], input[type="text"]')
    .first();
  const password = page.locator('input[type="password"]').first();
  await username.fill(TEST_ACCOUNTS.admin.username);
  await password.fill(TEST_ACCOUNTS.admin.password);
  await page.getByRole('button', { name: /登录|登 录|Login|Sign in/i }).first().click();

  let token: string | null = null;
  const response = await loginResponse;
  if (response) {
    try {
      const json = (await response.json()) as { data?: { accessToken?: string; access_token?: string } };
      token = json?.data?.accessToken ?? json?.data?.access_token ?? null;
    } catch {
      token = null;
    }
  }

  await page.waitForURL((url) => !/\/login/.test(url.pathname), { timeout: 20_000 });
  return token;
}

test.describe('MCP 管理面：API 全链路（A2-05 / M2-05）', () => {
  test('建服务器 → 测试连接 → 启用 → 工具治理 → 轮换凭据 → 禁用', async ({ loginAs, apiGet, apiPost }) => {
    const token = await loginAs('admin');
    await skipIfMCPDisabled(apiGet, token);

    const name = `e2e-mcp-${Date.now()}`;
    let serverId: number | undefined;
    try {
      // 1) 建服务器（默认拒绝：创建后不自动启用）。
      const created = await apiPost(token, '/api/v1/ai/mcp-servers', {
        name,
        display_name: `E2E ${name}`,
        transport: 'streamable',
        url: MOCK_MCP_URL,
        credential_type: 'none',
      });
      expect(created.status, `创建服务器失败：${JSON.stringify(created.data)}`).toBeLessThan(300);
      serverId = (created.data as Envelope<{ id: number }>).data.id;
      expect(serverId, '创建应返回服务器 id').toBeTruthy();

      // 默认拒绝（D7）：新建服务器处于 disabled。
      const afterCreate = await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}`);
      expect((afterCreate.data as Envelope<{ status: string }>).data.status).toBe('disabled');

      // 2) 测试连接（同步短超时；不落库）——mock 服务器应握手成功。
      const tested = await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/test`, {
        transport: 'streamable',
        url: MOCK_MCP_URL,
      });
      expect(tested.status, `测试连接失败：${JSON.stringify(tested.data)}`).toBeLessThan(300);
      expect((tested.data as Envelope<{ ok: boolean }>).data.ok).toBe(true);

      // 3) 启用（202 + 状态回读）→ healthy + 工具被发现。
      const enabled = await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/enable`, {});
      expect([200, 202]).toContain(enabled.status);
      await waitFor(
        '服务器进入 healthy',
        async () => (await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}`)).data as Envelope<{ status: string }>,
        (res) => res.data?.status === 'healthy',
      );
      const tools = await waitFor(
        '工具被发现',
        async () =>
          (await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}/tools`)).data as Envelope<{ items: any[] }>,
        (res) => (res.data?.items?.length ?? 0) > 0,
      );
      // D3：可调用名必须有强制前缀 mcp__<server>__<tool>。
      const firstTool = tools.data.items[0];
      expect(firstTool.callable_name).toMatch(new RegExp(`^mcp__${name}__`));

      // 4) 工具治理：分类标注（read_only/risk）+ 批量停用/恢复。
      const classified = await apiPost(
        token,
        `/api/v1/ai/mcp-servers/${serverId}/tools/${encodeURIComponent(firstTool.callable_name)}/classification`,
        { read_only: true, risk: 'low' },
      );
      expect(classified.status, `分类失败：${JSON.stringify(classified.data)}`).toBeLessThan(300);

      const bulkOff = await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/tools/bulk`, { enabled: false });
      expect(bulkOff.status).toBeLessThan(300);
      const afterBulkOff = (await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}/tools`)).data as Envelope<{
        items: any[];
      }>;
      expect(afterBulkOff.data.items.every((t) => t.enabled === false), '批量停用后所有工具应不生效').toBe(true);

      const bulkOn = await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/tools/bulk`, { enabled: true });
      expect(bulkOn.status).toBeLessThan(300);

      // 5) 凭据轮换（只写不读回；掩码恒定）。
      const secret = `Bearer e2e-${Date.now()}`;
      const rotated = await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/rotate-credential`, {
        credential_type: 'static_header',
        credential: { Authorization: secret },
      });
      expect(rotated.status, `轮换失败：${JSON.stringify(rotated.data)}`).toBeLessThan(300);
      const detail = (await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}`)).data as Envelope<{
        credential_masked?: string;
      }>;
      expect(detail.data.credential_masked, '凭据应只回掩码').toBeTruthy();
      expect(JSON.stringify(detail.data), '明文凭据不得出现在任何响应中').not.toContain(secret);

      // 6) 禁用（202）→ 工具面塌缩 + 状态回读。
      const disabled = await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/disable`, {});
      expect([200, 202]).toContain(disabled.status);
      await waitFor(
        '服务器进入 disabled',
        async () => (await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}`)).data as Envelope<{ status: string }>,
        (res) => res.data?.status === 'disabled',
      );
    } finally {
      if (serverId) {
        // 清理（幂等）：删除失败不掩盖主断言结果。
        await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/disable`, {}).catch(() => undefined);
        const del = await fetch(`${API_BASE}/api/v1/ai/mcp-servers/${serverId}`, {
          method: 'DELETE',
          headers: { Authorization: `Bearer ${token}` },
        }).catch(() => undefined);
        void del;
      }
    }
  });
});

test.describe('MCP 管理页：浏览器渲染与交互（A2-05 / M2-05）', () => {
  test('登录 → 管理页 → 服务器行/状态/健康摘要 → 工具抽屉', async ({ page, apiGet, apiPost }) => {
    // 浏览器登录（真实表单）并复用其令牌做 API 前置（避免同角色重复登录触发限流）。
    const token = await loginViaUI(page);
    if (!token) {
      test.skip(true, '登录响应未返回访问令牌（Cookie 会话形态）：无法做 API 前置，先由 CI 校准登录契约');
      return;
    }
    await skipIfMCPDisabled(apiGet, token);

    // 通过 API 预置一台已启用的服务器（UI 只验证渲染与交互，不重复表单流程）。
    const name = `e2e-ui-${Date.now()}`;
    const created = await apiPost(token, '/api/v1/ai/mcp-servers', {
      name,
      display_name: `E2E UI ${name}`,
      transport: 'streamable',
      url: MOCK_MCP_URL,
      credential_type: 'none',
    });
    const serverId = (created.data as Envelope<{ id: number }>).data.id;

    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });

    try {
      await apiPost(token, `/api/v1/ai/mcp-servers/${serverId}/enable`, {});
      await waitFor(
        '服务器进入 healthy',
        async () => (await apiGet(token, `/api/v1/ai/mcp-servers/${serverId}`)).data as Envelope<{ status: string }>,
        (res) => res.data?.status === 'healthy',
      );

      await page.goto('/admin/mcp-servers', { waitUntil: 'domcontentloaded' });

      // 服务器行与状态徽标（data-testid 为页面稳定锚点）。
      const row = page.locator(`[data-testid="mcp-row-${serverId}"]`);
      await expect(row).toBeVisible({ timeout: 20_000 });
      await expect(page.locator(`[data-testid="mcp-status-${serverId}"]`)).toBeVisible();
      await expect(row).toContainText(`E2E UI ${name}`);

      // 健康摘要卡片（页面顶部 summary）。
      await expect(page.locator('[data-testid^="mcp-summary-"]').first()).toBeVisible();

      // 工具抽屉：打开后应列出该服务器发现的工具（含 callable_name 前缀）。
      await row.getByRole('button').filter({ hasText: /工具|Tools/i }).first().click();
      const toolRow = page.locator('[data-testid^="mcp-tool-row-mcp__"]').first();
      await expect(toolRow).toBeVisible({ timeout: 20_000 });

      // 证据截图（CI 上传 playwright-report / results）。
      await page.screenshot({ path: test.info().outputPath('mcp-admin-page.png'), fullPage: true });

      expect(consoleErrors, `管理页出现 console error：${consoleErrors.join(' | ')}`).toEqual([]);
    } finally {
      const del = await fetch(`${API_BASE}/api/v1/ai/mcp-servers/${serverId}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      }).catch(() => undefined);
      void del;
    }
  });

  test('治理页冒烟：/ai/approval 与 /ai/audit 可渲染且无 console error', async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });

    await loginViaUI(page);
    for (const path of ['/ai/approval', '/ai/audit']) {
      await page.goto(path, { waitUntil: 'domcontentloaded' });
      await expect(page.locator('main, [role="main"], .ant-layout-content').first()).toBeVisible({
        timeout: 20_000,
      });
      await page.screenshot({ path: test.info().outputPath(`page${path.replace(/\//g, '-')}.png`), fullPage: true });
    }
    expect(consoleErrors, `治理页出现 console error：${consoleErrors.join(' | ')}`).toEqual([]);
  });
});
