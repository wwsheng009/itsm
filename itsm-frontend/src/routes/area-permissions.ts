/**
 * 区域守卫权限集（剩余缺口治理 B6 / Sweep §4；routes/index.tsx 与 Sidebar 共用单源）。
 *
 * 语义：admin 区 = 「任一细码可入（本表并集）+ 页级细化门禁（routes/index.tsx 内联）」。
 * 修复前 `ADMIN_AREA_PERMISSIONS` 仅 4 码（user:write/role:write/system_config:write/
 * ticket_type:manage），仅持 approval:write / sla:write 等专职管理员被整区拦下，
 * 与后端细粒度不一致（Sweep §4「粗粒度偏差」）。
 *
 * 码表来源（2026-10-10 实测）：backend `RequirePermission` 词表（router/handlers 扫描）
 * + Sweep §4 表；只收录「能真实进入至少一个 admin 页面」的码，避免区域放行面漂移。
 */

/** admin 区并集：任一命中即可进入区域，具体页面由页级门禁继续收敛。 */
export const ADMIN_AREA_PERMISSIONS: string[] = [
  // 组织与账号
  'user:write',
  'group:write',
  'department:write',
  'team:write',
  // RBAC（admin/permissions 为只读面，后端码 = permission:read）
  'role:write',
  'permission:read',
  // 系统配置与接入
  'system_config:write',
  'connector:write',
  // 工单治理
  'ticket_type:manage',
  'ticket_category:update',
  // 流程与审批（admin/workflows、admin/approval-chains、admin/approvals）
  'workflow:write',
  'bpmn:write',
  'approval:write',
  // SLA（escalation-rules/matrices 无 escalation:* 词表，复用 sla:write）
  'sla:write',
  // CMDB 治理
  'cmdb:write',
  // AI 与 MCP 治理
  'ai:write',
  'mcp:admin',
  // 变更 CAB
  'change:approve',
  // 服务目录治理
  'service_catalog:write',
];

/** 租户管理（admin/tenants）：菜单口径 = system:write；同时放行 tenant:* 专职运营角色。 */
export const TENANT_ADMIN_PERMISSIONS: string[] = ['system:write', 'tenant:read', 'tenant:write'];

/** MSP 区（/msp/*）区域守卫权限集。 */
export const MSP_AREA_PERMISSIONS: string[] = [
  'msp:read',
  'msp_ticket:read',
  'msp_customer:read',
  'msp_allocation:read',
  'msp_report:read',
];
