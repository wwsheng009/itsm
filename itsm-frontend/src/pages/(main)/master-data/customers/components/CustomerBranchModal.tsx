import { useEffect, useState } from 'react';
import { App, Col, Form, Input, InputNumber, Modal, Row, Select, Switch } from 'antd';
import {
  masterDataService,
  type BranchWritePayload,
  type CustomerBranch,
} from '@/lib/services/masterDataService';
import AddressFields from './AddressFields';

interface CustomerBranchModalProps {
  open: boolean;
  customerId: number;
  branch: CustomerBranch | null;
  onCancel: () => void;
  onSaved: () => void;
}

interface BranchFormValues {
  code?: string;
  name: string;
  branchType?: string;
  aliases?: string[];
  address?: Record<string, string>;
  timezone?: string;
  contactPhone?: string;
  emailDomains?: string[];
  serviceTier?: string;
  supportHours?: string;
  isPrimary?: boolean;
  linkedTenantId?: number;
  status?: string;
}

export default function CustomerBranchModal({
  open,
  customerId,
  branch,
  onCancel,
  onSaved,
}: CustomerBranchModalProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<BranchFormValues>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (branch) {
      form.resetFields();
      form.setFieldsValue({
        code: branch.code,
        name: branch.name,
        branchType: branch.branchType,
        aliases: branch.aliases ?? [],
        address: (branch.address ?? {}) as Record<string, string>,
        timezone: branch.timezone,
        contactPhone: branch.contactPhone,
        emailDomains: branch.emailDomains ?? [],
        serviceTier: branch.serviceTier,
        supportHours: branch.supportHours,
        isPrimary: branch.isPrimary,
        linkedTenantId: branch.linkedTenantId ?? undefined,
        status: branch.status,
      });
      return;
    }
    form.resetFields();
    form.setFieldsValue({
      branchType: 'site',
      aliases: [],
      emailDomains: [],
      address: {},
      isPrimary: false,
      status: 'active',
    });
  }, [branch, form, open]);

  const submit = async () => {
    const values = await form.validateFields();
    const payload: BranchWritePayload = {
      customerId,
      code: values.code,
      name: values.name,
      branchType: values.branchType,
      aliases: values.aliases ?? [],
      address: values.address ?? {},
      timezone: values.timezone,
      contactPhone: values.contactPhone,
      emailDomains: values.emailDomains ?? [],
      serviceTier: values.serviceTier,
      supportHours: values.supportHours,
      isPrimary: values.isPrimary ?? false,
      linkedTenantId: values.linkedTenantId ?? null,
      status: values.status,
    };
    setSaving(true);
    try {
      if (branch) {
        await masterDataService.updateBranch(branch.id, payload);
        message.success('服务点已更新');
      } else {
        await masterDataService.createBranch(payload);
        message.success('服务点已添加');
      }
      onSaved();
    } catch (error) {
      message.error((error as Error).message || '保存服务点失败');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={branch ? '编辑服务点' : '添加服务点'}
      open={open}
      onOk={submit}
      onCancel={onCancel}
      confirmLoading={saving}
      width={760}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        <Row gutter={12}>
          <Col xs={24} md={12}>
            <Form.Item name="name" label="名称" rules={[{ required: true }]}>
              <Input placeholder="例如：上海分公司" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="code" label="服务点编码">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="branchType" label="类型">
              <Select
                options={[
                  { value: 'hq', label: '总部' },
                  { value: 'site', label: '服务点' },
                  { value: 'datacenter', label: '数据中心' },
                  { value: 'warehouse', label: '仓库' },
                  { value: 'store', label: '门店' },
                  { value: 'subsidiary', label: '子公司' },
                ]}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="aliases" label="别名">
              <Select mode="tags" tokenSeparators={[',', '，']} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="timezone" label="时区">
              <Input placeholder="Asia/Shanghai" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="contactPhone" label="联系电话">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="emailDomains" label="邮箱域名">
              <Select mode="tags" tokenSeparators={[',', '，']} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="serviceTier" label="服务档位">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="supportHours" label="服务时间">
              <Input placeholder="7x24 / 5x8" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="linkedTenantId" label="关联租户 ID">
              <InputNumber min={1} className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={12} md={6}>
            <Form.Item name="isPrimary" label="主服务点" valuePropName="checked">
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
        <AddressFields prefix={['address']} />
      </Form>
    </Modal>
  );
}
