/**
 * TenantOnboardingModal：租户开通闭环（模板供给 → 首个管理员 → 可用）。
 *
 * 对接冻结契约：
 * - GET  /api/v1/tenants/:id/readiness       → TenantReadinessResponse
 * - POST /api/v1/tenants/:id/provision       → TenantReadinessResponse（幂等）
 * - POST /api/v1/tenants/:id/bootstrap-admin → BootstrapAdminResponse
 *   （已存在首管时 HTTP 409 / envelope code 4090，前端降级为“已创建”态）
 *
 * 约定：
 * - 打开时拉取 readiness；关闭后再打开重新拉取（destroyOnHidden + effect 重置），避免陈旧；
 * - fetchReadiness / provision / createAdmin 可注入（测试替身），默认走 TenantAPI；
 * - 服务端生成的密码仅在响应中回传一次，弹窗内显著提示“仅显示一次，请立即保存”。
 */
import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Modal, Space, Steps, Tag, Typography } from 'antd';
import { Copy, Rocket } from 'lucide-react';
import { TenantAPI } from '@/lib/api/tenant-api';
import type {
  BootstrapAdminRequest,
  BootstrapAdminResponse,
  TenantReadinessResponse,
} from '@/lib/api/api-config';

const { Text, Title } = Typography;

export type OnboardingTenant = {
  id: number;
  name: string;
  code: string;
  domain?: string;
};

export type TenantOnboardingModalProps = {
  open: boolean;
  tenant?: OnboardingTenant;
  onClose: () => void;
  /** 可注入的 readiness 拉取（缺省 TenantAPI.getTenantReadiness）。 */
  fetchReadiness?: (id: number) => Promise<TenantReadinessResponse>;
  /** 可注入的模板供给（缺省 TenantAPI.provisionTenant）。 */
  provision?: (id: number, templateVersion?: string) => Promise<TenantReadinessResponse>;
  /** 可注入的首管创建（缺省 TenantAPI.createBootstrapAdmin）。 */
  createAdmin?: (id: number, payload: BootstrapAdminRequest) => Promise<BootstrapAdminResponse>;
};

type AdminFormValues = {
  username?: string;
  email?: string;
  password?: string;
};

/** 已存在首管：HTTP 409 或 envelope code 4090。 */
export function isBootstrapConflict(error: unknown): boolean {
  const err = error as { code?: number; httpStatus?: number } | null | undefined;
  return err?.code === 4090 || err?.httpStatus === 409;
}

