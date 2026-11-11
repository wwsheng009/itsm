import { request as playwrightRequest, type APIRequestContext } from 'playwright';
import { account } from '../fixtures/demo-accounts';
import { DEMO_BASE_URL } from '../fixtures/demo-auth';

interface AdminCleanupSession {
  api: APIRequestContext;
  tenantId: number;
  headers: Record<string, string>;
}

let sessionPromise: Promise<AdminCleanupSession> | null = null;

async function loginAdmin(): Promise<AdminCleanupSession> {
  const admin = account('admin');
  const api = await playwrightRequest.newContext({ baseURL: DEMO_BASE_URL, timeout: 30_000 });
  const login = await api.post('/api/v1/auth/login', {
    data: { username: admin.username, password: admin.password },
  });
  if (!login.ok()) {
    await api.dispose();
    throw new Error(`admin cleanup login failed: HTTP ${login.status()}`);
  }
  const token = (await api.storageState()).cookies.find(cookie => cookie.name === 'access_token')?.value;
  if (!token) {
    await api.dispose();
    throw new Error('admin cleanup login missing access_token');
  }
  return { api, tenantId: 1, headers: { Authorization: `Bearer ${token}` } };
}

async function getAdminCleanupSession(): Promise<AdminCleanupSession> {
  if (!sessionPromise) sessionPromise = loginAdmin();
  return sessionPromise;
}

async function switchTenant(session: AdminCleanupSession, tenantId: number): Promise<void> {
  if (session.tenantId === tenantId) return;
  const response = await session.api.post('/api/v1/auth/switch-tenant', {
    headers: session.headers,
    data: { tenantId },
  });
  if (!response.ok()) {
    throw new Error(`admin cleanup switch tenant ${tenantId} failed: HTTP ${response.status()}`);
  }
  const token = (await session.api.storageState()).cookies.find(cookie => cookie.name === 'access_token')?.value;
  if (!token) throw new Error('admin cleanup switch missing access_token');
  session.headers = { Authorization: `Bearer ${token}` };
  session.tenantId = tenantId;
}

/**
 * 平台 super_admin 切换到目标租户完成服务请求清理，作为租户管理员删除权限的兜底。
 */
export async function deleteServiceRequestAsPlatformAdmin(
  tenantId: number,
  serviceRequestId: number,
): Promise<boolean> {
  const session = await getAdminCleanupSession();
  await switchTenant(session, tenantId);
  const response = await session.api.delete(`/api/v1/service-requests/${serviceRequestId}`, {
    headers: session.headers,
  });
  return response.ok();
}

/** 发布单的 release:delete 不在租户管理员默认权限内；清理走平台 super_admin。 */
export async function deleteReleaseAsPlatformAdmin(
  tenantId: number,
  releaseId: number,
): Promise<boolean> {
  const session = await getAdminCleanupSession();
  await switchTenant(session, tenantId);
  const response = await session.api.delete(`/api/v1/releases/${releaseId}`, {
    headers: session.headers,
  });
  return response.ok();
}

/** 跨租户边界用例创建的变更可能不在租户管理员删除权限内；清理走平台 super_admin。 */
export async function deleteChangeAsPlatformAdmin(
  tenantId: number,
  changeId: number,
): Promise<boolean> {
  const session = await getAdminCleanupSession();
  await switchTenant(session, tenantId);
  const response = await session.api.delete(`/api/v1/changes/${changeId}`, {
    headers: session.headers,
  });
  return response.ok();
}