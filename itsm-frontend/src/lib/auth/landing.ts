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
import { MSP_WORKBENCH_PERMISSIONS } from '@/routes/area-permissions';
import { permissionGranted } from '@/routes/route-permissions';

/** 租户内用户默认落点：租户内运营总览。 */
export const TENANT_LANDING_PATH = '/dashboard';
/** MSP 服务商员工默认落点：跨租户工作台。 */
export const MSP_LANDING_PATH = '/msp/workbench';

/**
 * 按权限解析默认落点：
 * - 平台超级管理员（permissions = ['*']）按治理视角落 /dashboard；
 * - 显式持有工作台数据面操作码（msp_ticket:read，服务商内置角色基线）→ 工作台；
 * - 其余（含仅持区域码 msp:read 的客户租户 admin）→ /dashboard。
 *
 * 注意不能宽泛到 MSP_AREA_PERMISSIONS：`msp:read` 同时存在于客户租户 admin
 * 角色基线中（服务端 `msp/status` 判其为「非MSP用户」），会把人送进必然 403
 * 的工作台。
 */
export function resolveDefaultLanding(permissions?: string[] | null): string {
  if (!permissions || permissions.includes('*')) return TENANT_LANDING_PATH;
  const hasWorkbenchAccess = MSP_WORKBENCH_PERMISSIONS.some(code =>
    permissionGranted(permissions ?? undefined, code)
  );
  return hasWorkbenchAccess ? MSP_LANDING_PATH : TENANT_LANDING_PATH;
}
