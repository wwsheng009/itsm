/**
 * 登录默认落点测试（跨租户缺口治理）。
 *
 * 语义（双条件，与后端 IsMSP 同向）：
 * - 非服务商租户上下文（客户租户 / 未知类型）→ /dashboard（fail-closed）；
 * - 服务商租户 + 数据面操作码 msp_ticket:read → /msp/workbench；
 * - 平台通配 `*` / 仅区域码 msp:read / 无权限 → /dashboard。
 */
import {
  MSP_LANDING_PATH,
  resolveDefaultLanding,
  TENANT_LANDING_PATH,
} from '../landing';

describe('resolveDefaultLanding', () => {
  it('无权限 / 空权限 → 租户内总览', () => {
    expect(resolveDefaultLanding(undefined, 'msp_provider')).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding(null, 'msp_provider')).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding([], 'msp_provider')).toBe(TENANT_LANDING_PATH);
  });

  it('未知/缺失租户类型 → 租户内总览（fail-closed）', () => {
    expect(resolveDefaultLanding(['msp_ticket:read'])).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding(['msp_ticket:read'], null)).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding(['msp_ticket:read'], 'standard')).toBe(TENANT_LANDING_PATH);
  });

  it('客户租户即使残留 msp_ticket:read（角色克隆过度授权）→ 租户内总览', () => {
    expect(resolveDefaultLanding(['msp_ticket:read', 'ticket:read'], 'msp_customer')).toBe(
      TENANT_LANDING_PATH
    );
    expect(resolveDefaultLanding(['msp_ticket:read'], 'customer')).toBe(TENANT_LANDING_PATH);
  });

  it('服务商租户 + 数据面操作码 msp_ticket:read → 跨租户工作台', () => {
    expect(resolveDefaultLanding(['msp_ticket:read'], 'msp_provider')).toBe(MSP_LANDING_PATH);
    // 历史别名 `msp` 与后端 tenantmode 同语义
    expect(resolveDefaultLanding(['msp_ticket:read'], 'msp')).toBe(MSP_LANDING_PATH);
    // 服务商 msp_manager 基线（含多个 msp_* 码）
    expect(
      resolveDefaultLanding(
        [
          'msp:read',
          'msp:write',
          'msp_customer:read',
          'msp_ticket:read',
          'msp_allocation:read',
          'msp_report:read',
        ],
        'msp_provider'
      )
    ).toBe(MSP_LANDING_PATH);
  });

  it('服务商租户但仅区域码 / 平台通配 → 租户内总览', () => {
    expect(resolveDefaultLanding(['msp:read'], 'msp_provider')).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding(['*'], 'msp_provider')).toBe(TENANT_LANDING_PATH);
  });

  it('资源通配 msp_ticket:* 命中；msp:* / msp:admin 不蕴含', () => {
    expect(resolveDefaultLanding(['msp_ticket:*'], 'msp_provider')).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp:*'], 'msp_provider')).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding(['msp:admin'], 'msp_provider')).toBe(TENANT_LANDING_PATH);
  });

  it('落点为常量且互不相同（避免配置漂移）', () => {
    expect(TENANT_LANDING_PATH).toBe('/dashboard');
    expect(MSP_LANDING_PATH).toBe('/msp/workbench');
  });
});
