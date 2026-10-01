import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { AlertCircle, CheckCircle, Lock, UserPlus } from 'lucide-react';
import { Button, Card, ConfigProvider, Flex, Form, Input, Typography, message } from 'antd';
import { antdTheme } from '@/lib/antd-theme';
import AuthService, { type InvitationLandingInfo } from '@/lib/services/auth-service';
import { logger } from '@/lib/env';

const { Text, Title } = Typography;

/**
 * 邀请落地页（IP-P1-4c，公开路由 `/invite?token=...`）。
 *
 * 契约（IP-P1-4b）：GET /api/v1/auth/invitations/:token 最小回显（邮箱脱敏），
 * POST .../accept 设置密码并完成建号/绑定 + membership；重放/过期/撤销返回
 * 可展示错误。接受成功后引导至登录页，由首登强制改密策略接管后续。
 */
export default function InviteLandingPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [form] = Form.useForm();
  const token = searchParams.get('token') || '';

  const [loading, setLoading] = useState(true);
  const [info, setInfo] = useState<InvitationLandingInfo | null>(null);
  const [invalidReason, setInvalidReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    if (!token) {
      setLoading(false);
      setInvalidReason('邀请链接缺少 token，请从邮件中的完整链接打开');
      return;
    }
    let alive = true;
    AuthService.inspectInvitation(token)
      .then(data => {
        if (!alive) return;
        setInfo(data);
        if (data.status !== 'pending') {
          setInvalidReason(
            data.status === 'accepted'
              ? '该邀请已被接受，请直接登录'
              : data.status === 'revoked'
                ? '该邀请已被撤销，请联系管理员重新邀请'
                : '该邀请已过期，请联系管理员重新邀请'
          );
        }
      })
      .catch(err => {
        if (!alive) return;
        logger.error('邀请回显失败:', err);
        setInvalidReason(err instanceof Error ? err.message : '邀请链接无效或不可用');
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [token]);

  const passwordStrength = useMemo(
    () => (password: string) => {
      let strength = 0;
      if (password.length >= 8) strength++;
      if (/[A-Z]/.test(password)) strength++;
      if (/[a-z]/.test(password)) strength++;
      if (/[0-9]/.test(password)) strength++;
      if (/[^A-Za-z0-9]/.test(password)) strength++;
      return strength;
    },
    []
  );

  const handleAccept = async (values: { name?: string; password: string; confirmPassword: string }) => {
    setSubmitting(true);
    try {
      await AuthService.acceptInvitation({ token, password: values.password, name: values.name });
      setSuccess(true);
      message.success('账号已激活，请使用新密码登录');
      setTimeout(() => navigate('/login', { replace: true }), 3000);
    } catch (err) {
      logger.error('接受邀请失败:', err);
      message.error(err instanceof Error ? err.message : '接受邀请失败，请稍后重试');
    } finally {
      setSubmitting(false);
    }
  };

  const shell = (children: React.ReactNode) => (
    <ConfigProvider theme={antdTheme}>
      <div className='min-h-screen flex items-center justify-center p-5 bg-gradient-to-br from-gray-50 to-blue-50'>
        <div className='w-full max-w-[420px]'>
          <Card className='rounded-xl shadow-xl border-none' styles={{ body: { padding: '40px' } }}>
            {children}
          </Card>
        </div>
      </div>
    </ConfigProvider>
  );

  if (loading) {
    return shell(
      <div className='text-center py-10'>
        <Text className='text-gray-500'>正在校验邀请链接…</Text>
      </div>
    );
  }

  if (success) {
    return shell(
      <div className='text-center'>
        <div className='w-16 h-16 mx-auto mb-4 bg-green-100 rounded-full flex items-center justify-center'>
          <CheckCircle size={32} className='text-green-600' />
        </div>
        <Title level={3} className='!mb-2 !text-gray-900 !text-xl'>
          账号已激活
        </Title>
        <Text className='!text-gray-500 !text-sm block !mb-6'>
          密码设置成功，即将跳转到登录页
        </Text>
        <Button type='primary' size='large' className='w-full h-10' onClick={() => navigate('/login')}>
          立即登录
        </Button>
      </div>
    );
  }

  if (invalidReason || !info) {
    return shell(
      <div className='text-center'>
        <div className='w-16 h-16 mx-auto mb-4 bg-red-100 rounded-full flex items-center justify-center'>
          <AlertCircle size={32} className='text-red-600' />
        </div>
        <Title level={3} className='!mb-2 !text-gray-900 !text-xl'>
          邀请不可用
        </Title>
        <Text className='!text-gray-500 !text-sm block !mb-6'>{invalidReason || '邀请链接无效或不可用'}</Text>
        <Button type='primary' size='large' className='w-full h-10' onClick={() => navigate('/login')}>
          返回登录
        </Button>
      </div>
    );
  }

  return shell(
    <>
      <div className='text-center mb-6'>
        <div className='w-14 h-14 mx-auto mb-3 bg-blue-100 rounded-full flex items-center justify-center'>
          <UserPlus size={26} className='text-blue-600' />
        </div>
        <Title level={2} className='!mb-1 !text-gray-900 !text-xl'>
          加入 {info.tenantName || '企业'}
        </Title>
        <Text className='!text-gray-500 !text-sm'>
          邀请邮箱 {info.emailMasked} · 角色 {info.roleCode}
        </Text>
      </div>

      <Form form={form} layout='vertical' onFinish={handleAccept} requiredMark={false}>
        <Form.Item name='name' label='姓名（选填）'>
          <Input size='large' placeholder='请输入姓名' autoComplete='name' />
        </Form.Item>

        <Form.Item
          name='password'
          label='设置密码'
          rules={[{ required: true, message: '请输入密码' }]}
        >
          <Input.Password size='large' prefix={<Lock size={16} />} placeholder='至少 8 位，含大小写与数字' autoComplete='new-password' />
        </Form.Item>

        <Form.Item
          name='confirmPassword'
          label='确认密码'
          dependencies={['password']}
          rules={[
            { required: true, message: '请再次输入密码' },
            ({ getFieldValue }) => ({
              validator: (_, value) =>
                !value || getFieldValue('password') === value
                  ? Promise.resolve()
                  : Promise.reject(new Error('两次输入的密码不一致')),
            }),
          ]}
        >
          <Input.Password size='large' prefix={<Lock size={16} />} placeholder='再次输入密码' autoComplete='new-password' />
        </Form.Item>

        <Form.Item noStyle shouldUpdate={(prev, cur) => prev.password !== cur.password}>
          {({ getFieldValue }) => {
            const strength = passwordStrength(getFieldValue('password') || '');
            if (!getFieldValue('password')) return null;
            return (
              <Flex className='mb-4' gap={4}>
                {[1, 2, 3, 4, 5].map(level => (
                  <div
                    key={level}
                    className={`h-1 flex-1 rounded ${
                      strength >= level ? (strength >= 4 ? 'bg-green-500' : 'bg-amber-400') : 'bg-gray-200'
                    }`}
                  />
                ))}
              </Flex>
            );
          }}
        </Form.Item>

        <Button
          type='primary'
          htmlType='submit'
          size='large'
          loading={submitting}
          className='w-full h-10 rounded-md text-sm font-semibold'
        >
          设置密码并激活账号
        </Button>
      </Form>

      <div className='text-center mt-4'>
        <Text className='text-gray-400 text-xs'>
          已有账号？{' '}
          <Link to='/login' className='text-blue-600 hover:underline'>
            去登录
          </Link>
        </Text>
      </div>
    </>
  );
}
