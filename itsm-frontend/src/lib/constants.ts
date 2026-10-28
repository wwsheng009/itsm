export const INCIDENT_STATUS_LABEL: Record<string, string> = {
  new: 'Pending',
  inProgress: 'In Progress',
  waitingCustomer: 'Waiting for Customer',
  resolved: 'Resolved',
  closed: 'Closed',
};

export const INCIDENT_PRIORITY_LABEL: Record<string, string> = {
  urgent: 'Critical',
  high: 'High',
  medium: 'Medium',
  low: 'Low',
};

export const TICKET_STATUS_LABEL: Record<string, string> = {
  submitted: 'Submitted',
  assigned: 'Assigned',
  inProgress: 'In Progress',
  resolved: 'Resolved',
  closed: 'Closed',
};

/**
 * 通用枚举 code → 中文标签（覆盖审查根因 B 中反复直出的原始 code）。
 * 仅作展示兜底：命中返回中文，未命中由调用方回退原始值，绝不返回 undefined。
 */

/** 配置项类型 code → 中文（CMDB 配置项列表曾直出 application/database/network…）。 */
export const CI_TYPE_LABEL: Record<string, string> = {
  application: '应用系统',
  database: '数据库',
  network: '网络',
  server: '服务器',
  storage: '存储',
  middleware: '中间件',
  virtual: '虚拟资源',
  cloud: '云资源',
  device: '设备',
};

/** 服务请求状态 code → 中文（与 constants/service-request.ts 的枚举取值对齐）。 */
export const SERVICE_REQUEST_STATUS_LABEL: Record<string, string> = {
  submitted: '已提交',
  manager_approved: '经理已审批',
  it_approved: 'IT 已审批',
  security_approved: '安全已审批',
  provisioning: '履约中',
  delivered: '已交付',
  failed: '履约失败',
  rejected: '已拒绝',
  cancelled: '已取消',
};

/** 审批步骤 code → 中文（ApprovalStep：manager/it/security，替代 `MANAGER 审批` 直出）。 */
export const APPROVAL_STEP_LABEL: Record<string, string> = {
  manager: '经理审批',
  it: 'IT 审批',
  security: '安全部门审批',
};

/**
 * 通用枚举取中文标签。
 * 命中词表返回中文；否则原样返回 code（保持既有行为、不产生 undefined）；
 * code 为空时返回 fallback。
 */
export const enumLabel = (
  map: Record<string, string>,
  code?: string | null,
  fallback?: string,
): string => {
  if (!code) return fallback || '';
  return map[code] || code;
};
