/**
 * 登录默认落点测试（跨租户缺口治理）。
 *
 * 语义：
 * - 无 MSP 权限 → /dashboard（租户内总览）；
 * - 命中任一 MSP 区域权限码（含通配/resource:admin 蕴含）→ /msp/workbench。
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

  it('MSP 细码（任一）→ 跨租户工作台', () => {
    expect(resolveDefaultLanding(['msp:read'])).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp_ticket:read'])).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp_customer:read'])).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp_allocation:read'])).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp_report:read'])).toBe(MSP_LANDING_PATH);
  });

  it('通配符与资源管理员蕴含同样命中', () => {
    expect(resolveDefaultLanding(['*'])).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp:*'])).toBe(MSP_LANDING_PATH);
    expect(resolveDefaultLanding(['msp:admin'])).toBe(MSP_LANDING_PATH);
  });

  it('落点为常量且互不相同（避免配置漂移）', () => {
    expect(TENANT_LANDING_PATH).toBe('/dashboard');
    expect(MSP_LANDING_PATH).toBe('/msp/workbench');
  });
});
