import { useEffect, useState } from 'react';
import { App, Col, Form, Input, Modal, Row, Select, Switch } from 'antd';
import type { Tenant } from '@/lib/api/api-config';
import {
  masterDataService,
  type CustomerTenantLink,
  type CustomerTenantLinkWritePayload,
} from '@/lib/services/masterDataService';

interface CustomerTenantLinkModalProps {
  open: boolean;
  customerId: number;
  link: CustomerTenantLink | null;
  tenants: Tenant[];
  onCancel: () => void;
  onSaved: () => void;
}

interface LinkFormValues {
  linkedTenantId: number;
  linkType?: string;
  isBillingAnchor?: boolean;
  note?: string;
}

const linkTypeOptions = [
  { value: 'primary', label: '主租户' },
  { value: 'secondary', label: '辅助租户' },
  { value: 'sandbox', label: '测试租户' },
  { value: 'archive', label: '归档租户' },
  { value: 'regional', label: '区域租户' },
];

export default function CustomerTenantLinkModal({
  open,
  customerId,
  link,
  tenants,
  onCancel,
  onSaved,
}: CustomerTenantLinkModalProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<LinkFormValues>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (link) {
      form.resetFields();
      form.setFieldsValue({
        linkedTenantId: link.linkedTenantId,
        linkType: link.linkType,
        isBillingAnchor: link.isBillingAnchor,
        note: link.note,
      });
      return;
    }
    form.resetFields();
    form.setFieldsValue({
      linkType: 'primary',
      isBillingAnchor: false,
    });
  }, [form, link, open]);

  const submit = async () => {
    const values = await form.validateFields();
    const payload: CustomerTenantLinkWritePayload = {
      linkedTenantId: values.linkedTenantId,
      linkType: values.linkType,
      isBillingAnchor: values.isBillingAnchor ?? false,
      note: values.note,
    };
    setSaving(true);
    try {
      if (link) {
        await masterDataService.updateTenantLink(customerId, link.id, payload);
        message.success('租户关联已更新');
      } else {
        await masterDataService.createTenantLink(customerId, payload);
        message.success('租户关联已添加');
      }
      onSaved();
    } catch (error) {
      message.error((error as Error).message || '保存租户关联失败；请检查租户类型与服务商归属');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={link ? '编辑关联租户' : '添加关联租户'}
      open={open}
      onOk={submit}
      onCancel={onCancel}
      confirmLoading={saving}
      width={640}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        <Row gutter={12}>
          <Col xs={24}>
            <Form.Item
              name="linkedTenantId"
              label="目标租户"
              rules={[{ required: true, message: '请选择目标租户' }]}
            >
              <Select
                showSearch
                optionFilterProp="label"
                options={tenants.map(tenant => ({
                  value: tenant.id,
                  label: `${tenant.name}（${tenant.code} · ${tenant.type}）`,
                }))}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="linkType" label="关联类型">
              <Select options={linkTypeOptions} />
            </Form.Item>
          </Col>
          <Col xs={12} md={8}>
            <Form.Item name="isBillingAnchor" label="结算锚点" valuePropName="checked">
              <Switch />
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
