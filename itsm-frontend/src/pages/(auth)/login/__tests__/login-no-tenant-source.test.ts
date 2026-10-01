import fs from 'node:fs';
import path from 'node:path';

/**
 * FE-A4 / LOGIN-R1：登录页 DOM 不得出现任何租户列表/选择器（客户关系保护）。
 * 渲染级断言受 React 19 + antd 兼容性影响（同目录 page.test.tsx 中渲染用例 skip），
 * 因此这里以源码级回归锁定"无租户面"契约。
 */
describe('登录页无租户选择面', () => {
  const source = fs.readFileSync(
    path.join(process.cwd(), 'src/pages/(auth)/login/index.tsx'),
    'utf8'
  );

  it('不渲染任何租户列表/选择器/切换控件', () => {
    expect(source).not.toMatch(
      /TenantSwitcher|tenant-list|tenantList|tenantSelect|TenantSelect|选择租户|租户列表|切换租户/
    );
    expect(source).not.toMatch(/tenants\s*\[\s*0\s*\]/);
  });

  it('不请求租户候选接口（/auth/tenants）', () => {
    expect(source).not.toMatch(/auth\/tenants|TenantAPI|useTenantStore/);
  });
});
