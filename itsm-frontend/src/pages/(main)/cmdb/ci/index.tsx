
// 历史路由兼容：早期版本使用单数 /cmdb/ci 作为 CI 清单入口，
// 现行规范统一为复数 /cmdb/cis。详见菜单配置 menu-config.ts。
// 注意：本路由仅作为兼容入口，不再渲染任何业务 UI。

import { Navigate } from 'react-router';

export default function LegacyCIPage() {
  return <Navigate to="/cmdb/cis" replace />;
}
