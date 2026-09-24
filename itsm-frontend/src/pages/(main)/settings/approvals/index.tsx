import { useNavigate } from 'react-router';

import { useEffect } from 'react';

/**
 * 审批配置设置页面
 * 重定向到管理后台的审批配置页面
 */
export default function SettingsApprovalsPage() {
  const navigate = useNavigate();

  useEffect(() => {
    // 审批配置已迁移到 BPMN 工作流管理，跳转到工作流管理页面
    navigate('/admin/workflows', { replace: true });
  }, [navigate]);

  return null;
}