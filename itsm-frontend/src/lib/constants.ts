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

/** 服务请求状态 code → 中文（详情页/列表曾直出 SUBMITTED / submitted / cancelled）。 */
export const SERVICE_REQUEST_STATUS_LABEL: Record<string, string> = {
  draft: '草稿',
  submitted: '已提交',
  SUBMITTED: '已提交',
  approved: '已批准',
  rejected: '已驳回',
  in_progress: '处理中',
  inProgress: '处理中',
  fulfilled: '已交付',
  cancelled: '已取消',
  closed: '已关闭',
};

/** 优先级 code → 中文（工单/服务请求统计曾直出 low/medium/high/critical）。 */
export const PRIORITY_LABEL: Record<string, string> = {
  urgent: '紧急',
  critical: '致命',
  high: '高',
  medium: '中',
  low: '低',
};

/** 审批环节角色码 → 中文（服务请求详情曾直出 MANAGER / IT / SECURITY）。 */
export const APPROVAL_ROLE_LABEL: Record<string, string> = {
  MANAGER: '经理',
  IT: 'IT 支持',
  SECURITY: '安全',
  admin: '管理员',
  approver: '审批人',
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
