
/**
 * 历史“待审批”入口。
 *
 * 所有审批待办已在 /approvals 统一展示和处理；保留该重定向以兼容旧菜单、书签与通知链接，
 * 避免用户在两套审批列表之间看到不一致的待办。
 */

import { Navigate } from 'react-router';

export default function PendingApprovalsPage() {
  return <Navigate to="/approvals" replace />;
}
