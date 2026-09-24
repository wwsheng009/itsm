
// test-coverage-guard: skip — ConfigProvider 语言切换的薄封装,providers.test.tsx 覆盖渲染。
// 迁移说明：`@ant-design/nextjs-registry`（Next SSR 样式提取）在纯客户端渲染下无需保留，已移除。
import { ConfigProvider, App } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import enUS from 'antd/locale/en_US';
import { useTheme, getAntdTheme } from '@/lib/design-system/theme';
import { useI18n } from '@/lib/i18n/useI18n';

interface AntdProviderProps {
  children: React.ReactNode;
}

export const AntdProvider: React.FC<AntdProviderProps> = ({ children }) => {
  const { isDark } = useTheme();
  const { language } = useI18n();
  const antdTheme = getAntdTheme(isDark);

  const antdLocale = language === 'en-US' ? enUS : zhCN;

  return (
    <ConfigProvider theme={antdTheme} locale={antdLocale}>
      <App>{children}</App>
    </ConfigProvider>
  );
};
