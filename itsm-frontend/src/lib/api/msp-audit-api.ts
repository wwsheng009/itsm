/**
 * MSP 审计看板 API（IP-P1-8）。
 *
 * 契约来源（后端 service/msp_audit.go + handlers/msp/audit.go）：
 * - GET /api/v1/msp/audit/summary?days=30
 *   → home 租户窗口内的跨租户审计聚合 + 越权尝试/冲突告警明细。
 * - httpClient 自动解包 `data` 包络并做 camelCase 归一化。
 */
import { httpClient } from './http-client';

/** 审计看板路由（routes/** 注册；此处作为导航目标常量）。 */
export const MSP_AUDIT_PATH = '/msp/audit';

export interface MspAuditAggRow {
  key: string;
  label?: string;
  count: number;
}

/** 拒绝事件（越权尝试/冲突探测）明细行。 */
export interface MspAuditDenialRow {
  id: number;
  createdAt: string;
  action: string;
  source: string;
  targetTenantId: number;
  targetName?: string;
  actorAccount: string;
  statusCode: number;
  reasonCode: string;
  path: string;
}

export interface MspAuditSummary {
  windowDays: number;
  generatedAt: string;
  totalEvents: number;
  crossTenantEvents: number;
  deniedEvents: number;
  bySource: MspAuditAggRow[];
  byAction: MspAuditAggRow[];
  byTargetTenant: MspAuditAggRow[];
  byMembership: MspAuditAggRow[];
  recentDenials: MspAuditDenialRow[];
}

/** 拒绝类事件（与后端 service.isDeniedAction 对齐）。 */
export const MSP_AUDIT_DENIED_ACTIONS = ['tenant.scope_denied', 'tenant.probe_denied'] as const;

export const MSP_AUDIT_ACTION_LABEL: Record<string, string> = {
  'tenant.scope_denied': '未分配客户访问',
  'tenant.probe_denied': '租户冲突/探测',
  'tenant.switch_denied': '切换被拒',
  'workbench.action': '工作台操作',
  'auth.login': '登录',
  'user.provision': '建号',
  'user.invite': '邀请',
  'user.invite_accept': '邀请接受',
};

export const MSP_AUDIT_SOURCE_LABEL: Record<string, string> = {
  workbench: '工作台',
  header: '请求头通道',
  switch: '显式切换',
  login: '登录通道',
  platform_selected: '平台指定',
  job: '后台任务',
  system: '系统',
  legacy: '历史（未标记）',
};

/** GET /api/v1/msp/audit/summary —— 审计看板聚合（days 默认 30，上限 90）。 */
export async function getMspAuditSummary(days = 30): Promise<MspAuditSummary> {
  const safeDays = Number.isFinite(days) && days > 0 ? Math.min(Math.floor(days), 90) : 30;
  return httpClient.get<MspAuditSummary>(`/api/v1/msp/audit/summary?days=${safeDays}`);
}
