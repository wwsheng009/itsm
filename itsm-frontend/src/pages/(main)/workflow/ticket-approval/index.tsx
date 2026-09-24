
/**
 * 旧版”工单审批”入口，已收敛到审批中心 /approvals。
 * 旧链接（菜单、收藏、外部跳转）通过本重定向继续可用。
 */

import { Navigate } from 'react-router';

export default function TicketApprovalPage() {
  return <Navigate to="/approvals" replace />;
}
