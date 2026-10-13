/**
 * 登录默认落点测试（跨租户缺口治理）。
 *
 * 语义：
 * - 无权限 / 仅区域码（msp:read，客户租户 admin 也持有）/ 平台通配 `*` → /dashboard；
 * - 显式持有工作台数据面操作码 msp_ticket:read（服务商内置角色基线）→ /msp/workbench。
 */
import {
  MSP_LANDING_PATH,
  resolveDefaultLanding,
  TENANT_LANDING_PATH,
} from '../landing';

describe('resolveDefaultLanding', () => {
  it('无权限 / 空权限 → 租户内总览', () => {
    expect(resolveDefaultLanding(undefined)).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding(null)).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding([])).toBe(TENANT_LANDING_PATH);
  });

  it('无关业务权限 → 租户内总览', () => {
    expect(resolveDefaultLanding(['ticket:read', 'dashboard:read'])).toBe(TENANT_LANDING_PATH);
  });

  it('仅区域码 msp:read（客户租户 admin 基线）→ 租户内总览', () => {
    expect(resolveDefaultLanding(['msp:read', 'ticket:read', 'dashboard:read'])).toBe(
      TENANT_LANDING_PATH
    );
  });

  it('平台超级管理员通配 `*` → 租户内总览（治理视角）', () => {
    expect(resolveDefaultLanding(['*'])).toBe(TENANT_LANDING_PATH);
  });

  it('工作台数据面操作码 msp_ticket:read → 跨租户工作台', () => {
    expect(resolveDefaultLanding(['msp_ticket:read'])).toBe(MSP_LANDING_PATH);
    // 服务商 msp_manager 基线（含多个 msp_* 码）
    expect(
      resolveDefaultLanding([
        'msp:read',
        'msp:write',
        'msp_customer:read',
        'msp_ticket:read',
        'msp_allocation:read',
        'msp_report:read',
      ])
    ).toBe(MSP_LANDING_PATH);
  });

  it('资源通配 msp_ticket:* 同样命中；msp:* / msp:admin 不蕴含', () => {
    expect(resolveDefaultLanding(['msp_ticket:*'])).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp:*'])).toBe(TENANT_LANDING_PATH);
    expect(resolveDefaultLanding(['msp:admin'])).toBe(TENANT_LANDING_PATH);
  });

  it('落点为常量且互不相同（避免配置漂移）', () => {
    expect(TENANT_LANDING_PATH).toBe('/dashboard');
    expect(MSP_LANDING_PATH).toBe('/msp/workbench');
  });
});