export default function TenantOnboardingModal({
  open,
  tenant,
  onClose,
  fetchReadiness,
  provision,
  createAdmin,
}: TenantOnboardingModalProps) {
  const tenantId = tenant?.id;
  const [form] = Form.useForm<AdminFormValues>();

  const [step, setStep] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [readiness, setReadiness] = useState<TenantReadinessResponse | null>(null);
  const [provisionLoading, setProvisionLoading] = useState(false);
  const [provisionError, setProvisionError] = useState<string | null>(null);
  const [adminLoading, setAdminLoading] = useState(false);
  const [adminError, setAdminError] = useState<string | null>(null);
  const [adminCreated, setAdminCreated] = useState(false);
  const [adminConflict, setAdminConflict] = useState(false);
  const [createdAdmin, setCreatedAdmin] = useState<BootstrapAdminResponse | null>(null);
  const [copied, setCopied] = useState(false);

  const loadReadiness = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await (fetchReadiness ?? TenantAPI.getTenantReadiness)(tenantId);
      setReadiness(res);
    } catch (err: unknown) {
      setError(err instanceof Error && err.message ? err.message : '租户就绪度加载失败');
    } finally {
      setLoading(false);
    }
  }, [tenantId, fetchReadiness]);

  // 打开时重置并重新拉取；关闭→再打开不复用旧数据。
  useEffect(() => {
    if (!open || !tenantId) return;
    setStep(0);
    setReadiness(null);
    setError(null);
    setProvisionLoading(false);
    setProvisionError(null);
    setAdminLoading(false);
    setAdminError(null);
    setAdminCreated(false);
    setAdminConflict(false);
    setCreatedAdmin(null);
    setCopied(false);
    loadReadiness();
  }, [open, tenantId, loadReadiness]);

  const handleProvision = async () => {
    if (!tenantId) return;
    setProvisionLoading(true);
    setProvisionError(null);
    try {
      const res = await (provision ?? TenantAPI.provisionTenant)(tenantId);
      // provision 与 readiness 同构：直接用返回结构刷新 items / ready / templateVersion。
      setReadiness(res);
    } catch (err: unknown) {
      setProvisionError(err instanceof Error && err.message ? err.message : '模板开通失败');
    } finally {
      setProvisionLoading(false);
    }
  };

  const handleCreateAdmin = async () => {
    if (!tenantId) return;
    let values: AdminFormValues;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }

    const payload: BootstrapAdminRequest = {};
    const username = values.username?.trim();
    if (username) payload.username = username;
    const email = values.email?.trim();
    if (email) payload.email = email;
    if (values.password) payload.password = values.password;

    setAdminLoading(true);
    setAdminError(null);
    try {
      const res = await (createAdmin ?? TenantAPI.createBootstrapAdmin)(tenantId, payload);
      setCreatedAdmin(res);
      setAdminCreated(true);
    } catch (err: unknown) {
      if (isBootstrapConflict(err)) {
        // 并发/重复提交：后端已有首管，等价于已创建。
        setAdminConflict(true);
        setAdminCreated(true);
      } else {
        setAdminError(err instanceof Error && err.message ? err.message : '创建管理员失败');
      }
    } finally {
      setAdminLoading(false);
    }
  };

  const handleCopyPassword = async () => {
    const password = createdAdmin?.password;
    if (!password) return;
    try {
      await navigator.clipboard?.writeText(password);
      setCopied(true);
    } catch {
      // 剪贴板不可用（非安全上下文/权限拒绝）：文本框本身可手动复制。
    }
  };

  const created = adminCreated || (readiness?.bootstrapAdmins ?? 0) > 0;
  const nextDisabled = step === 0 ? !readiness : step === 1 ? !created : false;

  const footer = [
    step > 0 ? (
      <Button key="prev" onClick={() => setStep((s) => Math.max(0, s - 1))} data-testid="previous-step">
        上一步
      </Button>
    ) : null,
    step < 2 ? (
      <Button
        key="next"
        type="primary"
        disabled={nextDisabled}
        onClick={() => setStep((s) => Math.min(2, s + 1))}
        data-testid="next-step"
      >
        下一步
      </Button>
    ) : (
      <Button key="finish" type="primary" onClick={onClose} data-testid="finish-onboarding">
        完成
      </Button>
    ),
  ];

  return (
    <Modal
      open={open}
      title={
        <span>
          <Rocket className="inline-block w-4 h-4 mr-2" />
          租户开通{tenant ? ` · ${tenant.name}` : ''}
        </span>
      }
      onCancel={onClose}
      footer={footer}
      width={640}
      destroyOnHidden
      data-testid="tenant-onboarding-modal"
    >
      <Steps
        current={step}
        size="small"
        className="my-4"
        items={[{ title: '模板供给' }, { title: '首个管理员' }, { title: '完成' }]}
      />

      {step === 0 ? (
        <div data-testid="onboarding-step-provision">
          {loading ? <Text type="secondary">加载中…</Text> : null}

          {!loading && error ? (
            <Alert
              type="error"
              showIcon
              message={error}
              data-testid="readiness-error"
              action={
                <Button size="small" onClick={loadReadiness} data-testid="readiness-retry">
                  重试
                </Button>
              }
            />
          ) : null}

          {!loading && !error && readiness ? (
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <Space size={8} wrap>
                <Text type="secondary">
                  模板版本：{readiness.templateVersion !== undefined ? readiness.templateVersion : '-'}
                </Text>
                {readiness.ready ? (
                  <Tag color="success" data-testid="readiness-ready">
                    已就绪
                  </Tag>
                ) : (
                  <Tag color="warning" data-testid="readiness-not-ready">
                    待供给
                  </Tag>
                )}
              </Space>

              <div data-testid="readiness-items">
                {readiness.items.map((item) => (
                  <div
                    key={item.key}
                    className="flex items-center justify-between py-1"
                    data-testid={`readiness-item-${item.key}`}
                  >
                    <Text>{item.label}</Text>
                    {item.required && item.count === 0 ? (
                      <Text type="danger">缺失</Text>
                    ) : (
                      <Text type={item.required ? undefined : 'secondary'}>{item.count}</Text>
                    )}
                  </div>
                ))}
              </div>

              {provisionError ? (
                <Alert type="error" showIcon message={provisionError} data-testid="provision-error" />
              ) : null}

              <Button
                type="primary"
                icon={<Rocket className="w-4 h-4" />}
                loading={provisionLoading}
                onClick={handleProvision}
                data-testid="provision-button"
              >
                开通模板
              </Button>
            </Space>
          ) : null}
        </div>
      ) : null}

      {step === 1 ? (
        <div data-testid="onboarding-step-admin">
          {created ? (
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <Alert
                type="success"
                showIcon
                message={adminConflict ? '该租户已存在首个管理员' : '首个管理员已创建'}
                data-testid="admin-created"
              />
              {createdAdmin ? (
                <Text type="secondary">
                  账号：{createdAdmin.username}
                  {createdAdmin.email ? `（${createdAdmin.email}）` : ''}
                </Text>
              ) : null}
              {createdAdmin?.generated && createdAdmin.password ? (
                <div data-testid="one-time-password-block">
                  <Alert
                    type="warning"
                    showIcon
                    message="一次性密码仅显示一次，请立即保存"
                    description="关闭弹窗后无法再次查看；该管理员首次登录将强制修改密码。"
                  />
                  <Space.Compact style={{ width: '100%', marginTop: 8 }}>
                    <Input
                      readOnly
                      value={createdAdmin.password}
                      data-testid="one-time-password"
                      aria-label="一次性密码"
                    />
                    <Button
                      icon={<Copy className="w-4 h-4" />}
                      onClick={handleCopyPassword}
                      data-testid="copy-password-button"
                    >
                      {copied ? '已复制' : '复制'}
                    </Button>
                  </Space.Compact>
                </div>
              ) : null}
            </Space>
          ) : (
            <Form form={form} layout="vertical">
              <Form.Item label="用户名" name="username">
                <Input
                  placeholder={`admin-${tenant?.code ?? ''}`}
                  data-testid="bootstrap-username"
                  autoComplete="off"
                />
              </Form.Item>
              <Form.Item
                label="邮箱"
                name="email"
                rules={[{ type: 'email', message: '请输入正确的邮箱' }]}
              >
                <Input placeholder="可选" data-testid="bootstrap-email" autoComplete="off" />
              </Form.Item>
              <Form.Item label="密码" name="password">
                <Input.Password
                  placeholder="留空自动生成 12–128 位"
                  data-testid="bootstrap-password"
                  autoComplete="new-password"
                />
              </Form.Item>

              {adminError ? (
                <Alert
                  type="error"
                  showIcon
                  message={adminError}
                  className="mb-4"
                  data-testid="admin-error"
                />
              ) : null}

              <Button
                type="primary"
                loading={adminLoading}
                onClick={handleCreateAdmin}
                data-testid="create-admin-button"
              >
                创建管理员
              </Button>
            </Form>
          )}
        </div>
      ) : null}

      {step === 2 ? (
        <Space direction="vertical" size={12} style={{ width: '100%' }} data-testid="onboarding-step-done">
          <Alert
            type="success"
            showIcon
            message={`租户 ${tenant?.name ?? ''} 开通流程已完成`}
            data-testid="onboarding-done"
          />
          <div>
            <Title level={5} className="!mb-1">
              租户信息
            </Title>
            <Text>租户编码：{tenant?.code ?? '-'}</Text>
            <br />
            <Text>访问域名：{tenant?.domain || '未配置'}</Text>
          </div>
          <div>
            <Title level={5} className="!mb-1">
              登录提示
            </Title>
            <Text>
              管理员账号：
              {createdAdmin?.username || (adminConflict ? '沿用已存在账号' : `admin-${tenant?.code ?? ''}`)}
            </Text>
            <br />
            <Text type="secondary">首次登录必须修改密码（mustChangePassword）。</Text>
          </div>
        </Space>
      ) : null}
    </Modal>
  );
}
