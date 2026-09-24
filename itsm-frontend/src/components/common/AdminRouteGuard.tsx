import { useLocation, useNavigate } from 'react-router';

import { Button, Result } from 'antd';
import { usePermissions } from '@/lib/hooks/use-permissions';

export function AdminRouteGuard({ children }: { children: React.ReactNode }) {
  const pathname = useLocation().pathname;
  const { isAdmin } = usePermissions();
  const navigate = useNavigate();

  if (pathname.startsWith('/admin') && !isAdmin()) {
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
