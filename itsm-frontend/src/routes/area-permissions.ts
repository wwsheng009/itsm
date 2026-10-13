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

/**
 * MSP 区（/msp/*）区域守卫权限集（数据面操作码，2026-10-13 收口）。
 *
 * 语义：能进 /msp 区域 = 至少能消费该区域某个数据面（工作台/客户/分配/报表）。
 * 注意**不含** `msp:read`：该码存在于客户租户角色基线（服务端 /msp/status 判其为
 * 「非MSP用户」），只放行它会让客户进入必然 403 的 MSP 区域（页内错误态）；
 * 数据面端点仍由后端 MSPMiddleware（IsMSP = 租户类型 + msp_role）最终裁决。
 */
export const MSP_AREA_PERMISSIONS: string[] = [
  'msp_ticket:read',
  'msp_customer:read',
  'msp_allocation:read',
  'msp_report:read',
];

/**
 * MSP 跨租户数据面入口权限（工作台 summary / tickets 的端点级守卫码）。
 *
 * 与 MSP_AREA_PERMISSIONS 的区别：区域放行包含 `msp:read`（客户租户 admin 的
 * 角色基线也持有该码，但服务端 `/msp/status` 判定其为「非MSP用户」）；
 * 数据面分区/落点必须按操作码收敛，否则会渲染出必然 403 的空数据。
 *
 * 码表依据（itsm-backend/internal/authz/roles.go）：
 * - 服务商 5 个内置角色（msp_viewer/tech/specialist/manager/admin）均含 msp_ticket:read；
 * - 客户租户内 msp_* 角色基线（MSPCustomerScopeRolePermissionCodes）不含跨租户 msp_* 码。
 */
export const MSP_WORKBENCH_PERMISSIONS: string[] = ['msp_ticket:read'];

/**
 * 服务商租户类型（镜像后端 pkg/tenantmode.IsMSPProviderTenantType：
 * `msp_provider` + 历史别名 `msp`）。
 *
 * 用途：跨租户数据面（工作台 summary / 分区入口）除了操作码，还必须校验
 * 「当前租户上下文为服务商租户」——客户租户的角色克隆可能残留跨租户 msp_* 码，
 * 仅凭权限码会误放行客户侧入口（后端 IsMSP 判定 = 租户类型 + msp_role，fail-closed）。
 */
export const MSP_PROVIDER_TENANT_TYPES: string[] = ['msp_provider', 'msp'];

/** 当前租户上下文是否服务商租户（未知类型一律 false，fail-closed）。 */
export function isMspProviderTenant(type?: string | null): boolean {
  return !!type && MSP_PROVIDER_TENANT_TYPES.includes(type);
}
