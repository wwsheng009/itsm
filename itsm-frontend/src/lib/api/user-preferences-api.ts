/**
 * 用户服务端偏好 API（IP-P1-6c）。
 *
 * 契约（itsm-backend/handlers/common/user_preferences.go）：
 * - GET /api/v1/users/me/preferences → { preferences: { workbenchFilter?: {...} } }
 * - PUT 同路径：请求体为偏好补丁（白名单键；null 删除键），响应返回合并后全量；
 * - 仅本人可读写（AuthMiddleware + claims user_id）。
 */
import { httpClient } from './http-client';

/** 工作台过滤器偏好（与 CustomerFilter 的 URL query 语义一致）。 */
export interface WorkbenchFilterPreference {
  mode: 'all' | 'subset';
  customerTenantIds: number[];
}

export interface UserPreferences {
  workbenchFilter?: WorkbenchFilterPreference;
  [key: string]: unknown;
}

export interface UserPreferencesResponse {
  preferences: UserPreferences;
}

/** GET /api/v1/users/me/preferences */
export async function getUserPreferences(): Promise<UserPreferencesResponse> {
  return httpClient.get<UserPreferencesResponse>('/api/v1/users/me/preferences');
}

/** PUT /api/v1/users/me/preferences（白名单键；null 删除） */
export async function updateUserPreferences(
  patch: Partial<UserPreferences> & Record<string, unknown>
): Promise<UserPreferencesResponse> {
  return httpClient.put<UserPreferencesResponse>('/api/v1/users/me/preferences', patch);
}
