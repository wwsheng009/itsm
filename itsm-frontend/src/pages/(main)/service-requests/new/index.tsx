import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import {
  Alert,
  Breadcrumb,
  Button,
  Card,
  Col,
  Empty,
  Input,
  Row,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd';
import { ArrowLeft, Search, Send } from 'lucide-react';
import { ServiceCatalogApi } from '@/lib/api/service-catalog-api';
import { ServiceStatus } from '@/types/service-catalog';
import type { ServiceItem } from '@/types/service-catalog';

const { Title, Text, Paragraph } = Typography;

/**
 * 新建服务请求。
 *
 * 服务请求必须基于「服务目录项」发起，因此本页负责选择服务，
 * 选中后跳转到 /service-catalog/request/:id 填写申请表单并提交审批。
 *
 * 修复背景：/service-requests/new 原先没有独立路由，被 /service-requests/:id
 * 捕获后以 id="new" 请求详情接口，导致页面无法加载、也无法创建。
 */
export default function ServiceRequestNewPage() {
  const navigate = useNavigate();
  const [services, setServices] = useState<ServiceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [keyword, setKeyword] = useState('');

  const loadServices = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await ServiceCatalogApi.getServices({
        page: 1,
        pageSize: 100,
        status: ServiceStatus.PUBLISHED,
      });
      setServices(data.services || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : '服务目录加载失败，请稍后重试');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadServices();
  }, [loadServices]);

  const filteredServices = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    if (!q) return services;
    return services.filter(service =>
      `${service.name} ${service.shortDescription || ''} ${String(service.category || '')}`
        .toLowerCase()
        .includes(q)
    );
  }, [services, keyword]);

  return (
    <div className="p-6 max-w-6xl mx-auto">
      <Breadcrumb
        className="mb-4"
        items={[
          { title: <a onClick={() => navigate('/service-requests')}>服务请求</a> },
          { title: '新建服务请求' },
        ]}
      />

      <Space className="mb-4" align="center">
        <Button icon={<ArrowLeft />} onClick={() => navigate('/service-requests')}>
          返回
        </Button>
        <Title level={3} style={{ margin: 0 }}>
          新建服务请求
        </Title>
      </Space>

      <Alert
        type="info"
        showIcon
        className="mb-4"
        message="请选择需要申请的服务"
        description="服务请求需基于服务目录项发起。选择服务后将进入申请表单，填写申请理由并提交审批。"
      />

      <Card>
        <Input
          allowClear
          prefix={<Search />}
          placeholder="搜索服务名称、描述或分类"
          value={keyword}
          onChange={e => setKeyword(e.target.value)}
          className="mb-4 max-w-md"
        />

        {error && (
          <Alert
            type="error"
            showIcon
            className="mb-4"
            message={error}
            action={
              <Button size="small" onClick={loadServices}>
                重试
              </Button>
            }
          />
        )}

        <Spin spinning={loading}>
          {!loading && filteredServices.length === 0 ? (
            <Empty description={keyword ? '没有匹配的服务' : '暂无已发布的服务目录项'}>
              <Button type="primary" onClick={() => navigate('/service-catalog')}>
                前往服务目录
              </Button>
            </Empty>
          ) : (
            <Row gutter={[16, 16]}>
              {filteredServices.map(service => (
                <Col key={service.id} xs={24} sm={12} lg={8}>
                  <Card
                    className="h-full rounded-lg shadow-sm"
                    title={service.name}
                    extra={<Tag color="blue">{String(service.category || '未分类')}</Tag>}
                    actions={[
                      <Button
                        key="apply"
                        type="primary"
                        icon={<Send />}
                        onClick={() => navigate(`/service-catalog/request/${service.id}`)}
                      >
                        申请
                      </Button>,
                    ]}
                  >
                    <Paragraph type="secondary" ellipsis={{ rows: 2 }} className="!mb-0">
                      {service.shortDescription || '暂无服务说明'}
                    </Paragraph>
                    {typeof service.requestCount === 'number' && service.requestCount > 0 && (
                      <Text type="secondary" className="text-xs">
                        已有 {service.requestCount} 次申请
                      </Text>
                    )}
                  </Card>
                </Col>
              ))}
            </Row>
          )}
        </Spin>
      </Card>
    </div>
  );
}
