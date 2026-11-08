import { useEffect, useState } from 'react';
import { App, Col, Form, Input, Modal, Row, Select, Switch } from 'antd';
import {
  masterDataService,
  type CustomerBranch,
  type CustomerContact,
  type CustomerContactWritePayload,
} from '@/lib/services/masterDataService';

interface CustomerContactModalProps {
  open: boolean;
  customerId: number;
  contact: CustomerContact | null;
  branches: CustomerBranch[];
  onCancel: () => void;
  onSaved: () => void;
}

interface ContactFormValues {
  branchId?: number;
  name: string;
  nameEn?: string;
  title?: string;
  department?: string;
  email?: string;
  mobile?: string;
  phone?: string;
  wechat?: string;
  contactRole?: string;
  isPrimary?: boolean;
  language?: string;
  timezone?: string;
  optIn?: boolean;
  status?: string;
}

const roleOptions = [
  { value: 'business', label: '业务对接' },
  { value: 'technical', label: '技术对接' },
  { value: 'billing', label: '财务' },
  { value: 'escalation', label: '升级投诉' },
  { value: 'noc', label: '值班/监控接收' },
  { value: 'executive', label: '高管' },
];

export default function CustomerContactModal({
  open,
  customerId,
  contact,
  branches,
  onCancel,
  onSaved,
}: CustomerContactModalProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<ContactFormValues>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (contact) {
      form.resetFields();
      form.setFieldsValue({
        branchId: contact.branchId ?? undefined,
        name: contact.name,
        nameEn: contact.nameEn,
        title: contact.title,
        department: contact.department,
        email: contact.email,
        mobile: contact.mobile,
        phone: contact.phone,
        wechat: contact.wechat,
        contactRole: contact.contactRole,
        isPrimary: contact.isPrimary,
        language: contact.language,
        timezone: contact.timezone,
        optIn: contact.optIn,
        status: contact.status,
      });
      return;
    }
    form.resetFields();
    form.setFieldsValue({
      contactRole: 'business',
      isPrimary: false,
      optIn: true,
      status: 'active',
    });
  }, [contact, form, open]);

  const submit = async () => {
    const values = await form.validateFields();
    const payload: CustomerContactWritePayload = {
      branchId: values.branchId ?? null,
      name: values.name,
      nameEn: values.nameEn,
      title: values.title,
      department: values.department,
      email: values.email,
      mobile: values.mobile,
      phone: values.phone,
      wechat: values.wechat,
      contactRole: values.contactRole,
      isPrimary: values.isPrimary ?? false,
      language: values.language,
      timezone: values.timezone,
      optIn: values.optIn ?? true,
      status: values.status,
    };
    setSaving(true);
    try {
      if (contact) {
        await masterDataService.updateContact(customerId, contact.id, payload);
        message.success('联系人已更新');
      } else {
        await masterDataService.createContact(customerId, payload);
        message.success('联系人已添加');
      }
      onSaved();
    } catch (error) {
      message.error((error as Error).message || '保存联系人失败');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={contact ? '编辑联系人' : '添加联系人'}
      open={open}
      onOk={submit}
      onCancel={onCancel}
      confirmLoading={saving}
      width={720}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        <Row gutter={12}>
          <Col xs={24} md={12}>
            <Form.Item name="name" label="姓名" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="nameEn" label="英文名">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="title" label="职位">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="department" label="部门">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="contactRole" label="联系角色">
              <Select options={roleOptions} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="branchId" label="所属服务点">
              <Select
                allowClear
                options={branches.map(branch => ({
                  value: branch.id,
                  label: branch.name,
                }))}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="email" label="邮箱">
              <Input placeholder="name@example.com" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="mobile" label="手机">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="phone" label="电话">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="wechat" label="微信">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="language" label="语言">
              <Input placeholder="zh-CN" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="timezone" label="时区">
              <Input placeholder="Asia/Shanghai" />
            </Form.Item>
          </Col>
          <Col xs={12} md={6}>
            <Form.Item name="isPrimary" label="主要联系人" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Col>
          <Col xs={12} md={6}>
            <Form.Item name="optIn" label="接收通知" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="status" label="状态">
              <Select
                options={[
                  { value: 'active', label: '活跃' },
                  { value: 'inactive', label: '停用' },
                ]}
              />
            </Form.Item>
          </Col>
        </Row>
      </Form>
    </Modal>
  );
}
