
import { useEffect } from 'react';
import { useTheme } from '@/lib/design-system/theme';

/**
 * 将主题上下文中的 isDark 状态同步到 <html> 上的 .dark 类，
 * 让 globals.css 中 .dark { ... } 变量能即时生效。
 *
 * 同时设置 color-scheme 以影响浏览器原生滚动条/表单控件。
 */
export const ThemeHtmlClassSync: React.FC = () => {
  const { isDark } = useTheme();

  useEffect(() => {
    if (typeof document === 'undefined') return;
    const root = document.documentElement;
    // 必须成对维护 .dark / .light：
    // 样式层（antd-layout-overrides.css、theme-variables.css、globals.css）存在
    // @media (prefers-color-scheme: dark) { :root:not(.light) ... } 这类「跟随系统」兜底，
    // 若亮色态只移除 .dark 而不写 .light，OS 为深色时该兜底会命中，
    // 导致亮色主题下布局壳/顶栏被刷成 #141414 / #1f1f1f（黑底白卡）。
    if (isDark) {
      root.classList.add('dark');
      root.classList.remove('light');
      root.style.colorScheme = 'dark';
    } else {
      root.classList.remove('dark');
      root.classList.add('light');
      root.style.colorScheme = 'light';
    }
  }, [isDark]);

  return null;
};
