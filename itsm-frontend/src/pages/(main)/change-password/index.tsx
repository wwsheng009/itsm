import React, { useState } from 'react';
import { useNavigate } from 'react-router';
import { Alert, App, Button, Card, Form, Input, Space, Typography } from 'antd';
import { KeyRound } from 'lucide-react';
import { AuthService } from '@/lib/services/auth-service';
import { useAuthStore } from '@/lib/store/auth-store';
import { usePasswordPolicy } from '@/lib/hooks/usePasswordPolicy';
import { useI18n } from '@/lib/i18n/useI18n';

const { Title, Text } = Typography;

interface ChangePasswordFormValues {
  oldPassword: string;
  newPassword: string;
  confirmPassword: string;
}

/**
 * 自助改密页（IP-P1-5）。
 * - 首登强制改密：`RequireAuth` 把 mustChangePassword 用户收敛到本页（guards.tsx）；
 * - 成功后清除本地 mustChangePassword 标志并回到工作台；
 * - 密码规则来自租户生效策略（/auth/password-policy，与服务端同源）。
 */
const ChangePasswordPage: React.FC = () => {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const { t } = useI18n();
  const [form] = Form.useForm<ChangePasswordFormValues>();
  const { hint, buildRules } = usePasswordPolicy();
  const [submitting, setSubmitting] = useState(false);
  const mustChange = useAuthStore(state => state.user?.mustChangePassword === true);

  const handleSubmit = async (values: ChangePasswordFormValues) => {
    setSubmitting(true);
    try {
      await AuthService.changePassword({
        oldPassword: values.oldPassword,
        newPassword: values.newPassword,
      });
      useAuthStore.getState().updateUser({ mustChangePassword: false });
      message.success(t('profile.changePasswordSuccess'));
      navigate('/dashboard', { replace: true });
    } catch (error) {
      message.error(error instanceof Error ? error.message : t('profile.changePasswordFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex justify-center">
      <Card style={{ width: 480 }}>
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <div>
            <Title level={3} style={{ marginBottom: 4 }}>
              <KeyRound className="inline-block align-middle mr-2" size={20} />
              修改密码
            </Title>
            <Text type="secondary">
              {mustChange
                ? '首次登录需修改密码后才能使用系统'
                : '修改成功后其他设备上的会话将失效，需重新登录'}
            </Text>
          </div>

          {mustChange && (
            <Alert
              type="warning"
              showIcon
              message="管理员要求首次登录修改密码"
              description="请先设置符合租户密码策略的新密码，完成后即可进入工作台。"
            />
          )}

          <Form form={form} layout="vertical" onFinish={handleSubmit} requiredMark={false}>
            <Form.Item
              name="oldPassword"
              label="当前密码"
              rules={[{ required: true, message: '请输入当前密码' }]}
            >
              <Input.Password placeholder="请输入当前密码" autoComplete="current-password" />
            </Form.Item>
            <Form.Item
              name="newPassword"
              label="新密码"
              rules={buildRules('请输入新密码')}
              extra={hint}
            >
              <Input.Password placeholder="请输入新密码" autoComplete="new-password" />
            </Form.Item>
            <Form.Item
              name="confirmPassword"
              label="确认新密码"
              dependencies={['newPassword']}
              rules={[
                { required: true, message: '请再次输入新密码' },
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!value || getFieldValue('newPassword') === value) {
                      return Promise.resolve();
                    }
                    return Promise.reject(new Error('两次输入的密码不一致'));
                  },
                }),
              ]}
            >
              <Input.Password placeholder="请再次输入新密码" autoComplete="new-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={submitting} block>
              确认修改
            </Button>
          </Form>
        </Space>
      </Card>
    </div>
  );
};

export default ChangePasswordPage;
