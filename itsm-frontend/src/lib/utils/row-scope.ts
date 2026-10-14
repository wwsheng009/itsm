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

/** 与后端 domain/role.IsAdminLike 对齐的管理角色词表。 */
const ADMIN_LIKE_ROLES = new Set(['super_admin', 'admin', 'manager', 'sysadmin']);

export interface RowScopeActor {
  id?: number | null;
  role?: string | null;
}

export interface RowScopeResource {
  requesterId?: number | null;
  assigneeId?: number | null;
}

/**
 * 判断当前用户是否可对该工单执行行级写操作（解决/关闭/编辑等）。
 * - 管理角色：全租户可写；
 * - 其余角色：仅创建人（requester）或受理人（assignee）；
 * - 身份未知（无 id）时返回 false（fail-closed）。
 */
export function canWriteRow(actor: RowScopeActor | null | undefined, resource: RowScopeResource | null | undefined): boolean {
  if (!actor || !resource) return false;
  const actorId = actor.id ?? 0;
  if (actorId <= 0) return false;
  if (actor.role && ADMIN_LIKE_ROLES.has(actor.role)) return true;
  if (resource.requesterId != null && resource.requesterId > 0 && actorId === resource.requesterId) return true;
  if (resource.assigneeId != null && resource.assigneeId > 0 && actorId === resource.assigneeId) return true;
  return false;
}
