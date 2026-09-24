// test-coverage-guard: skip — 纯展示型降级 UI，错误边界行为由 ErrorBoundary.test.tsx 覆盖。
import { useNavigate, useRouteError } from 'react-router';

import React, { useEffect } from 'react';
import { Result, Button } from 'antd';
import { LayoutDashboard, RotateCcw } from 'lucide-react';

/**
 * 路由级错误边界（react-router `errorElement`）
 * 捕获子路由中的运行时错误并显示降级 UI。
 *
 * 迁移说明（Next.js → Vite/React Router）：
 * - 旧实现是 Next 的 `error.tsx` 契约（`{ error, reset }` props）；
 * - 新实现通过 `useRouteError()` 读取错误，`reset` 回调不存在，重试改为整页 reload。
 */
export default function RouteError() {
  const error = useRouteError();
  const navigate = useNavigate();

  const message =
    error instanceof Error ? error.message : typeof error === 'string' ? error : '';

  useEffect(() => {
    // 记录错误到控制台（后续可替换为 logger 服务）
    console.error('[RouteError]', error);
  }, [error]);

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <Result
        status="500"
        title="页面出错了"
        subTitle={
          message
            ? `抱歉，页面遇到了一个意外错误：${message}`
            : '抱歉，页面遇到了一个意外错误。请尝试重试或返回仪表盘。'
        }
        extra={[
          <Button
            key="retry"
            type="primary"
            icon={<RotateCcw />}
            onClick={() => window.location.reload()}
          >
            重试
          </Button>,
          <Button
            key="dashboard"
            icon={<LayoutDashboard />}
            onClick={() => navigate('/dashboard')}
          >
            返回仪表盘
          </Button>,
        ]}
      />
    </div>
  );
}
