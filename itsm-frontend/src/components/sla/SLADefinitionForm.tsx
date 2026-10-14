import React, { useEffect, useMemo, useState } from 'react';
import { Button, Form, Input, InputNumber, Select, Space, Switch } from 'antd';
import { SLAPriority, SLAPriorityLabels } from '@/constants/sla';
import { TicketTypeApi } from '@/lib/api/ticketTypeApi';

/**
 * SLA 定义共享表单（新建 / 编辑复用）。
 * 字段与后端 dto.CreateSLADefinitionRequest 对齐（itsm-backend/dto/sla_dto.go:8-19）：
 * name*、responseTime*（分钟，min=1）、resolutionTime*（分钟，min=1）为必填；
 * description/serviceType/priority/isActive 可选。
 *
 * v1.3（方案 D-SLA-3）：serviceType 从自由文本改为**租户动态工单类型受控枚举**
 * （数据源 TicketTypeApi.list，与后端匹配引擎的动态类型解析对齐）；
 * 留空 = 类型无关租户默认定义（L3c 兜底位）。
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

interface TypeOption {
  value: string;
  label: string;
}

const SLADefinitionForm: React.FC<SLADefinitionFormProps> = ({
  initialValues,
  submitting = false,
  submitText = '保存',
  onSubmit,
  onCancel,
}) => {
  const [form] = Form.useForm<SLADefinitionFormValues>();
  const [typeOptions, setTypeOptions] = useState<TypeOption[]>([]);
  const [typesLoading, setTypesLoading] = useState(false);
  const [typeLoadFailed, setTypeLoadFailed] = useState(false);
  const serviceTypeValue = Form.useWatch('serviceType', form);

  // 动态工单类型下拉（受控枚举）：读取当前租户的 active 类型配置。
  // 失败时退化为自由输入，避免阻塞定义维护（后端匹配按 code 精确比对）。
  useEffect(() => {
    let cancelled = false;
    (async () => {
      setTypesLoading(true);
      try {
        const resp = await TicketTypeApi.list({ status: 'active', pageSize: 200 });
        if (cancelled) return;
        const items = resp?.types ?? [];
        setTypeOptions(
          items.map(t => ({ value: t.code, label: `${t.name}（${t.code}）` }))
        );
      } catch {
        if (!cancelled) setTypeLoadFailed(true);
      } finally {
        if (!cancelled) setTypesLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const legacyValueNotice = useMemo(() => {
    if (!serviceTypeValue || typeOptions.length === 0) return null;
    return typeOptions.some(o => o.value === serviceTypeValue)
      ? null
      : '该值不在当前租户类型配置中（历史数据不生效，请重新选择）';
  }, [serviceTypeValue, typeOptions]);

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
        <Form.Item
          name="serviceType"
          label="服务类型（工单类型）"
          style={{ minWidth: 260 }}
          extra={
            legacyValueNotice ??
            '留空 = 类型无关租户默认（作为兜底定义生效）；否则按该类型的 code 精确匹配'
          }
          validateStatus={legacyValueNotice ? 'warning' : undefined}
        >
          {typeLoadFailed ? (
            <Input placeholder="类型列表加载失败，可手动输入类型 code" maxLength={50} />
          ) : (
            <Select
              allowClear
              showSearch
              loading={typesLoading}
              placeholder="选择工单类型（留空 = 类型无关默认）"
              options={typeOptions}
              optionFilterProp="label"
              style={{ width: 260 }}
              notFoundContent={typesLoading ? '加载中…' : '暂无可选类型（可留空）'}
            />
          )}
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
