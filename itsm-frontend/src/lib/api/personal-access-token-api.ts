import { httpClient } from './http-client';

/**
 * 个人访问令牌（Personal Access Token，PAT）API 客户端。
 *
 * 设计：docs/plan/personal-access-token-plan-2026-10-16.md §3.9。
 * 端点全部为身份自省面（浏览器会话 Cookie + CSRF）；令牌明文仅创建/轮换响应返回一次。
 * 令牌调用方（curl/CI/CLI）使用 Authorization: Bearer <itsm_pat_...>，不走本客户端。
 */

const BASE = '/api/v1/auth/personal-access-tokens';

export interface PersonalAccessToken {
  id: number;
  name: string;
  description?: string;
  tokenPrefix: string;
  maskedToken: string;
  status: 'active' | 'revoked' | 'expired';
  expiresAt?: string; // 永不过期时缺省
  neverExpires: boolean;
  lastUsedAt?: string;
  lastUsedIp?: string;
  useCount: number;
  revokedAt?: string;
  revokedReason?: string;
  createdAt: string;
}

export interface CreateTokenRequest {
  name: string;
  description?: string;
  expiresInDays?: number;
  expiresAt?: string;
  /** true = 永不过期（与 expiresInDays/expiresAt 互斥，优先生效） */
  neverExpires?: boolean;
}

export interface CreateTokenResponse {
  token: PersonalAccessToken;
  plaintextToken: string;
}

export interface UsageItem {
  id: number;
  at: string;
  action: string;
  method: string;
  path: string;
  statusCode: number;
  ip: string;
  source: string;
}

export interface UsageSummary {
  useCount: number;
  lastUsedAt?: string;
  lastUsedIp?: string;
  firstUsedAt?: string;
}

export interface UsagePage {
  items: UsageItem[];
  total: number;
  page: number;
  pageSize: number;
  summary: UsageSummary;
}

export interface SecurityEvent {
  id: number;
  at: string;
  action: string;
  source: string;
  path: string;
  method: string;
  statusCode: number;
  ip: string;
  tokenId?: number;
}

export interface EventPage {
  items: SecurityEvent[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ListUsagesParams {
  page?: number;
  pageSize?: number;
}

export interface ListSecurityEventsParams extends ListUsagesParams {
  kind?: 'all' | 'login' | 'token';
}

export class PersonalAccessTokenApi {
  /** 本人令牌列表（掩码，无明文）。 */
  static async listTokens(): Promise<PersonalAccessToken[]> {
    const data = await httpClient.get<{ tokens: PersonalAccessToken[] }>(BASE);
    return data?.tokens ?? [];
  }

  /** 创建令牌；明文仅此一次返回（UI 必须一次性展示）。 */
  static async createToken(req: CreateTokenRequest): Promise<CreateTokenResponse> {
    return httpClient.post<CreateTokenResponse>(BASE, req);
  }

  /** 轮换：gracePeriodHours 0=立即吊销旧令牌；24=旧令牌 24h 宽限。 */
  static async rotateToken(id: number, gracePeriodHours: 0 | 24 = 0): Promise<CreateTokenResponse> {
    return httpClient.post<CreateTokenResponse>(`${BASE}/${id}/rotate`, { gracePeriodHours });
  }

  /** 吊销单令牌（幂等）。 */
  static async revokeToken(id: number): Promise<PersonalAccessToken> {
    return httpClient.post<PersonalAccessToken>(`${BASE}/${id}/revoke`, {});
  }

  /** 吊销本人全部令牌。 */
  static async revokeAllTokens(): Promise<{ revokedCount: number }> {
    return httpClient.post<{ revokedCount: number }>(`${BASE}/revoke-all`, {});
  }

  /** 令牌使用历史（分页 + 汇总）。 */
  static async listUsages(id: number, params?: ListUsagesParams): Promise<UsagePage> {
    return httpClient.get<UsagePage>(`${BASE}/${id}/usages`, params);
  }

  /** 本人安全事件（登录/切换/令牌事件合并；分页上限 100）。 */
  static async listSecurityEvents(params?: ListSecurityEventsParams): Promise<EventPage> {
    return httpClient.get<EventPage>('/api/v1/auth/security-events', params);
  }
}
