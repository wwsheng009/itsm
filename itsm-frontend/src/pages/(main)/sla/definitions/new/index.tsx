import React, { useState } from 'react';
import { useNavigate } from 'react-router';
import { App, Button, Card, Result } from 'antd';
import { useCan } from '@/components/auth/PermissionGate';
import SLADefinitionForm, {
  type SLADefinitionFormValues,
} from '@/components/sla/SLADefinitionForm';
import { SLAApi } from '@/lib/api/sla-api';

/**
 * 新建 SLA 定义（/sla/definitions/new）。
 * 写面：POST /api/v1/sla/definitions（router/sla_routes.go:20，sla:write）。
 * 路由级门禁见 routes/route-permissions.ts；此处为组件级双保险。
 */
export default function SLADefinitionNewPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const canWriteSla = useCan(['sla:write']);
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (values: SLADefinitionFormValues) => {
    setSubmitting(true);
    try {
      const created = await SLAApi.createSLADefinition(values);
      message.success('SLA 定义创建成功');
      navigate(`/sla/definitions/${created.id}`);
    } catch (error) {
      console.error('Failed to create SLA definition:', error);
      message.error('创建失败，请检查必填项');
    } finally {
      setSubmitting(false);
    }
  };

  if (!canWriteSla) {
    return (
      <Result
        status="403"
        title="无权限"
        subTitle="新建 SLA 定义需要 sla:write 权限"
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
        <h1 className="mb-1 text-2xl font-bold">新建 SLA 定义</h1>
        <p className="m-0 text-sm text-gray-500">
          定义服务级别目标：响应/解决时限、优先级与适用范围。
        </p>
      </div>
      <Card>
        <SLADefinitionForm
          submitText="创建"
          submitting={submitting}
          onSubmit={handleSubmit}
          onCancel={() => navigate('/sla')}
        />
      </Card>
    </div>
  );
}
