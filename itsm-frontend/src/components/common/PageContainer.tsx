/**
 * @deprecated 请使用 BusinessPageTemplate 替代，此组件为历史遗留实现
 */
import React from 'react';
import { Layout, Typography, Breadcrumb, Space, Divider } from 'antd';
import type { BreadcrumbProps } from 'antd';

const { Content } = Layout;
const { Title } = Typography;

interface PageContainerProps {
  header?: {
    title?: React.ReactNode;
    breadcrumb?: {
      items?: BreadcrumbProps['items'];
    };
  };
  extra?: React.ReactNode;
  children?: React.ReactNode;
}

export const PageContainer: React.FC<PageContainerProps> = ({ header, extra, children }) => {
  return (
    // 背景必须跟随主题 token：硬编码 #fff 会导致暗色主题下页面仍为白底，
    // 而文字色已按暗色 token 变浅，形成白字白底不可读（黑洞/白洞问题）。
    <div
      style={{
        padding: '24px',
        background: 'var(--color-background-primary)',
        minHeight: '100%',
      }}
    >
      {(header?.title || header?.breadcrumb?.items) && (
        <>
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              marginBottom: '16px',
            }}
          >
            <div>
              {header?.breadcrumb?.items && (
                <Breadcrumb items={header.breadcrumb.items} style={{ marginBottom: '8px' }} />
              )}
              {header?.title && (
                <Title level={4} style={{ margin: 0 }}>
                  {header.title}
                </Title>
              )}
            </div>
            {extra && <Space>{extra}</Space>}
          </div>
          <Divider style={{ margin: '0 0 24px 0' }} />
        </>
      )}
      <Content>{children}</Content>
    </div>
  );
};
