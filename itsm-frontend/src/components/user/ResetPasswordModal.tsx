import React, { useEffect, useMemo, useState } from 'react';
import { Alert, App, Button, Checkbox, Form, Input, Modal, Radio, Space, Typography } from 'antd';
import type { FormItemProps } from 'antd';
import { Copy } from 'lucide-react';

/**
 * 重置密码模态框（共享组件，TUM-4）：
 * - `modes=['specified']`：既有 /users 管理面行为（管理员指定新密码）；
 * - `modes=['generated','specified']`：平台侧租户用户管理（默认一次性口令 + 强制改密）。
 * 一次性口令结果卡片要求勾选「我已保存」后方可关闭，防止误关丢失。
 */
export type ResetPasswordMode = 'generated' | 'specified';

export interface ResetPasswordSubmitValues {
  mode: ResetPasswordMode;
  newPassword?: string;
}

export interface ResetPasswordSubmitResult {
  generatedPassword?: string;
  mustChangePassword?: boolean;
}

export interface ResetPasswordModalProps {
  open: boolean;
  user: { id: number; username: string; name?: string } | null;
  /** 可选模式（默认两种） */
  modes?: ResetPasswordMode[];
  /** 指定模式密码策略提示 */
  policyHint?: string;
  /** 指定模式校验规则（与后端策略同源；缺省仅必填） */
  buildRules?: (requiredMessage: string) => FormItemProps['rules'];
  title?: string;
  /** 错误文案映射（默认取 error.message） */
  mapError?: (error: unknown) => string;
  onSubmit: (values: ResetPasswordSubmitValues) => Promise<ResetPasswordSubmitResult | void>;
  onClose: () => void;
}

const { Text, Paragraph } = Typography;

const ResetPasswordModal: React.FC<ResetPasswordModalProps> = ({
  open,
  user,
  modes = ['generated', 'specified'],
  policyHint,
  buildRules,
  title,
  mapError,
  onSubmit,
  onClose,
}) => {
  const { message } = App.useApp();
  const [form] = Form.useForm<ResetPasswordSubmitValues>();
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<ResetPasswordSubmitResult | null>(null);
  const [saved, setSaved] = useState(false);

  const mode = Form.useWatch('mode', form);
  const allowGenerated = modes.includes('generated');
  const allowSpecified = modes.includes('specified');
  const defaultMode: ResetPasswordMode = allowGenerated ? 'generated' : 'specified';

  useEffect(() => {
    if (open) {
      setResult(null);
      setSaved(false);
      setSubmitting(false);
      form.setFieldsValue({ mode: defaultMode, newPassword: undefined });
    }
  }, [open, defaultMode, form]);

  const rules = useMemo<FormItemProps['rules']>(
    () =>
      buildRules ? buildRules('请输入新密码') : [{ required: true, message: '请输入新密码' }],
    [buildRules]
  );

  const handleSubmit = async (values: ResetPasswordSubmitValues) => {
    setSubmitting(true);
    try {
      const next = await onSubmit({
        mode: values.mode,
        newPassword: values.mode === 'specified' ? values.newPassword : undefined,
      });
      if (next?.generatedPassword) {
        setResult(next);
      } else {
        onClose();
      }
    } catch (error) {
      message.error(
        mapError ? mapError(error) : error instanceof Error ? error.message : '操作失败，请稍后重试'
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleCopy = async () => {
    const password = result?.generatedPassword;
    if (!password) return;
    try {
      await navigator.clipboard.writeText(password);
      message.success('口令已复制');
    } catch {
      message.warning('复制失败，请手动选择复制');
    }
  };

  const handleCancel = () => {
    if (result && !saved) {
      message.warning('请先勾选「我已保存口令」再关闭');
      return;
    }
    onClose();
  };

  const footer = result ? (
    <Button type="primary" disabled={!saved} onClick={onClose} data-testid="reset-password-close">
      关闭
    </Button>
  ) : (
    <Space>
      <Button onClick={handleCancel}>取消</Button>
      <Button type="primary" loading={submitting} onClick={() => form.submit()}>
        确认重置
      </Button>
    </Space>
  );

  return (
    <Modal
      open={open}
      title={title ?? `重置密码${user ? ` - ${user.username}` : ''}`}
      onCancel={handleCancel}
      footer={footer}
      maskClosable={!result}
      closable={!result}
      destroyOnHidden
    >
      {result ? (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Alert
            type="warning"
            showIcon
            message="一次性口令仅显示一次"
            description="关闭后无法再次查看，需重新生成（重新生成会作废该口令）。请通过安全渠道转交用户，并要求其首次登录后立即修改。"
          />
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              padding: '12px 16px',
              border: '1px solid var(--ant-color-border, #d9d9d9)',
              borderRadius: 8,
              background: 'var(--ant-color-fill-quaternary, #fafafa)',
            }}
          >
            <Text strong style={{ fontFamily: 'monospace', fontSize: 16 }}>
              {result.generatedPassword}
            </Text>
            <Button icon={<Copy size={14} />} onClick={handleCopy}>
              复制
            </Button>
          </div>
          {result.mustChangePassword && (
            <Text type="secondary">该用户下次登录将被要求修改密码。</Text>
          )}
          <Checkbox checked={saved} onChange={e => setSaved(e.target.checked)}>
            我已保存口令（关闭后不可再查看）
          </Checkbox>
        </Space>
      ) : (
        <Form form={form} layout="vertical" onFinish={handleSubmit} initialValues={{ mode: defaultMode }}>
          {allowGenerated && allowSpecified && (
            <Form.Item name="mode" label="重置方式">
              <Radio.Group>
                <Radio.Button value="generated">一次性口令（推荐）</Radio.Button>
                <Radio.Button value="specified">指定新密码</Radio.Button>
              </Radio.Group>
            </Form.Item>
          )}
          {mode === 'generated' ? (
            <Alert
              type="info"
              showIcon
              message="将生成 16 位一次性强口令，并吊销该用户全部会话"
              description="口令明文仅本次响应回传一次，后端不落库、不写审计；用户首次登录需强制改密。"
            />
          ) : (
            <Form.Item
              name="newPassword"
              label="新密码"
              rules={rules}
              extra={policyHint}
              preserve={false}
            >
              <Input.Password placeholder="请输入符合租户密码策略的新密码" autoComplete="new-password" />
            </Form.Item>
          )}
          {mode === 'specified' && (
            <Paragraph type="secondary" style={{ marginBottom: 0 }}>
              重置后该用户全部会话将立即失效，需重新登录。
            </Paragraph>
          )}
        </Form>
      )}
    </Modal>
  );
};

export default ResetPasswordModal;
