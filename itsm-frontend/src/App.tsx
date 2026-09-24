import { Outlet } from 'react-router';
import { ThemeProvider, ThemeConfig } from '@/lib/design-system/theme';
import { AntdProvider } from '@/lib/providers/AntdProvider';
import { QueryProvider } from '@/lib/providers/QueryProvider';
import { RecentVisitTracker } from '@/components/layout/RecentVisitTracker';
import { ThemeHtmlClassSync } from '@/components/layout/ThemeHtmlClassSync';
import { DayjsLocaleSync } from '@/components/layout/DayjsLocaleSync';
import ErrorBoundary from '@/components/common/ErrorBoundary';
import GlobalShortcutProvider from '@/components/common/GlobalShortcutProvider';

const bodyFontFamily = `var(--font-noto-sans-sc), var(--font-inter), -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol', 'Noto Color Emoji'`;

/**
 * 全局 Provider 树（迁移自 src/app/layout.tsx）。
 *
 * 顺序必须与迁移前一致：
 * ThemeProvider → (RecentVisitTracker / ThemeHtmlClassSync / DayjsLocaleSync)
 * → ThemeConfig → AntdProvider → QueryProvider → GlobalShortcutProvider → ErrorBoundary → 路由内容。
 *
 * 注意：`@ant-design/nextjs-registry`（Next SSR 样式提取）已移除——纯客户端渲染下仅保留 antd ConfigProvider。
 */
export default function RootProviders() {
  return (
    <ThemeProvider>
      <RecentVisitTracker />
      <ThemeHtmlClassSync />
      <DayjsLocaleSync />
      <ThemeConfig>
        <AntdProvider>
          <QueryProvider>
            <GlobalShortcutProvider>
              <ErrorBoundary>
                <div className="app-root antialiased" style={{ fontFamily: bodyFontFamily }}>
                  <Outlet />
                </div>
              </ErrorBoundary>
            </GlobalShortcutProvider>
          </QueryProvider>
        </AntdProvider>
      </ThemeConfig>
    </ThemeProvider>
  );
}
