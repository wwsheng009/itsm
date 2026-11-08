import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Card,
  Col,
  Descriptions,
  Empty,
  Progress,
  Row,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  masterDataService,
  type Customer360,
  type Customer360Project,
  type Customer360WorkItem,
} from '@/lib/services/masterDataService';

const { Text, Title } = Typography;

const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';

const priorityLabel: Record<string, string> = {
  critical: '紧急',
  high: '高',
  medium: '中',
  low: '低',
};

const healthColor: Record<string, string> = {
  green: 'green',
  yellow: 'orange',
  red: 'red',
};

interface Customer360PanelProps {
  customerId: number;
}

export default function Customer360Panel({ customerId }: Customer360PanelProps) {
  const [data, setData] = useState<Customer360 | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setData(await masterDataService.customer360(customerId));
    } catch {
      setData(null);
    } finally {
      setLoading(false);
    }
  }, [customerId]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading && !data) {
    return (
      <div className="py-16 text-center">
        <Spin />
      </div>
    );
  }

  if (!data) {
    return <Empty description="客户 360 数据加载失败" />;
  }

  const { overview, serviceHealth, tickets, contracts, projects, assets, contacts } = data;
  const recentColumns: ColumnsType<Customer360WorkItem> = [
    {
      title: '类型',
      dataIndex: 'kind',
      width: 80,
      render: (value: string) => <Tag>{value === 'incident' ? '事件' : '工单'}</Tag>,
    },
    { title: '编号', dataIndex: 'number', width: 130 },
    { title: '标题', dataIndex: 'title' },
    {
      title: '优先级',
      dataIndex: 'priority',
      width: 90,
      render: (value: string) => priorityLabel[value] ?? value,
    },
    { title: '状态', dataIndex: 'status', width: 110 },
    { title: '创建时间', dataIndex: 'createdAt', width: 170, render: formatDate },
  ];
  const projectColumns: ColumnsType<Customer360Project> = [
    { title: '项目', dataIndex: 'name' },
    { title: '编码', dataIndex: 'code', width: 130 },
    {
      title: '健康度',
      dataIndex: 'health',
      width: 90,
      render: (value: string) => <Tag color={healthColor[value] ?? 'default'}>{value}</Tag>,
    },
    {
      title: '进度',
      dataIndex: 'progress',
      width: 130,
      render: (value: number) => <Progress percent={value} size="small" />,
    },
    { title: '风险', dataIndex: 'openRisks', width: 70 },
    { title: '里程碑风险', dataIndex: 'milestoneRisks', width: 100 },
  ];

  return (
    <div className="space-y-4">
      {contracts.renewalAlert && (
        <Alert
          type="warning"
          showIcon
          message="合同续约提醒"
          description={`有 ${contracts.expiringSoon} 份合同将在 30 天内到期，请及时安排续约。`}
        />
      )}
      <Row gutter={[12, 12]}>
        <Col xs={12} sm={8} lg={4}>
          <Card size="small">
            <Statistic title="质量分" value={overview.dataQualityScore} />
          </Card>
        </Col>
        <Col xs={12} sm={8} lg={4}>
          <Card size="small">
            <Statistic title="有效合同" value={contracts.activeCount} />
          </Card>
        </Col>
        <Col xs={12} sm={8} lg={4}>
          <Card size="small">
            <Statistic title="未结工单" value={tickets.ticketOpen} />
          </Card>
        </Col>
        <Col xs={12} sm={8} lg={4}>
          <Card size="small">
            <Statistic title="未结事件" value={tickets.incidentOpen} />
          </Card>
        </Col>
        <Col xs={12} sm={8} lg={4}>
          <Card size="small">
            <Statistic title="在途项目" value={projects.inFlight} />
          </Card>
        </Col>
        <Col xs={12} sm={8} lg={4}>
          <Card size="small">
            <Statistic title="资产 / CI" value={`${assets.assetCount} / ${assets.ciCount}`} />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8}>
          <Card size="small" title="服务健康（近 30 天）">
            <Space orientation="vertical" size="middle" className="w-full">
              <Space align="center" size="large">
                <Progress
                  type="circle"
                  size={88}
                  percent={Math.round(serviceHealth.complianceRate ?? 0)}
                  status={
                    serviceHealth.complianceRate != null && serviceHealth.complianceRate < 90
                      ? 'exception'
                      : 'normal'
                  }
                />
                <div>
                  <Text type="secondary">SLA 达标率</Text>
                  <Title level={4} className="!mb-0">
                    {serviceHealth.complianceRate == null
                      ? '暂无数据'
                      : `${serviceHealth.complianceRate.toFixed(1)}%`}
                  </Title>
                </div>
              </Space>
              <Descriptions size="small" column={1}>
                <Descriptions.Item label="30 天违约">
                  {serviceHealth.violations30d}
                </Descriptions.Item>
                <Descriptions.Item label="未处置违约">
                  {serviceHealth.openViolations}
                </Descriptions.Item>
                <Descriptions.Item label="超期工单 / 事件">
                  {tickets.ticketOverdue} / {tickets.incidentOverdue}
                </Descriptions.Item>
              </Descriptions>
              {serviceHealth.metrics.length > 0 && (
                <Space wrap size={[4, 4]}>
                  {serviceHealth.metrics.slice(0, 6).map(metric => (
                    <Tag key={`${metric.metricType}-${metric.measuredAt}`}>
                      {metric.metricName || metric.metricType}: {metric.value}
                      {metric.unit}
                    </Tag>
                  ))}
                </Space>
              )}
            </Space>
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card size="small" title="联系人与关系">
            <Descriptions size="small" column={1}>
              <Descriptions.Item label="联系人">
                {contacts.primary
                  ? `${contacts.primary.name} · ${contacts.primary.email || contacts.primary.phone || '-'}`
                  : `${contacts.total} 人`}
              </Descriptions.Item>
              <Descriptions.Item label="升级联系人">
                {contacts.escalation
                  ? `${contacts.escalation.name} · ${contacts.escalation.email || contacts.escalation.phone || '-'}`
                  : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="集团关系">
                上级 {data.relationships.parents} / 下级 {data.relationships.children} / 伙伴{' '}
                {data.relationships.partners}
              </Descriptions.Item>
              <Descriptions.Item label="关联租户">
                {data.tenants.total}（主 {data.tenants.primary}，待开通{' '}
                {data.tenants.provisioning}）
              </Descriptions.Item>
            </Descriptions>
            <Space wrap size={[4, 4]} className="mt-2">
              {data.tenants.items.map(item => (
                <Tag key={item.linkId} color={item.linkType === 'primary' ? 'blue' : 'default'}>
                  {item.tenantName || `#${item.linkedTenantId}`} · {item.provisionStatus}
                </Tag>
              ))}
            </Space>
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card size="small" title="合同与资产">
            <Descriptions size="small" column={1}>
              <Descriptions.Item label="当前合同">
                {contracts.current?.contractNumber ?? '-'}
              </Descriptions.Item>
              <Descriptions.Item label="剩余天数">
                {contracts.current?.remainingDays != null
                  ? `${contracts.current.remainingDays} 天`
                  : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="关键资产">
                {assets.keyAssets.length ? assets.keyAssets.join('、') : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="关键 CI">
                {assets.keyCIs.length ? assets.keyCIs.join('、') : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="外部标识">
                {data.externalIds.length
                  ? data.externalIds
                      .map(item => `${item.systemCode}:${item.externalId}`)
                      .join('、')
                  : '-'}
              </Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
      </Row>

      <Card size="small" title="近期工单 / 事件">
        <Table
          rowKey={record => `${record.kind}-${record.id}`}
          size="small"
          columns={recentColumns}
          dataSource={tickets.recent}
          pagination={false}
          locale={{ emptyText: '暂无工单或事件' }}
        />
      </Card>

      <Card size="small" title="关联项目">
        <Table
          rowKey="id"
          size="small"
          columns={projectColumns}
          dataSource={projects.items}
          pagination={false}
          locale={{ emptyText: '暂无在途项目' }}
        />
      </Card>
    </div>
  );
}
