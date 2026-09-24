import { Link, useNavigate, useSearchParams } from 'react-router';

import React, { useState, useEffect, useRef, Suspense } from 'react';
import { User, Lock, Shield, ArrowRight } from 'lucide-react';
import { useI18n } from '@/lib/i18n/useI18n';
import {
  Typography,
  Space,
  Alert,
  ConfigProvider,
  Form,
  Input,
  Button,
  Checkbox,
  Card,
  Row,
  Col,
  Flex,
  Tooltip,
  Spin,
} from 'antd';
import { antdTheme } from '@/lib/antd-theme';
import { AuthService } from '@/lib/services/auth-service';
import { logger } from '@/lib/env';
import { useAuthStoreHydration } from '@/lib/store/auth-store';
import { notify } from '@/lib/notify';

const { Text, Title } = Typography;

/**
 * 登录表单子组件
 * 使用 useSearchParams 读取 expired 参数，需要被 Suspense 包裹
 */
function LoginForm() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { t } = useI18n();
  const [form] = Form.useForm();

  // Hydrate auth store
  useAuthStoreHydration();

  // 状态管理
  const [loading, setLoading] = useState(false); // 提交中（含限流倒计时）
  const [succeeded, setSucceeded] = useState(false); // 会话已被服务端确认，正在跳转
  const [slowRedirect, setSlowRedirect] = useState(false); // 客户端跳转过慢，已改用整页跳转
  const [error, setError] = useState('');
  const [shake, setShake] = useState(false); // 失败时抖动卡片，避免"点了没反应"的观感
  const [countdown, setCountdown] = useState(0); // P0-2：限流倒计时（秒）
  const countdownTimer = useRef<ReturnType<typeof setInterval> | null>(null);
  // 倒计时期间按钮必须保持 loading，finally 不得解除（见 handleLogin）
  const countdownActive = useRef(false);
  const [rememberMe, setRememberMe] = useState(false);

  // 按钮/输入框忙碌态：提交中或已确认成功但尚未离开登录页
  const busy = loading || succeeded;

  // 清理倒计时定时器
  useEffect(() => {
    return () => {
      if (countdownTimer.current) {
        clearInterval(countdownTimer.current);
      }
    };
  }, []);

  // 启动倒计时
  const startCountdown = (seconds: number) => {
    if (countdownTimer.current) {
      clearInterval(countdownTimer.current);
    }
    countdownActive.current = true;
    setCountdown(seconds);
    setLoading(true);
    countdownTimer.current = setInterval(() => {
      setCountdown(prev => {
        if (prev <= 1) {
          if (countdownTimer.current) {
            clearInterval(countdownTimer.current);
            countdownTimer.current = null;
          }
          countdownActive.current = false;
          setLoading(false);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
  };

  // 检查会话过期标记
  const isExpired = searchParams.get('expired') === 'true';
  const redirectPath = searchParams.get('redirect') || '/dashboard';

  /**
   * 统一的失败反馈出口。
   *
   * 同时做三件事，缺一件用户就容易以为"点了没反应"：
   * 1. 内联 Alert —— 持久可见，可回看具体原因；
   * 2. toast —— 即时打断视线，表单被键盘/滚动遮挡时也能看到；
   * 3. 卡片抖动 —— 不依赖阅读文字的即时反馈。
   */
  const reportFailure = (text: string) => {
    setError(text);
    setShake(true);
    // 不传 context：notify 会自动拼成「<context>失败：...」，
    // 而这里抓到的已经是完整可读的一句话，避免出现"登录失败失败"。
    notify.error(new Error(text), { fallback: text });
  };

  /**
   * 把异常翻译成用户可读的一句话。
   *
   * 后端在凭证错误时直接回内部英文串（handlers/common/service.go 的
   * "invalid credentials"），网络层则可能是 "Failed to fetch"，
   * 两者原样展示对用户都没有意义，这里统一收敛成本地化文案。
   */
  const describeError = (err: unknown): string => {
    // fetch 在网络不通/被拦截时抛 TypeError
    if (err instanceof TypeError) return t('auth.login.networkError');
    const text = (err instanceof Error ? err.message : '').trim();
    if (!text) return t('auth.login.loginFailed');

    const lower = text.toLowerCase();
    if (lower.includes('invalid credentials')) return t('auth.login.invalidCredentials');
    if (lower.includes('user account is inactive')) return t('auth.login.accountInactive');
    return text;
  };

  // 跳转兜底：navigate.push 是客户端导航，必须等目标路由的 RSC 负载返回才切页；
  // 慢环境（首次编译、后端慢）下这一步可能远超预期。此时有两条硬性要求：
  // 1. 绝不能提示"登录失败"——会话已经确认成功，报失败是自相矛盾的假警报；
  // 2. 不能只是干等或弹提示，要真的把用户送进去：改用整页跳转
  //    window.location.assign，绕开客户端路由，由浏览器自己显示加载进度。
  useEffect(() => {
    if (!succeeded) return;
    const timer = setTimeout(() => {
      logger.warn('客户端跳转超时，改用整页跳转:', redirectPath);
      setSlowRedirect(true);
      window.location.assign(redirectPath);
    }, 10000);
    return () => clearTimeout(timer);
  }, [succeeded, redirectPath]);

  // 处理登录提交
  const handleLogin = async (values: { username: string; password: string }) => {
    logger.info('开始登录:', values);
    setLoading(true);
    setError('');
    setSucceeded(false);
    setSlowRedirect(false);

    // 成功跳转中不解锁按钮：置为 true 后只允许"离开本页"，
    // 不允许界面回到可点击的静止态（那正是"没有过渡"的来源）。
    let authenticated = false;

    try {
      const success = await AuthService.login(
        values.username,
        values.password,
        undefined,
        rememberMe
      );

      if (!success) {
        reportFailure(t('auth.login.loginFailed'));
        return;
      }

      authenticated = true;
      logger.info('认证信息已存储，准备跳转');

      // 成功提示必须先于跳转出现：navigate.push 是异步导航，目标页的 RSC 负载
      // 通常还要几百毫秒；这段空窗期若不给任何反馈，用户看到的就是
      // "点了登录，页面没动静"。
      setSucceeded(true);
      notify.success(t('auth.login.loginSuccess'));

      navigate(redirectPath);
      logger.info('已执行跳转命令');
    } catch (err) {
      logger.error('登录错误:', err);
      const e = err as Error & { retryAfterSeconds?: number };
      // P0-2：限流响应携带 retryAfterSeconds，启动倒计时让按钮显示剩余秒数。
      if (typeof e.retryAfterSeconds === 'number' && e.retryAfterSeconds > 0) {
        reportFailure(`${e.message}（${e.retryAfterSeconds} 秒后自动恢复）`);
        startCountdown(e.retryAfterSeconds);
        return;
      }
      reportFailure(describeError(err));
    } finally {
      // 倒计时与跳转各自管理 loading，这里只负责普通失败后解锁按钮
      if (!authenticated && !countdownActive.current) {
        setLoading(false);
      }
    }
  };

  return (
    <Card
      className={`relative rounded-xl shadow-xl border-none ${shake ? 'animate-shake' : ''}`}
      onAnimationEnd={e => {
        // 只响应抖动本身：子元素的 animationend 会冒泡到这里
        if (e.animationName === 'shake') setShake(false);
      }}
      styles={{ body: { padding: '40px' } }}
    >
      <div className='text-center mb-6'>
        <Title level={2} className='!mb-2 !text-gray-900 !text-2xl'>
          {t('auth.login.title')}
        </Title>
        <Text className='!text-gray-500 !text-sm'>{t('auth.login.subtitle')}</Text>
      </div>

      {/* 默认账号提示：开发环境使用 admin/admin123，生产环境必须修改 */}
      {import.meta.env.DEV && (
        <Alert
          title='开发环境默认账号'
          description={
            <span>
              用户名：<strong>admin</strong>　密码：<strong>admin123</strong>
              <br />
              生产环境部署前必须修改默认密码、JWT_SECRET、数据库密码。
            </span>
          }
          type='info'
          className='mb-5'
          showIcon
          closable
        />
      )}

      {/* 会话过期提示 */}
      {isExpired && (
        <Alert
          title='会话已过期'
          description='您的会话已过期，请重新登录。'
          type='warning'
          className='mb-5'
          showIcon
          closable
        />
      )}

      {/* 成功反馈：跳转前的明确确认，同时说明"接下来会发生什么" */}
      {succeeded && (
        <Alert
          title={t('auth.login.loginSuccess')}
          description={t('auth.login.loginSuccessDetail')}
          type='success'
          className='mb-5 animate-slide-down'
          showIcon
        />
      )}

      {error && !succeeded && (
        <Alert
          title={t('auth.login.loginFailed')}
          description={error}
          type='error'
          className='mb-5 animate-slide-down'
          showIcon
          closable
          onClose={() => setError('')}
        />
      )}

      <Form
        form={form}
        onFinish={handleLogin}
        onFinishFailed={({ values, errorFields }) => {
          logger.warn('表单验证失败:', errorFields);
          if (errorFields.length > 0) {
            reportFailure(errorFields[0].errors[0] || t('auth.login.loginFailed'));
          }
        }}
        layout='vertical'
        size='middle'
      >
        <Form.Item
          name='username'
          label={t('auth.login.usernameLabel')}
          rules={[
            { required: true, message: t('auth.login.usernameRequired') },
            { min: 3, message: t('auth.login.usernameMinLength') },
          ]}
        >
          <Input
            prefix={<User size={14} className='text-gray-400' />}
            placeholder={t('auth.login.usernamePlaceholder')}
            disabled={busy}
          />
        </Form.Item>

        <Form.Item
          name='password'
          label={t('auth.login.passwordLabel')}
          rules={[
            { required: true, message: t('auth.login.passwordRequired') },
            { min: 6, message: t('auth.login.passwordMinLength') },
          ]}
        >
          <Input.Password
            prefix={<Lock size={14} className='text-gray-400' />}
            placeholder={t('auth.login.passwordPlaceholder')}
            disabled={busy}
          />
        </Form.Item>

        <Form.Item className='mb-5'>
          <Flex justify='space-between' align='center'>
            <Checkbox
              checked={rememberMe}
              onChange={e => setRememberMe(e.target.checked)}
              disabled={busy}
            >
              {t('auth.login.rememberMe')}
            </Checkbox>
            <Tooltip title={busy ? t('auth.login.loggingIn') : ''}>
              <Link to='/forgot-password'>
                <Button type='link' className='p-0 h-auto text-xs' disabled={busy}>
                  {t('auth.login.forgotPassword')}
                </Button>
              </Link>
            </Tooltip>
          </Flex>
        </Form.Item>

        <Form.Item>
          <Button
            type='primary'
            htmlType='submit'
            loading={busy}
            disabled={countdown > 0}
            size='large'
            className='w-full h-10 rounded-md text-sm font-semibold'
            icon={<ArrowRight size={14} />}
          >
            {countdown > 0
              ? `请等待 ${countdown} 秒`
              : succeeded
                ? t('auth.login.redirecting')
                : loading
                  ? t('auth.login.loggingIn')
                  : t('auth.login.loginButton')}
          </Button>
        </Form.Item>
      </Form>

      <div className='text-center mt-5'>
        <Text className='text-gray-400 text-xs'>
          {t('auth.login.noAccount')}{' '}
          <Link to='/register'>
            <Button type='link' className='p-0 h-auto text-xs'>
              {t('auth.login.registerNow')}
            </Button>
          </Link>
        </Text>
      </div>

      {/* 跳转过渡：从"确认会话成功"到目标页渲染完成之间的视觉衔接。
          延迟 200ms 出现，导航很快时不会闪一下。 */}
      {succeeded && (
        <div
          className='animate-fade-in-late absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 rounded-xl bg-white/90'
          role='status'
          aria-live='polite'
        >
          <Spin size='large' />
          {/* 超时后不报失败，只说"换一种方式进入"，避免与成功提示自相矛盾。
              遮罩不用 backdrop-blur：模糊整块卡片在慢机器上很贵，而它恰好在跳转时显示。 */}
          <Text className='!text-gray-600 !text-sm'>
            {slowRedirect ? t('auth.login.slowRedirect') : t('auth.login.loginSuccessDetail')}
          </Text>
        </div>
      )}
    </Card>
  );
}

/**
 * 登录页面组件
 * 使用统一的设计系统和 Ant Design 组件，保持与系统内部一致的视觉风格
 */
export default function LoginPage() {
  return (
    <ConfigProvider theme={antdTheme}>
      <div className='min-h-screen flex items-center justify-center p-5 bg-gradient-to-br from-gray-50 to-blue-50'>
        <div className='w-full max-w-[1000px]'>
          <Row gutter={[32, 0]} align='middle'>
            {/* 左侧品牌区域 */}
            <Col xs={0} lg={10}>
              <div className='bg-gradient-to-br from-blue-600 to-blue-700 p-10 px-8 rounded-xl h-[480px] flex flex-col justify-center relative overflow-hidden'>
                <div className='absolute -top-8 -right-8 w-32 h-32 bg-white/10 rounded-full blur-xl' />

                <div className='relative z-10'>
                  <Title level={1} className='!text-white !mb-3 !text-3xl !font-bold'>
                    AI-Native ITSM
                  </Title>
                  <Text className='!text-white/90 !text-sm block !mb-8'>
                    AI 驱动的 IT 服务管理系统
                  </Text>

                  <Space orientation='vertical' size='middle' className='w-full'>
                    <Flex align='center' gap={10}>
                      <div className='w-8 h-8 bg-white/20 rounded-md flex items-center justify-center'>
                        <Shield size={16} color='white' />
                      </div>
                      <div>
                        <Text className='!text-white !font-semibold !text-sm block'>
                          企业级安全
                        </Text>
                        <Text className='!text-white/80 !text-xs'>多层安全防护</Text>
                      </div>
                    </Flex>

                    <Flex align='center' gap={10}>
                      <div className='w-8 h-8 bg-white/20 rounded-md flex items-center justify-center'>
                        <ArrowRight size={16} color='white' />
                      </div>
                      <div>
                        <Text className='!text-white !font-semibold !text-sm block'>
                          智能自动化
                        </Text>
                        <Text className='!text-white/80 !text-xs'>AI 驱动流程</Text>
                      </div>
                    </Flex>
                  </Space>
                </div>
              </div>
            </Col>

            {/* 右侧登录表单 — 使用 Suspense 包裹 useSearchParams */}
            <Col xs={24} lg={14} className='animate-scale-in'>
              <Suspense
                fallback={
                  <Card
                    className='rounded-xl shadow-xl border-none'
                    styles={{ body: { padding: '40px' } }}
                  >
                    <div className='text-center py-20 text-gray-400'>加载中...</div>
                  </Card>
                }
              >
                <LoginForm />
              </Suspense>
            </Col>
          </Row>
        </div>
      </div>
    </ConfigProvider>
  );
}
