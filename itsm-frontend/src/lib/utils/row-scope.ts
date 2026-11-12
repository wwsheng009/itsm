/**
 * 行级权限预判（2026-10-14 缺口③修复）。
 *
 * 背景：后端生命周期写操作（resolve/close/escalate/PUT 更新）有行级校验
 * （handlers/common/datascope.CanWriteResource）：管理角色（admin/super_admin/
 * manager/sysadmin）全租户可写，其余角色仅创建人/受理人可写。
 * 前端此前仅按权限码（如 ticket:update）判定按钮显隐，导致权限码在但行级
 * 403 的「可点必败」形态（demo：MSP 经理切换客户租户后角色降级为 end_user，
 * 但权限列表仍含 ticket:update）。
 *
 * 本工具与后端判定对齐，仅作 UI 预判——服务端仍是最终裁决，不得用于替代
 * 后端校验。未知角色按非管理处理（安全收窄，与后端一致）。
 */

/** 与后端 domain/role.IsAdminLike 对齐的管理角色词表（仅表达“管理类”语义）。 */
const ADMIN_LIKE_ROLES = new Set(['super_admin', 'admin', 'manager', 'sysadmin']);

/**
 * 与后端 domain/role.DefaultDataScope 对齐的「目标租户内全量可写」角色词表。
 *
 * 后端把 provider_tenant_admin 与 MSP 五角色（msp_viewer/msp_tech/msp_specialist/
 * msp_manager/msp_admin）的规范 data_scope 统一定为 all，
 * 因此 CanWriteResourceWithScope 走 IsTenantWide 直接放行，判定为租户内全量可写。
 *
 * 2026-11-12 修复：此前本文件只认 ADMIN_LIKE_ROLES，导致 MSP/provider 角色
 * （例如 provider 经理切入客户租户后的生效角色 msp_manager）被误判为只读：
 * 权限码 ticket:update 明明存在（「抄送」按钮可见），但编辑/分配/解决/关闭全部隐藏。
 * 前端预判必须与后端放行口径一致，否则出现「该给的权限没给」。
 */
const TENANT_WIDE_ROLES = new Set([
  ...ADMIN_LIKE_ROLES,
  'provider_tenant_admin',
  'msp_viewer',
  'msp_tech',
  'msp_specialist',
  'msp_manager',
  'msp_admin',
]);

export interface RowScopeActor {
  id?: number | null;
  role?: string | null;
  /**
   * MSP 原生角色（provider_admin / provider_agent / customer_user）。
   *
   * 2026-11-12 修复的关键：provider 侧账号在客户租户里的 `role` 仍是 `end_user`
   * （实测 /auth/me：role=end_user，mspRole=provider_admin，membershipId=22），
   * 真正的 MSP 身份只体现在这个字段上。只比较 `role` 必然把 provider 经理误判为只读。
   */
  mspRole?: string | null;
  /**
   * 生效角色全量列表（主角色 + membership 角色 + m2m 附加角色）。
   * 后端 datascope.ResolveForActor 取「多角色最宽范围」，前端同样必须按并集判定。
   */
  roles?: (string | null | undefined)[] | null;
}

export interface RowScopeResource {
  requesterId?: number | null;
  assigneeId?: number | null;
}

/**
 * 判断 actor 是否持有「目标租户内全量可写」的角色（管理类 ∪ MSP/provider 类）。
 * 大小写与前后空白做归一，避免后端 code 与前端字面量细微差异导致漏判。
 */
export function isTenantWideRole(role: string | null | undefined): boolean {
  if (!role) return false;
  return TENANT_WIDE_ROLES.has(role.trim().toLowerCase());
}

/**
 * MSP 原生角色 → RBAC 角色映射，与后端 middleware.mspRoleToRBACRoleMap 逐项一致
 * （provider_admin → msp_manager、provider_agent → msp_tech、customer_user → end_user）。
 * 未命中映射时原样返回，便于直接写 msp_* 角色的调用方复用。
 */
const MSP_NATIVE_ROLE_MAP: Record<string, string> = {
  provider_admin: 'msp_manager',
  provider_agent: 'msp_tech',
  customer_user: 'end_user',
};

/** 把 MSP 原生角色归一为 RBAC 角色 code。 */
export function normalizeMspRole(mspRole: string | null | undefined): string | null {
  if (!mspRole) return null;
  const key = mspRole.trim().toLowerCase();
  return MSP_NATIVE_ROLE_MAP[key] ?? key;
}

/**
 * 判断 actor 是否持有「目标租户内全量可写」的角色。
 * 判定面覆盖三处，任一命中即可（与后端「多角色取最宽范围」一致）：
 *   1. `role`（主/生效角色）；
 *   2. `mspRole`（MSP 原生角色，映射后判定）——provider 侧账号的关键路径；
 *   3. `roles[]`（membership / m2m 附加角色并集）。
 */
export function hasTenantWideRole(
  actor: Pick<RowScopeActor, 'role' | 'mspRole' | 'roles'> | null | undefined,
): boolean {
  if (!actor) return false;
  if (isTenantWideRole(actor.role)) return true;
  if (isTenantWideRole(normalizeMspRole(actor.mspRole))) return true;
  if (Array.isArray(actor.roles)) {
    return actor.roles.some((r) => isTenantWideRole(r ?? null) || isTenantWideRole(normalizeMspRole(r ?? null)));
  }
  return false;
}

/**
 * 判断当前用户是否可对该工单执行行级写操作（解决/关闭/编辑等）。
 * - 租户全量角色（管理类 + MSP/provider 类）：全租户可写；
 * - 其余角色：仅创建人（requester）或受理人（assignee）；
 * - 身份未知（无 id）时返回 false（fail-closed）。
 */
export function canWriteRow(actor: RowScopeActor | null | undefined, resource: RowScopeResource | null | undefined): boolean {
  if (!actor || !resource) return false;
  const actorId = actor.id ?? 0;
  if (actorId <= 0) return false;
  if (hasTenantWideRole(actor)) return true;
  if (resource.requesterId != null && resource.requesterId > 0 && actorId === resource.requesterId) return true;
  if (resource.assigneeId != null && resource.assigneeId > 0 && actorId === resource.assigneeId) return true;
  return false;
}
