/**
 * ProvisionUserModal：跨租户「建号」通道的通用弹窗（平台通道 / MSP 通道复用）。
 *
 * 对接冻结契约：
 * - 平台：POST /api/v1/tenants/:id/users         （tenant:write）
 * - MSP ：POST /api/v1/msp/customers/:cid/users  （msp_customer:write）
 *
 * 请求体 = CreateUserRequest 的 UI 子集（username/name/email/password 必填；
 * 密码强度由目标租户 system_configs 的密码策略在服务端校验）。
 * 调用方通过 `submit` 注入具体 API（便于测试替身与两处复用）。
 */
import { useEffect, useState } from 'react';
import { Alert, Form, Input, Modal, Select } from 'antd';

export interface ProvisionUserPayload {
  username: string;
  name: string;
  email: string;
  password: string;
  /** MSP 角色（users.msp_role 词表）；仅服务商租户建号需要。 */
  mspRole?: string;
}

export interface ProvisionCustomerOption {
  id: number;
  code: string;
  name: string;
}

interface ProvisionUserModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  /** 目标描述（租户/客户），用于弹窗内提示。 */
  targetLabel?: string;
  /** 提供时表单出现「MSP 角色」下拉（服务商租户建号必须携带）。 */
  mspRoleOptions?: { value: string; label: string }[];
  /** 提供时表单出现「客户」下拉（MSP 通道）。 */
  customerOptions?: ProvisionCustomerOption[];
  submit: (payload: ProvisionUserPayload, customerId: number | undefined) => Promise<unknown>;
  /** 提交成功后回调（例如刷新列表）。 */
  onSuccess?: () => void;
}

type FormValues = ProvisionUserPayload & { customerId?: number };

export default function ProvisionUserModal({
  open,
  onClose,
  title,
  targetLabel,
  mspRoleOptions,
  customerOptions,
  submit,
  onSuccess,
}: ProvisionUserModalProps) {
  const [form] = Form.useForm<FormValues>();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    form.resetFields();
    // 单客户时默认选中，减少操作步数。
    if (customerOptions && customerOptions.length === 1) {
      form.setFieldValue('customerId', customerOptions[0].id);
    }
  }, [open, customerOptions, form]);

  const handleSubmit = async (values: FormValues) => {
    if (customerOptions && !values.customerId) {
      setError('请选择客户');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await submit(
        {
          username: values.username,
          name: values.name,
          email: values.email,
          password: values.password,
          ...(mspRoleOptions && values.mspRole ? { mspRole: values.mspRole } : {}),
        },
        values.customerId
      );
      onClose();
      onSuccess?.();
    } catch (err: unknown) {
      const msg = (err as { message?: string } | null)?.message || '建号失败';
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title={title}
      open={open}
      onCancel={onClose}
      onOk={() => form.submit()}
      confirmLoading={submitting}
      okText="建号"
      destroyOnHidden
      width={520}
    >
      {targetLabel ? (
        <Alert
          type="info"
          showIcon
          message={`目标：${targetLabel}`}
          style={{ marginBottom: 12 }}
          data-testid="provision-user-target"
        />
      ) : null}
      {error ? (
        <Alert
          type="error"
          showIcon
          message={error}
          style={{ marginBottom: 12 }}
          data-testid="provision-user-error"
        />
      ) : null}
      <Form form={form} layout="vertical" onFinish={handleSubmit}>
        {customerOptions ? (
          <span data-testid="provision-customer-select">
            <Form.Item name="customerId" label="客户" rules={[{ required: true, message: '请选择客户' }]}>
              <Select
                placeholder="选择客户"
                showSearch
                optionFilterProp="label"
                options={customerOptions.map(c => ({ value: c.id, label: `${c.code} - ${c.name}` }))}
              />
            </Form.Item>
          </span>
        ) : null}
        <Form.Item
          name="username"
          label="用户名"
          rules={[
            { required: true, message: '请输入用户名' },
            { min: 3, max: 50, message: '用户名长度 3-50' },
          ]}
        >
          <Input placeholder="new.user" data-testid="provision-username-input" />
        </Form.Item>
        <Form.Item name="name" label="姓名" rules={[{ required: true, message: '请输入姓名' }]}>
          <Input placeholder="张三" data-testid="provision-name-input" />
        </Form.Item>
        <Form.Item
          name="email"
          label="邮箱"
          rules={[
            { required: true, message: '请输入邮箱' },
            { type: 'email', message: '邮箱格式不正确' },
          ]}
        >
          <Input placeholder="new.user@example.com" data-testid="provision-email-input" />
        </Form.Item>
        <Form.Item
          name="password"
          label="初始密码"
          extra="需满足目标租户密码策略（默认 ≥6 位）；交付后建议提醒用户首次登录修改。"
          rules={[
            { required: true, message: '请输入初始密码' },
            { min: 6, max: 128, message: '密码长度 6-128' },
          ]}
        >
          <Input.Password placeholder="初始密码" data-testid="provision-password-input" />
        </Form.Item>
        {mspRoleOptions && mspRoleOptions.length > 0 ? (
          <span data-testid="provision-msp-role-select">
            <Form.Item
              name="mspRole"
              label="MSP 角色"
              rules={[{ required: true, message: '请选择 MSP 角色' }]}
              extra="服务商租户员工必须带 MSP 角色，否则无法进入服务商工作台。"
            >
              <Select placeholder="选择 MSP 角色" options={mspRoleOptions} />
            </Form.Item>
          </span>
        ) : null}
      </Form>
    </Modal>
  );
}
