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
import { MSP_AREA_PERMISSIONS } from '@/routes/area-permissions';
import { permissionGranted } from '@/routes/route-permissions';

/** 租户内用户默认落点：租户内运营总览。 */
export const TENANT_LANDING_PATH = '/dashboard';
/** MSP 服务商员工默认落点：跨租户工作台。 */
export const MSP_LANDING_PATH = '/msp/workbench';

/**
 * 按权限解析默认落点：命中任一 MSP 区域权限码 → 工作台，否则 → /dashboard。
 * 与 routes/index.tsx 的 MSP 区域守卫共用同一权限集，避免双源漂移。
 */
export function resolveDefaultLanding(permissions?: string[] | null): string {
  const hasMspAccess = MSP_AREA_PERMISSIONS.some(code =>
    permissionGranted(permissions ?? undefined, code)
  );
  return hasMspAccess ? MSP_LANDING_PATH : TENANT_LANDING_PATH;
}
