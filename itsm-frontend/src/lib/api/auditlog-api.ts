import { httpClient } from './http-client';

// 审计日志项类型（与后端 DTO 对齐）
export interface AuditLog {
  id: number;
  createdAt: string; // ISO 时间字符串
  tenantId: number;
  userId: number;
  // 后端 service 层 join user 表填充，name 优先、缺失回退 username
  userName?: string;
  requestId: string;
  ip: string;
  resource: string;
  action: string;
  path: string;
  method: string;
  statusCode: number;
  requestBody: string;
  // IP-P0-10 作用域扩展字段（后端 DTO 已返回，前端此前未声明）
  source?: string; // login|switch|header|workbench|platform_selected|job|system|pat；缺失=legacy
  actorAccount?: string;
  targetTenantId?: number;
  targetUserId?: number;
  tokenId?: number; // PAT 归因（source=pat 时非空）
}

// 查询参数（对应后端 ListAuditLogsRequest）
export interface ListAuditLogsParams {
  page?: number;
  pageSize?: number;
  userId?: number;
  resource?: string;
  action?: string;
  method?: string;
  statusCode?: number;
  path?: string;
  requestId?: string;
  from?: string; // RFC3339
  to?: string; // RFC3339
  source?: string; // 来源过滤（含 pat）
  tokenId?: number; // PAT 归因过滤
}

// 响应结构（对应后端 ListAuditLogsResponse）
export interface ListAuditLogsResponse {
  logs: AuditLog[];
  total: number;
  page: number;
  pageSize: number;
}

// 查询审计日志（自动携带 Authorization、X-Tenant-ID / X-Tenant-Code）
export async function listAuditLogs(params: ListAuditLogsParams): Promise<ListAuditLogsResponse> {
  const query = new URLSearchParams();
  if (params.page) query.set('page', String(params.page));
  if (params.pageSize) query.set('pageSize', String(params.pageSize));
  if (params.userId !== undefined) query.set('userId', String(params.userId));
  if (params.resource) query.set('resource', params.resource);
  if (params.action) query.set('action', params.action);
  if (params.method) query.set('method', params.method);
  if (params.statusCode !== undefined) query.set('statusCode', String(params.statusCode));
  if (params.path) query.set('path', params.path);
  if (params.requestId) query.set('requestId', params.requestId);
  if (params.from) query.set('from', params.from);
  if (params.to) query.set('to', params.to);
  if (params.source) query.set('source', params.source);
  if (params.tokenId !== undefined) query.set('tokenId', String(params.tokenId));

  const endpoint = `/api/v1/audit-logs${query.toString() ? `?${query.toString()}` : ''}`;
  return await httpClient.get<ListAuditLogsResponse>(endpoint);
}
