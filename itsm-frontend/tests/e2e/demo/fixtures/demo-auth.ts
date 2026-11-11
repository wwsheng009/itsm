import {
  request as playwrightRequest,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
} from 'playwright';
import { account, type DemoAccount, type DemoRoleKey } from './demo-accounts';

export const DEMO_BASE_URL =
  process.env.PLAYWRIGHT_BASE_URL || 'http://172.18.3.238:8089';

export interface DemoSession {
  key: DemoRoleKey;
  account: DemoAccount;
  api: APIRequestContext;
  token: string;
  /** 登录响应中的用户视图（role 为签发 JWT 的 effective role）。 */
  loginUser: Record<string, unknown>;
  user: Record<string, unknown>;
  storageState: Awaited<ReturnType<APIRequestContext['storageState']>>;
}

const sessionCache = new Map<DemoRoleKey, Promise<DemoSession>>();

/**
 * 登录 demo 账号并缓存 session。
 *
 * 关键点：
 * - 登录响应只下发 httpOnly cookie，不依赖 body.accessToken；
 * - 使用同源相对路径 + baseURL，保留 8089 端口；
 * - 后续角色 API 调用复用同一 cookie/Authorization。
 */
export function getDemoSession(key: DemoRoleKey): Promise<DemoSession> {
  if (!sessionCache.has(key)) {
    sessionCache.set(key, loginDemo(key));
  }
  return sessionCache.get(key)!;
}

async function loginDemo(key: DemoRoleKey): Promise<DemoSession> {
  const acct = account(key);
  const api = await playwrightRequest.newContext({
    baseURL: DEMO_BASE_URL,
    timeout: 30_000,
  });

  const maxLoginAttempts = 5;
  let loginResponse: Awaited<ReturnType<typeof api.post>> | undefined;
  let lastLoginError: unknown;
  let lastLoginBody = '';
  for (let attempt = 0; attempt < maxLoginAttempts; attempt += 1) {
    try {
      loginResponse = await api.post('/api/v1/auth/login', {
        data: { username: acct.username, password: acct.password },
      });
    } catch (error) {
      // 演示环境偶发 ECONNRESET/连接中断：短暂退避后重试，不能把网络抖动当登录失败。
      lastLoginError = error;
      if (attempt < maxLoginAttempts - 1) {
        await new Promise(resolve => setTimeout(resolve, 2000));
      }
      continue;
    }
    if (loginResponse.ok()) break;

    const text = await loginResponse.text();
    lastLoginBody = text;
    let retryAfterSeconds = 0;
    try {
      retryAfterSeconds = Number(JSON.parse(text)?.data?.retryAfterSeconds ?? 0);
    } catch {
      // 非 JSON 错误不按限流处理。
    }
    // 登录限流是 10/min 的滚动窗口；只等 15s 且只重试两次在连续复跑时会误红。
    // 按服务端 retryAfterSeconds 等待（上限 60s），并用更多尝试覆盖窗口尾部。
    if (
      (loginResponse.status() === 403 || loginResponse.status() === 429) &&
      retryAfterSeconds > 0 &&
      attempt < maxLoginAttempts - 1
    ) {
      await new Promise(resolve => setTimeout(resolve, Math.min(retryAfterSeconds + 1, 60) * 1000));
      continue;
    }
    break;
  }
  if (!loginResponse?.ok()) {
    const text = lastLoginBody || String(lastLoginError ?? 'no response');
    await api.dispose();
    throw new Error(
      `demo login failed for ${acct.username}: HTTP ${loginResponse?.status() ?? 'network'} ${text.slice(0, 300)}`,
    );
  }
  const loginBody = await loginResponse.json();
  if (loginBody?.code !== 0) {
    await api.dispose();
    throw new Error(
      `demo login envelope failed for ${acct.username}: ${JSON.stringify(loginBody).slice(0, 300)}`,
    );
  }
  const loginUser = (loginBody?.data?.user ?? {}) as Record<string, unknown>;

  const storageState = await api.storageState();
  const access = storageState.cookies.find(cookie => cookie.name === 'access_token');
  if (!access?.value) {
    await api.dispose();
    throw new Error(`demo login missing access_token cookie for ${acct.username}`);
  }

  const headers = { Authorization: `Bearer ${access.value}` };
  const meResponse = await api.get('/api/v1/auth/me', { headers });
  if (!meResponse.ok()) {
    await api.dispose();
    throw new Error(`/auth/me failed for ${acct.username}: HTTP ${meResponse.status()}`);
  }
  const meBody = await meResponse.json();
  if (meBody?.code !== 0) {
    await api.dispose();
    throw new Error(`/auth/me envelope failed for ${acct.username}: ${JSON.stringify(meBody).slice(0, 300)}`);
  }

  return {
    key,
    account: acct,
    api,
    token: access.value,
    loginUser,
    user: meBody.data as Record<string, unknown>,
    storageState,
  };
}

