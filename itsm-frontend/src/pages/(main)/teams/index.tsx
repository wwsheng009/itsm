import { useNavigate } from 'react-router';

import { useEffect } from 'react';

/**
 * 团队管理页面
 * 重定向到 /admin/teams
 * 保留 /teams 路由以兼容旧链接
 */
export default function TeamsPage() {
  const navigate = useNavigate();

  useEffect(() => {
    navigate('/admin/teams', { replace: true });
  }, [navigate]);

  return null;
}
