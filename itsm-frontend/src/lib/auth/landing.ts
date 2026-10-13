/**
 * 登录后默认落点（角色感知）。
 *
 * 背景（跨租户缺口治理）：
 * - /dashboard 是租户内总览（GET /api/v1/dashboard/overview 按当前租户过滤），
 *   MSP 服务商员工在此只能看到 provider 自身租户数据；
 * - MSP 员工的主工作面是 /msp/workbench（跨租户工作台，数据经 allocation 授权面）；
 * - 无显式 redirect 参数时，按用户能力码选择默认落点；显式 redirect
 *   （守卫回跳、通知深链）始终优先，由调用方自行短路。
 */
import { isMspProviderTenant, MSP_WORKBENCH_PERMISSIONS } from '@/routes/area-permissions';
import { permissionGranted } from '@/routes/route-permissions';

/** 租户内用户默认落点：租户内运营总览。 */
export const TENANT_LANDING_PATH = '/dashboard';
/** MSP 服务商员工默认落点：跨租户工作台。 */
export const MSP_LANDING_PATH = '/msp/workbench';

/**
 * 按权限解析默认落点：
 * - 非服务商租户上下文（客户租户 / 未知类型）→ /dashboard（fail-closed）；
 * - 平台超级管理员（permissions = ['*']）按治理视角落 /dashboard；
 * - 服务商租户 + 显式持有工作台数据面操作码（msp_ticket:read）→ /msp/workbench；
 * - 其余（含仅持区域码 msp:read 的角色）→ /dashboard。
 *
 * 两道收敛缺一不可：
 * 1. 权限码不能宽泛到 MSP_AREA_PERMISSIONS（`msp:read` 客户租户角色基线也持有）；
 * 2. 客户租户角色克隆可能残留跨租户 msp_* 码（IT总监/系统运维等），
 *    因此必须叠加租户类型校验，与后端 IsMSP（租户类型 + msp_role）同向。
 */
export function resolveDefaultLanding(
  permissions?: string[] | null,
  tenantType?: string | null
): string {
  if (!isMspProviderTenant(tenantType)) return TENANT_LANDING_PATH;
  if (!permissions || permissions.includes('*')) return TENANT_LANDING_PATH;
  const hasWorkbenchAccess = MSP_WORKBENCH_PERMISSIONS.some(code =>
    permissionGranted(permissions ?? undefined, code)
  );
  return hasWorkbenchAccess ? MSP_LANDING_PATH : TENANT_LANDING_PATH;
}
