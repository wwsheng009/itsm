
/**
 * 工作流列表页面
 * 重定向到 /workflow
 * 保留 /workflows 路由以兼容旧链接
 */

import { Navigate } from 'react-router';

export default function WorkflowsPage() {
  return <Navigate to="/workflow" replace />;
}
