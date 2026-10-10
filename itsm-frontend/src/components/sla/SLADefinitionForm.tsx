import React from 'react';
import { Button, Form, Input, InputNumber, Select, Space, Switch } from 'antd';
import { SLAPriority, SLAPriorityLabels } from '@/constants/sla';

/**
 * SLA 定义共享表单（新建 / 编辑复用）。
 * 字段与后端 dto.CreateSLADefinitionRequest 对齐（itsm-backend/dto/sla_dto.go:8-19）：
 * name*、responseTime*（分钟，min=1）、resolutionTime*（分钟，min=1）为必填；
 * description/serviceType/priority/isActive 可选。
 */
export interface SLADefinitionFormValues {
  name: string;
  description?: string;
  serviceType?: string;
  priority: string;
  responseTime: number;
  resolutionTime: number;
  isActive: boolean;
}

interface SLADefinitionFormProps {
  initialValues?: Partial<SLADefinitionFormValues>;
  submitting?: boolean;
  submitText?: string;
  onSubmit: (values: SLADefinitionFormValues) => void | Promise<void>;
  onCancel: () => void;
}

const { TextArea } = Input;

const SLADefinitionForm: React.FC<SLADefinitionFormProps> = ({
  initialValues,
  submitting = false,
  submitText = '保存',
  onSubmit,
  onCancel,
}) => {
  const [form] = Form.useForm<SLADefinitionFormValues>();

  return (
    <Form
      form={form}
      layout="vertical"
      onFinish={onSubmit}
      initialValues={{
        priority: SLAPriority.P3,
        responseTime: 60,
        resolutionTime: 240,
        isActive: true,
        ...initialValues,
      }}
      style={{ maxWidth: 720 }}
    >
      <Form.Item
        name="name"
        label="名称"
        rules={[{ required: true, message: '请输入 SLA 名称' }]}
      >
        <Input placeholder="例如：标准服务 SLA" maxLength={100} />
      </Form.Item>

      <Form.Item name="description" label="描述">
        <TextArea rows={3} placeholder="SLA 适用范围与说明（可选）" maxLength={500} />
      </Form.Item>

      <Space size="large" style={{ display: 'flex', alignItems: 'flex-start' }}>
        <Form.Item name="serviceType" label="服务类型" style={{ minWidth: 200 }}>
          <Input placeholder="例如：standard / premium（可选）" maxLength={50} />
        </Form.Item>

        <Form.Item name="priority" label="优先级" rules={[{ required: true }]}>
          <Select
            style={{ width: 180 }}
            options={Object.values(SLAPriority).map(p => ({
              value: p,
              label: SLAPriorityLabels[p],
            }))}
          />
        </Form.Item>

        <Form.Item name="isActive" label="启用" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="停用" />
        </Form.Item>
      </Space>

      <Space size="large" style={{ display: 'flex', alignItems: 'flex-start' }}>
        <Form.Item
          name="responseTime"
          label="响应时限（分钟）"
          rules={[{ required: true, message: '请输入响应时限' }]}
        >
          <InputNumber min={1} max={100000} style={{ width: 180 }} />
        </Form.Item>

        <Form.Item
          name="resolutionTime"
          label="解决时限（分钟）"
          rules={[{ required: true, message: '请输入解决时限' }]}
        >
          <InputNumber min={1} max={100000} style={{ width: 180 }} />
        </Form.Item>
      </Space>

      <Form.Item style={{ marginTop: 8 }}>
        <Space>
          <Button type="primary" htmlType="submit" loading={submitting}>
            {submitText}
          </Button>
          <Button onClick={onCancel} disabled={submitting}>
            取消
          </Button>
        </Space>
      </Form.Item>
    </Form>
  );
};

export default SLADefinitionForm;
