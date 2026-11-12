import { canWriteRow, hasTenantWideRole, normalizeMspRole } from '../row-scope';

describe('canWriteRow（行级预判，对齐后端 datascope.CanWriteResource）', () => {
  it('管理角色全租户可写', () => {
    for (const role of ['super_admin', 'admin', 'manager', 'sysadmin']) {
      expect(
        canWriteRow({ id: 99, role }, { requesterId: 6, assigneeId: 7 })
      ).toBe(true);
    }
  });

  it('创建人可写', () => {
    expect(
      canWriteRow({ id: 6, role: 'end_user' }, { requesterId: 6, assigneeId: 7 })
    ).toBe(true);
  });

  it('受理人可写', () => {
    expect(
      canWriteRow({ id: 7, role: 'agent' }, { requesterId: 6, assigneeId: 7 })
    ).toBe(true);
  });

  it('无关用户拒绝（权限码在但行级越权的「可点必败」形态）', () => {
    expect(
      canWriteRow({ id: 2, role: 'end_user' }, { requesterId: 6, assigneeId: 6 })
    ).toBe(false);
  });

  it('未知角色按非管理处理（安全收窄）', () => {
    expect(
      canWriteRow({ id: 2, role: 'mystery_role' }, { requesterId: 6, assigneeId: null })
    ).toBe(false);
  });

  it('身份未知 fail-closed', () => {
    expect(canWriteRow(null, { requesterId: 6, assigneeId: 6 })).toBe(false);
    expect(canWriteRow({ id: null, role: 'admin' }, { requesterId: 6 })).toBe(false);
    expect(canWriteRow({ id: 0, role: 'agent' }, { requesterId: 6 })).toBe(false);
  });

  it('资源缺失 fail-closed', () => {
    expect(canWriteRow({ id: 2, role: 'agent' }, null)).toBe(false);
    expect(canWriteRow({ id: 2, role: 'agent' }, {})).toBe(false);
  });
});

/**
 * MSP/provider 场景回归用例（2026-11-12）。
 *
 * 缺陷：/auth/me 对 provider 侧账号返回 role=end_user + mspRole=provider_admin，
 * 前端只比较 role，把「服务商经理」误判为只读 → 编辑/分配/解决/关闭按钮全部隐藏；
 * 而后端 datascope.ResolveForActor 会把 MSP 角色提级（provider_admin→msp_manager，
 * 规范 data_scope=all）并放行写操作。前端预判必须与之同口径。
 */
describe('canWriteRow（MSP/provider 场景，对齐后端 MSP 角色提级）', () => {
  it('服务商经理（role=end_user + mspRole=provider_admin）非创建人也可写', () => {
    expect(
      canWriteRow({ id: 2, role: 'end_user', mspRole: 'provider_admin' }, { requesterId: 6, assigneeId: null })
    ).toBe(true);
  });

  it('provider_agent → msp_tech 同样全量可写', () => {
    expect(
      canWriteRow({ id: 3, role: 'end_user', mspRole: 'provider_agent' }, { requesterId: 6 })
    ).toBe(true);
  });

  it('customer_user 不享受提级：非创建人仍拒绝', () => {
    expect(
      canWriteRow({ id: 8, role: 'end_user', mspRole: 'customer_user' }, { requesterId: 6 })
    ).toBe(false);
  });

  it('直接下发 msp_* 角色时同样全量可写', () => {
    for (const role of ['msp_viewer', 'msp_tech', 'msp_specialist', 'msp_manager', 'msp_admin']) {
      expect(hasTenantWideRole({ role })).toBe(true);
      expect(canWriteRow({ id: 9, role }, { requesterId: 6 })).toBe(true);
    }
  });

  it('roles[] 并集：多角色中任一为租户全量即可写（对齐后端「取最宽范围」）', () => {
    expect(
      canWriteRow({ id: 9, role: 'end_user', roles: ['msp_manager'] }, { requesterId: 6 })
    ).toBe(true);
    expect(
      canWriteRow({ id: 9, role: 'end_user', roles: ['end_user', 'viewer'] }, { requesterId: 6 })
    ).toBe(false);
  });

  it('角色归一：大小写与空白容错', () => {
    expect(normalizeMspRole(' Provider_Admin ')).toBe('msp_manager');
    expect(canWriteRow({ id: 9, role: 'end_user', mspRole: ' PROVIDER_ADMIN ' }, { requesterId: 6 })).toBe(true);
  });

  it('MSP 账号身份未知时仍 fail-closed', () => {
    expect(canWriteRow({ id: 0, role: 'end_user', mspRole: 'provider_admin' }, { requesterId: 6 })).toBe(false);
    expect(canWriteRow(null, { requesterId: 6 })).toBe(false);
  });
});
