
import React, { Suspense, useEffect, useState } from 'react';
import { Layout, App } from 'antd';
import { Outlet } from 'react-router';
import { bootstrapSession, type SessionStatus } from '@/lib/auth/session-bootstrap';
import RouteLoading from '@/components/common/RouteLoading';
import { Header } from '@/components/layout/Header';
import { Sidebar } from '@/components/layout/Sidebar';
import { httpClient } from '@/lib/api/http-client';
import { LAYOUT_CONFIG } from '@/config/layout.config';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { NetworkStatus } from '@/components/common/NetworkStatus';
import { AdminRouteGuard } from '@/components/common/AdminRouteGuard';
import { useLayoutStore } from '@/lib/store/layout-store';
import PageTransition from '@/components/common/PageTransition';
import { useAuthStore, useAuthStoreHydration } from '@/lib/store/auth-store';
import type { Tenant } from '@/lib/api/api-config';
import { useTheme } from '@/lib/design-system/theme';

const { Content } = Layout;

/**
 * 主应用布局
 * 包含 Header、Sidebar 和 Content 区域
 * 需要用户认证才能访问
 */
export default function MainLayout() {
  const { collapsed, setCollapsed } = useLayoutStore();
  const [mounted, setMounted] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const [sessionStatus, setSessionStatus] = useState<SessionStatus>(() =>
    useAuthStore.getState().isAuthenticated ? 'authenticated' : 'pending'
  );

  // 恢复持久化的 auth store，并同步租户上下文到内存
  useAuthStoreHydration();

  // 客户端挂载标记（避免首帧 SSR/CSR 不一致）
  useEffect(() => {
    setMounted(true);
  }, []);

  // 会话探活：逻辑抽到 src/lib/auth/session-bootstrap.ts，与路由守卫 RequireAuth 复用同一份实现
  useEffect(() => {
    let alive = true;
    bootstrapSession().then(status => {
      if (alive) setSessionStatus(status);
    });
    return () => {
      alive = false;
    };
    // 仅在布局挂载时检查一次；SPA 内部导航不重复检查，避免瞬时故障误踢用户
  }, []);

  // access_token 有效期 15 分钟：每 10 分钟主动刷新一次会话，
  // 防止 token 在操作期间过期导致被踢到 /login
  useEffect(() => {
    if (sessionStatus !== 'authenticated') return;
    const REFRESH_INTERVAL_MS = 10 * 60 * 1000;
    const timer = setInterval(() => {
      httpClient
        .refreshToken()
        .catch(() => {
          // 刷新失败不主动登出；下一次请求的 401 兜底流程会处理
        });
    }, REFRESH_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [sessionStatus]);

  // 响应式布局：在移动端自动折叠侧边栏；从移动端拉宽回桌面时恢复展开
  useEffect(() => {
    const handleResize = () => {
      const mobile = window.innerWidth < 768;
      setIsMobile(mobile);
      if (mobile) {
        setCollapsed(true);
      }
    };

    handleResize();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // 移动端 → 桌面端切换时恢复侧边栏展开（此前会永久停留在收起态）
  useEffect(() => {
    if (mounted && !isMobile) {
      setCollapsed(false);
    }
  }, [mounted, isMobile]);

  // 在移动端，点击内容区域时折叠侧边栏
  const handleContentClick = () => {
    if (isMobile && !collapsed) {
      setCollapsed(true);
    }
  };

  // 未挂载时显示 loading（避免服务端渲染问题）
  if (!mounted) {
    return null;
  }

  // 正在检查认证状态时显示 loading
  if (sessionStatus === 'pending') {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  // 未认证时不渲染布局（重定向由路由守卫 RequireAuth 处理）
  if (sessionStatus !== 'authenticated') {
    return null;
  }

  // 根据官方布局模式，使用单一容器控制侧边栏占位
  return (
    <ThemedMainLayout
      collapsed={collapsed}
      isMobile={isMobile}
      handleContentClick={handleContentClick}
    >
      <AdminRouteGuard>
        <PageTransition>
          <Suspense fallback={<RouteLoading />}>
            <Outlet />
          </Suspense>
        </PageTransition>
      </AdminRouteGuard>
    </ThemedMainLayout>
  );
}

function ThemedMainLayout({
  collapsed,
  isMobile,
  handleContentClick,
  children,
}: Readonly<{
  collapsed: boolean;
  isMobile: boolean;
  handleContentClick: () => void;
  children: React.ReactNode;
}>) {
  const { isDark } = useTheme();
  const setCollapsed = useLayoutStore(s => s.setCollapsed);

  return (
    <App>
      {/* Skip to main content link for accessibility */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:top-4 focus:left-4 focus:z-50 focus:bg-primary-600 focus:text-white focus:px-4 focus:py-2 focus:rounded-lg focus:shadow-lg focus:outline-none focus:ring-2 focus:ring-primary-400"
      >
        跳转到主要内容
      </a>
      <NetworkStatus />
      <Layout
        className="min-h-screen bg-[var(--color-background-primary)]"
        style={{
          paddingLeft: isMobile
            ? 0
            : collapsed
              ? LAYOUT_CONFIG.sider.collapsedWidth
              : LAYOUT_CONFIG.sider.width,
          transition: 'padding-left 0.2s ease',
        }}
      >
        {/* 侧边栏 */}
        <Sidebar
          collapsed={collapsed}
          onCollapse={setCollapsed}
          mobile={isMobile}
        />

        {/* 主区域 */}
        <Layout className="bg-[var(--color-background-primary)] min-h-screen">
          {/* 顶部导航栏 */}
          <Header collapsed={collapsed} onCollapse={setCollapsed} showBreadcrumb={true} />

          {/* 内容区域 */}
          <Content
            id="main-content"
            tabIndex={-1}
            onClick={handleContentClick}
            className="bg-[var(--color-background-primary)] w-auto min-w-0 max-w-full overflow-x-hidden shadow-none outline-none"
            style={{
              minHeight: LAYOUT_CONFIG.content.minHeight,
            }}
          >
            <div
              className="main-content"
              style={{
                padding: isMobile ? `${LAYOUT_CONFIG.content.paddingMobile}px` : '16px',
              }}
            >
              <PageTransition>{children}</PageTransition>
            </div>
          </Content>

          {/* 页脚（可选） */}
          <footer className="text-center p-4 bg-transparent text-gray-400 text-xs">
            AI-Native ITSM ©{new Date().getFullYear()} - AI驱动的IT服务管理系统
          </footer>
        </Layout>

      {/* 移动端遮罩层 */}
      {!collapsed && isMobile && (
        <div
          onClick={() => setCollapsed(true)}
          className="fixed inset-0 bg-black/45"
          style={{
            zIndex: LAYOUT_CONFIG.zIndex.sider - 1,
          }}
        />
      )}
      </Layout>
    </App>
  );
}
