import { useEffect } from 'react';
import { Outlet } from 'react-router';

/**
 * 认证路由组布局
 * 用于登录、注册等认证相关页面
 * 提供简洁的全屏布局，无需导航栏和侧边栏
 *
 * 迁移说明（Next.js → Vite，FE-V1-23）：原 `src/app/(auth)/layout.tsx` 通过
 * Next metadata 设置标题，SPA 下等价为挂载时改写 `document.title`，
 * 卸载时还原根标题，避免离开认证路由后标题残留。
 */
const AUTH_GROUP_TITLE = '登录 - AI-Native ITSM';

export default function AuthLayout() {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = AUTH_GROUP_TITLE;
    return () => {
      document.title = previousTitle;
    };
  }, []);

  return (
    <div className="auth-layout">
      {/* 认证页面不需要额外的布局结构 */}
      {/* 每个认证页面自己控制全屏布局和样式 */}
      <Outlet />
    </div>
  );
}
