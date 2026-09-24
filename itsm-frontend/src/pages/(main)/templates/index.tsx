
/**
 * 模板管理唯一入口为 /tickets/templates，本路由仅做兼容跳转。
 */

import { Navigate } from 'react-router';

export default function TemplatesRedirectPage() {
  return <Navigate to="/tickets/templates" replace />;
}