export async function closeDemoSessions(): Promise<void> {
  const sessions = await Promise.allSettled([...sessionCache.values()]);
  sessionCache.clear();
  await Promise.all(
    sessions
      .filter((result): result is PromiseFulfilledResult<DemoSession> => result.status === 'fulfilled')
      .map(result => result.value.api.dispose()),
  );
}

/**
 * 深度切换 demo 账号作用域，并同步更新 session 缓存的 cookie/token/user。
 *
 * 注意：切换后必须重新读取 cookie jar，否则后续 `authHeaders(session)` 仍会
 * 带旧租户的 Bearer token；浏览器 context 也必须使用更新后的 storageState。
 */
export async function switchDemoTenant(
  session: DemoSession,
  tenantId: number,
): Promise<void> {
  const response = await session.api.post('/api/v1/auth/switch-tenant', {
    data: { tenantId },
  });
  if (!response.ok()) {
    throw new Error(
      `switch tenant ${tenantId} failed for ${session.account.username}: HTTP ${response.status()} ${(await response.text()).slice(0, 300)}`,
    );
  }

  const storageState = await session.api.storageState();
  const access = storageState.cookies.find(cookie => cookie.name === 'access_token');
  if (!access?.value) {
    throw new Error(`switch tenant ${tenantId} missing access_token cookie`);
  }

  const headers = { Authorization: `Bearer ${access.value}` };
  const meResponse = await session.api.get('/api/v1/auth/me', { headers });
  if (!meResponse.ok()) {
    throw new Error(`/auth/me after switch failed: HTTP ${meResponse.status()}`);
  }
  const meBody = await meResponse.json();
  if (meBody?.code !== 0) {
    throw new Error(
      `/auth/me after switch envelope failed: ${JSON.stringify(meBody).slice(0, 300)}`,
    );
  }

  session.token = access.value;
  session.storageState = storageState;
  session.user = meBody.data as Record<string, unknown>;
}

/** 创建一个已登录的浏览器上下文（UI 测试用）。 */
export async function newAuthenticatedContext(
  browser: Browser,
  key: DemoRoleKey,
  options: { viewport?: { width: number; height: number } } = {},
): Promise<{ context: BrowserContext; page: Page; session: DemoSession }> {
  const session = await getDemoSession(key);
  const context = await browser.newContext({
    baseURL: DEMO_BASE_URL,
    storageState: session.storageState,
    viewport: options.viewport ?? { width: 1440, height: 900 },
  });
  const page = await context.newPage();
  return { context, page, session };
}

/** 环境闸门：版本、健康、部署模式。 */
export async function getDemoRuntime(session: DemoSession): Promise<{
  version: string;
  ready: boolean;
  deploymentMode: string;
}> {
  const headers = { Authorization: `Bearer ${session.token}` };
  const [versionRes, readyRes, statusRes] = await Promise.all([
    session.api.get('/api/v1/version', { headers }),
    session.api.get('/api/v1/readyz', { headers }),
    session.api.get('/api/v1/msp/status', { headers }),
  ]);

  const versionBody = versionRes.ok() ? await versionRes.json() : null;
  const readyBody = readyRes.ok() ? await readyRes.json() : null;
  const statusBody = statusRes.ok() ? await statusRes.json() : null;

  return {
    version: String(versionBody?.data?.version ?? ''),
    ready: Boolean(readyBody?.ready),
    deploymentMode: String(statusBody?.data?.deploymentMode ?? statusBody?.deploymentMode ?? ''),
  };
}