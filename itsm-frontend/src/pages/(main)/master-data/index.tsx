import { useEffect, useState } from 'react';
import { Button, Card, Col, Row, Space, Statistic, Tag, Typography } from 'antd';
import { FileText, Globe, Users } from 'lucide-react';
import { useNavigate } from 'react-router';
import { useCan } from '@/components/auth/PermissionGate';
import { masterDataService } from '@/lib/services/masterDataService';

const { Title, Text } = Typography;

interface Counts {
  customers: number | null;
  contracts: number | null;
  sources: number | null;
}

/**
 * 主数据模块入口。
 *
 * 客户、支持合同和来源组织是跨邮件报障复用的运营主数据，不再挂在
 * `/email-intake` 下；本页作为模块总览与入口。
 */
export default function MasterDataHomePage() {
  const navigate = useNavigate();
  const canCustomers = useCan(['customer_master:read']);
  const canContracts = useCan(['support_contract:read']);
  const canSources = useCan(['customer_master:read']);
  const [counts, setCounts] = useState<Counts>({
    customers: null,
    contracts: null,
    sources: null,
  });

  useEffect(() => {
    let alive = true;
    const tasks: Promise<void>[] = [];

    if (canCustomers) {
      tasks.push(
        masterDataService
          .customers()
          .then(result => {
            if (alive) setCounts(prev => ({ ...prev, customers: result.total }));
          })
          .catch(() => undefined)
      );
    }
    if (canContracts) {
      tasks.push(
        masterDataService
          .contracts()
          .then(result => {
            if (alive) setCounts(prev => ({ ...prev, contracts: result.total }));
          })
          .catch(() => undefined)
      );
    }
    if (canSources) {
      tasks.push(
        masterDataService
          .sourceOrganizations()
          .then(result => {
            if (alive) setCounts(prev => ({ ...prev, sources: result.total }));
          })
          .catch(() => undefined)
      );
    }

    void Promise.allSettled(tasks);
    return () => {
      alive = false;
    };
  }, [canContracts, canCustomers, canSources]);

  const cards = [
    canCustomers && {
      key: 'customers',
      title: '客户主数据',
      description: '维护客户名称、简称、别名、历史名称、关联客户租户与服务分支。',
      icon: <Users className="h-5 w-5" />,
      count: counts.customers,
      path: '/master-data/customers',
      action: '进入客户主数据',
    },
    canContracts && {
      key: 'contracts',
      title: '支持合同',
      description: '维护客户支持合同、有效期、分支归属和外部合同号映射。',
      icon: <FileText className="h-5 w-5" />,
      count: counts.contracts,
      path: '/master-data/contracts',
      action: '进入支持合同',
    },
    canSources && {
      key: 'sources',
      title: '来源组织',
      description: '维护邮件与外部渠道的识别来源、邮箱地址和域名。',
      icon: <Globe className="h-5 w-5" />,
      count: counts.sources,
      path: '/master-data/sources',
      action: '进入来源组织',
    },
  ].filter(Boolean) as Array<{
    key: string;
    title: string;
    description: string;
    icon: React.ReactNode;
    count: number | null;
    path: string;
    action: string;
  }>;

  return (
    <div className="space-y-5">
      <div>
        <Space align="center" wrap>
          <Title level={2} className="!mb-0">
            主数据管理
          </Title>
          <Tag color="blue">Master Data</Tag>
        </Space>
        <Text type="secondary" className="block mt-2">
          客户、支持合同和来源组织是服务运营的共享主数据，独立于邮件报障流程管理。
        </Text>
      </div>

      <Row gutter={[16, 16]}>
        {cards.map(card => (
          <Col key={card.key} xs={24} lg={8}>
            <Card
              className="h-full"
              title={
                <Space>
                  {card.icon}
                  <span>{card.title}</span>
                </Space>
              }
              extra={<Tag>{card.key}</Tag>}
            >
              <Statistic
                title="当前记录数"
                value={card.count ?? '-'}
                loading={card.count === null}
              />
              <Text type="secondary" className="block mt-3 min-h-12">
                {card.description}
              </Text>
              <Button
                type="primary"
                className="mt-3"
                onClick={() => navigate(card.path)}
              >
                {card.action}
              </Button>
            </Card>
          </Col>
        ))}
      </Row>
    </div>
  );
}
