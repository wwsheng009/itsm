import { useLocation, useNavigate } from 'react-router';

import { Button, Result } from 'antd';
import { ADMIN_AREA_PERMISSIONS } from '@/routes/area-permissions';
import { useAuthStore } from '@/lib/store/auth-store';

/**
 * /admin 区域兜底门禁（与 routes/area-permissions 单源同步，B6 并集语义）。
 *
 * 2026-10-14 验收修复：原实现用 role 判定（admin/super_admin），会把仅持细码的
 * 专职管理员——如 provider 持 sla:contract:* 访问 /admin/sla-contracts——整区拦下，
 * 与 routes/index.tsx 区域门禁 / Sidebar 的 ADMIN_AREA_PERMISSIONS 并集不一致。
 * 改为同一权限源：任一区域细码命中即放行；super_admin 经 `*` 通配命中。
 */
export function AdminRouteGuard({ children }: { children: React.ReactNode }) {
  const pathname = useLocation().pathname;
  const hasPermission = useAuthStore(state => state.hasPermission);
  const navigate = useNavigate();

  const allowed = ADMIN_AREA_PERMISSIONS.some(code => hasPermission(code));
  if (pathname.startsWith('/admin') && !allowed) {
    return (
      <Result
        status="403"
        title="403"
        subTitle="抱歉，您没有权限访问此页面。"
        extra={
          <Button type="primary" onClick={() => navigate('/')}>
            返回首页
          </Button>
        }
      />
    );
  }

  return <>{children}</>;
}
