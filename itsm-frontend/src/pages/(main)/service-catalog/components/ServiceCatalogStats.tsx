
import React from 'react';
import { Card, Col, Row, Statistic } from 'antd';
import { Boxes, HardDrive, Network, ShieldCheck, UserCog } from 'lucide-react';
import { useI18n } from '@/lib/i18n';

interface ServiceCatalogStatsProps {
  stats: {
    total: number;
    categories: Array<{ name: string; count: number }>;
  };
}

export const ServiceCatalogStats: React.FC<ServiceCatalogStatsProps> = ({ stats }) => {
  const { t } = useI18n();
  const categoryIcons = [Boxes, UserCog, Network, ShieldCheck];
  const categoryColors = ['#1890ff', '#722ed1', '#13c2c2', '#52c41a'];

  return (
    <div className="mb-6">
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card className="rounded-lg shadow-sm border border-gray-200">
            <Statistic
              title={t('serviceCatalog.totalServices')}
              value={stats.total}
              prefix={<HardDrive size={16} className="text-blue-500" />}
            />
          </Card>
        </Col>
        {stats.categories.map((category, index) => {
          const Icon = categoryIcons[index % categoryIcons.length];
          return (
            <Col key={category.name} xs={24} sm={12} lg={6}>
              <Card className="rounded-lg shadow-sm border border-gray-200">
                <Statistic
                  title={category.name}
                  value={category.count}
                  styles={{ content: { color: categoryColors[index % categoryColors.length] } }}
                  prefix={<Icon size={16} />}
                />
              </Card>
            </Col>
          );
        })}
      </Row>
    </div>
  );
};
