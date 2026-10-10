import { filterMenusByPermission } from '../menu-permission-filter';
import type { MenuItem } from '../menu-config';

const item = (partial: Partial<MenuItem> & { key: string }): MenuItem => ({
  label: partial.key,
  ...partial,
});

describe('filterMenusByPermission（菜单挂载点权限复核）', () => {
  it('未命中菜单码不挂载（system_config:write 不能放行 system:write）', () => {
    const menus = [item({ key: '/admin/tenants', permission: 'system:write' })];
    expect(filterMenusByPermission(menus, ['system_config:write'])).toEqual([]);
  });

  it('命中菜单码挂载；全局通配放行', () => {
    const menus = [item({ key: '/admin/tenants', permission: 'system:write' })];
    expect(filterMenusByPermission(menus, ['system:write'])).toHaveLength(1);
    expect(filterMenusByPermission(menus, ['*'])).toHaveLength(1);
  });

  it('资源 admin 蕴含具体动作（与后端一致）', () => {
    const menus = [item({ key: '/admin/tenants', permission: 'system:write' })];
    expect(filterMenusByPermission(menus, ['system:admin'])).toHaveLength(1);
  });

  it('无 permission_code 的公共项保留（含权限列表为空）', () => {
    const menus = [item({ key: '/dashboard' })];
    expect(filterMenusByPermission(menus, [])).toHaveLength(1);
    expect(filterMenusByPermission(menus, undefined)).toHaveLength(1);
  });

  it('递归过滤子项；容器自身未命中但子项存活时保留', () => {
    const menus = [
      item({
        key: '/admin',
        permission: 'system:write',
        children: [
          item({ key: '/admin/users', permission: 'user:read' }),
          item({ key: '/admin/tenants', permission: 'system:write' }),
        ],
      }),
    ];
    const out = filterMenusByPermission(menus, ['user:read']);
    expect(out).toHaveLength(1);
    expect(out[0].children?.map(child => child.key)).toEqual(['/admin/users']);
  });

  it('容器与子项均未命中时整体不挂载', () => {
    const menus = [
      item({
        key: '/admin',
        permission: 'system:write',
        children: [item({ key: '/admin/users', permission: 'user:read' })],
      }),
    ];
    expect(filterMenusByPermission(menus, ['ticket:read'])).toEqual([]);
  });
});
