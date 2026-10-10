import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { App, Button, Card, Result, Spin } from 'antd';
import { useCan } from '@/components/auth/PermissionGate';
import SLADefinitionForm, {
  type SLADefinitionFormValues,
} from '@/components/sla/SLADefinitionForm';
import { SLAApi, type SLADefinition } from '@/lib/api/sla-api';

/**
 * 编辑 SLA 定义（/sla/definitions/:id/edit）。
 * 写面：PUT /api/v1/sla/definitions/:id（router/sla_routes.go:32，sla:write）。
 * 路由级门禁见 routes/route-permissions.ts；此处为组件级双保险。
 */
export default function SLADefinitionEditPage() {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const { message } = App.useApp();
  const canWriteSla = useCan(['sla:write']);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [definition, setDefinition] = useState<SLADefinition | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const loadDefinition = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setLoadError('');
    try {
      setDefinition(await SLAApi.getSLADefinition(Number(id)));
    } catch (error) {
      console.error('Failed to load SLA definition:', error);
      setLoadError('加载 SLA 定义失败，可能已被删除');
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    void loadDefinition();
  }, [loadDefinition]);

  const handleSubmit = async (values: SLADefinitionFormValues) => {
    if (!id) return;
    setSubmitting(true);
    try {
      await SLAApi.updateSLADefinition(Number(id), values);
      message.success('SLA 定义更新成功');
      navigate(`/sla/definitions/${id}`);
    } catch (error) {
      console.error('Failed to update SLA definition:', error);
      message.error('保存失败，请检查必填项');
    } finally {
      setSubmitting(false);
    }
  };

  if (!canWriteSla) {
    return (
      <Result
        status="403"
        title="无权限"
        subTitle="编辑 SLA 定义需要 sla:write 权限"
        extra={
          <Button type="primary" onClick={() => navigate('/sla')}>
            返回 SLA 工作台
          </Button>
        }
      />
    );
  }

  if (loading) {
    return (
      <div className="flex min-h-[400px] items-center justify-center">
        <Spin size="large" />
      </div>
    );
  }

  if (loadError || !definition) {
    return (
      <Result
        status="404"
        title="未找到"
        subTitle={loadError || 'SLA 定义不存在'}
        extra={
          <Button type="primary" onClick={() => navigate('/sla')}>
            返回 SLA 工作台
          </Button>
        }
      />
    );
  }

  return (
    <div className="p-6">
      <div className="mb-6">
        <h1 className="mb-1 text-2xl font-bold">编辑 SLA 定义</h1>
        <p className="m-0 text-sm text-gray-500">{definition.name}</p>
      </div>
      <Card>
        <SLADefinitionForm
          submitText="保存"
          submitting={submitting}
          initialValues={{
            name: definition.name,
            description: definition.description,
            serviceType: definition.serviceType,
            priority: definition.priority,
            responseTime: definition.responseTime,
            resolutionTime: definition.resolutionTime,
            isActive: definition.isActive ?? true,
          }}
          onSubmit={handleSubmit}
          onCancel={() => navigate(`/sla/definitions/${id}`)}
        />
      </Card>
    </div>
  );
}
