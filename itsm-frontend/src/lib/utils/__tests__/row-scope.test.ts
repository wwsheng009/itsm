import { canWriteRow } from '../row-scope';

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
