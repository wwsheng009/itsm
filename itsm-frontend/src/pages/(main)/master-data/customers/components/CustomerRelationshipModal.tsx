import { useEffect, useState } from 'react';
import { App, Col, DatePicker, Form, Input, Modal, Row, Select, Switch } from 'antd';
import dayjs, { type Dayjs } from 'dayjs';
import {
  masterDataService,
  type CustomerRelationship,
  type ServiceCustomer,
} from '@/lib/services/masterDataService';

interface CustomerRelationshipModalProps {
  open: boolean;
  customerId: number;
  relationship: CustomerRelationship | null;
  customers: ServiceCustomer[];
  onCancel: () => void;
  onSaved: () => void;
}

interface RelationshipFormValues {
  toCustomerId?: number;
  relationType: string;
  isBidirectional?: boolean;
  effectiveFrom?: Dayjs;
  effectiveTo?: Dayjs;
  note?: string;
  status?: string;
}

const relationTypeOptions = [
  { value: 'parent', label: '母公司' },
  { value: 'subsidiary', label: '子公司' },
  { value: 'partner', label: '生态伙伴' },
  { value: 'reseller', label: '经销商' },
  { value: 'vendor', label: '供应商' },
  { value: 'competitor', label: '竞争对手' },
  { value: 'successor', label: '业务承继方' },
  { value: 'former_name_of', label: '曾用名主体' },
];

export default function CustomerRelationshipModal({
  open,
  customerId,
  relationship,
  customers,
  onCancel,
  onSaved,
}: CustomerRelationshipModalProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<RelationshipFormValues>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    form.resetFields();
    if (relationship) {
      form.setFieldsValue({
        toCustomerId: relationship.toCustomerId,
        relationType: relationship.relationType,
        isBidirectional: relationship.isBidirectional,
        effectiveFrom: relationship.effectiveFrom ? dayjs(relationship.effectiveFrom) : undefined,
        effectiveTo: relationship.effectiveTo ? dayjs(relationship.effectiveTo) : undefined,
        note: relationship.note,
        status: relationship.status,
      });
      return;
    }
    form.setFieldsValue({ relationType: 'parent', isBidirectional: false });
  }, [form, open, relationship]);

  const submit = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      if (relationship) {
        await masterDataService.updateRelationship(customerId, relationship.id, {
          effectiveFrom: values.effectiveFrom?.toISOString() ?? null,
          effectiveTo: values.effectiveTo?.toISOString() ?? null,
          note: values.note,
          status: values.status,
        });
        message.success('客户关系已更新');
      } else {
        await masterDataService.createRelationship(customerId, {
          toCustomerId: values.toCustomerId!,
          relationType: values.relationType,
          isBidirectional: values.isBidirectional ?? false,
          effectiveFrom: values.effectiveFrom?.toISOString() ?? null,
          effectiveTo: values.effectiveTo?.toISOString() ?? null,
          note: values.note,
        });
        message.success('客户关系已添加');
      }
      onSaved();
    } catch (error) {
      message.error((error as Error).message || '保存客户关系失败');
    } finally {
      setSaving(false);
    }
  };

  const targetOptions = customers
    .filter(customer => customer.id !== customerId)
    .map(customer => ({
      value: customer.id,
      label: `${customer.name}${customer.code ? `（${customer.code}）` : ''}`,
    }));

  return (
    <Modal
      title={relationship ? '编辑客户关系' : '添加客户关系'}
      open={open}
      onOk={submit}
      onCancel={onCancel}
      confirmLoading={saving}
      width={680}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        <Row gutter={12}>
          <Col xs={24} md={12}>
            <Form.Item
              name="toCustomerId"
              label="目标客户"
              rules={relationship ? [] : [{ required: true, message: '请选择目标客户' }]}
            >
              <Select
                showSearch
                optionFilterProp="label"
                disabled={!!relationship}
                options={targetOptions}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="relationType" label="关系类型" rules={[{ required: true }]}>
              <Select disabled={!!relationship} options={relationTypeOptions} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="isBidirectional" label="双向关系" valuePropName="checked">
              <Switch disabled={!!relationship} />
            </Form.Item>
          </Col>
          {relationship && (
            <Col xs={24} md={12}>
              <Form.Item name="status" label="状态">
                <Select
                  options={[
                    { value: 'active', label: '有效' },
                    { value: 'ended', label: '已结束' },
                  ]}
                />
              </Form.Item>
            </Col>
          )}
          <Col xs={24} md={12}>
            <Form.Item name="effectiveFrom" label="生效日期">
              <DatePicker className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="effectiveTo" label="失效日期">
              <DatePicker className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={24}>
            <Form.Item name="note" label="备注">
              <Input.TextArea rows={3} />
            </Form.Item>
          </Col>
        </Row>
      </Form>
    </Modal>
  );
}
