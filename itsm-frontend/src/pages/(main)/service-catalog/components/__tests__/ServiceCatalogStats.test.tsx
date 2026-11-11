import React from 'react';
import { render, screen } from '@/lib/test-utils';
import { ServiceCatalogStats } from '../ServiceCatalogStats';

describe('ServiceCatalogStats · 动态分类口径', () => {
  it('顶部卡片显示真实分类，且不再把终端支持/网络与访问塞进账号/安全桶', () => {
    render(
      <ServiceCatalogStats
        stats={{
          total: 3,
          categories: [
            { name: '终端支持', count: 1 },
            { name: '网络与访问', count: 1 },
            { name: '账号与权限', count: 1 },
          ],
        }}
      />,
    );

    expect(screen.getByText('服务总数')).toBeInTheDocument();
    for (const category of ['终端支持', '网络与访问', '账号与权限']) {
      expect(screen.getByText(category)).toBeInTheDocument();
    }
    expect(screen.queryByText('云资源服务')).toBeNull();
    expect(screen.queryByText('安全服务')).toBeNull();
    expect(screen.getAllByText('1').length).toBe(3);
    expect(screen.getByText('3')).toBeInTheDocument();
  });
});