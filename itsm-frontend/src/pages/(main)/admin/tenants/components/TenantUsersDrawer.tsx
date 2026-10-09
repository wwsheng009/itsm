import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import {
  Alert,
  App,
  Badge,
  Button,
  Drawer,
  Empty,
  Grid,
  Input,
  Modal,
  Result,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { LogOut, RefreshCw, RotateCcw, Search, UserMinus, UserPlus } from 'lucide-react';
import {
  TenantUserAdminApi,
  tenantUserAdminErrorMessage,
  type TenantUserItem,
} from '@/lib/api/tenant-user-admin-api';
import { PRIMARY_ROLE_LABEL, PRIMARY_ROLE_OPTIONS } from '@/lib/api/user-api';
import { usePasswordPolicy } from '@/lib/hooks/usePasswordPolicy';
import UserStatusToggle from '@/components/user/UserStatusToggle';
import ResetPasswordModal from '@/components/user/ResetPasswordModal';

const { Text } = Typography;

export interface TenantUsersDrawerTenant {
  id: number;
  name: string;
  code: string;
  type: string;
}

export interface TenantUsersDrawerProps {
  open: boolean;
  tenant: TenantUsersDrawerTenant | null;
  /** 写操作可用（tenant:write + super_admin；仅 UX 收敛，安全在后端 TUM-D1） */
  canWrite?: boolean;
  /** 空态「去建号」入口（关闭抽屉并打开建号弹窗） */
  onProvision?: () => void;
  onClose: () => void;
}

const PAGE_SIZE_DEFAULT = 20;
const BATCH_LIMIT = 50;

const TENANT_TYPE_LABEL: Record<string, string> = {
  saas_customer: 'SaaS客户',
  msp_provider: 'MSP服务商',
  msp_customer: 'MSP客户',
  internal: '内部组织',
  standard: '标准租户',
};

/** 紧凑时间：列表列宽有限，去掉秒；无效值原样返回。 */
const formatDateTime = (value?: string) => {
  if (!value) return '-';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

/**
 * 平台侧租户用户管理抽屉（TUM-4）。
 * 契约：docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md §4.1–§4.5。
 * - 深链 `?tenantUsers=<tenantId>` 由父页面持有；筛选/分页同步 `tu*` 查询参数（前进/后退可回退）；
 * - 启停乐观更新 + 失败回滚；最后管理员由后端 409 兜底（前端按当前页计数禁用）；
 * - 批量仅本页选择、上限 50，逐条结果 + 失败重试（后端批量端点为 P1，本实现按单条串行聚合）。
 */
const TenantUsersDrawer: React.FC<TenantUsersDrawerProps> = ({
  open,
  tenant,
  canWrite = true,
  onProvision,
  onClose,
}) => {
  const { message, modal } = App.useApp();
  const [searchParams, setSearchParams] = useSearchParams();
  const { hint: passwordHint, buildRules } = usePasswordPolicy();
  const screens = Grid.useBreakpoint();
  /** 桌面优先：宽屏 1160，随断点收敛；窄屏整屏，表格横向滚动由 scroll.x 承接。 */
  const drawerWidth = screens.xxl ? 1160 : screens.xl ? 1080 : screens.lg ? 960 : '100%';

  const [items, setItems] = useState<TenantUserItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<{ code?: string; message: string } | null>(null);

  const [keywordInput, setKeywordInput] = useState('');
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState<string | undefined>();
  const [role, setRole] = useState<string | undefined>();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE_DEFAULT);

  const [resetTarget, setResetTarget] = useState<TenantUserItem | null>(null);
  const [statusLoadingId, setStatusLoadingId] = useState<number | null>(null);
  const [forceLoadingId, setForceLoadingId] = useState<number | null>(null);
  const [offboardLoadingId, setOffboardLoadingId] = useState<number | null>(null);
  const [defaultConfirm, setDefaultConfirm] = useState<{ record: TenantUserItem } | null>(null);
  const [confirmCodeInput, setConfirmCodeInput] = useState('');
  const [selectedKeys, setSelectedKeys] = useState<React.Key[]>([]);
  const [batchRunning, setBatchRunning] = useState(false);
  const [batchFailed, setBatchFailed] = useState<{ id: number; name: string; message: string }[]>([]);
  const initializedRef = useRef(false);

  const tenantId = tenant?.id ?? 0;
  const isDefaultTenant = tenant?.code === 'default';

  // 打开时从 URL 恢复筛选（深链可复现）；关闭时清空 tu* 参数
  useEffect(() => {
    if (open && !initializedRef.current) {
      initializedRef.current = true;
      setKeywordInput(searchParams.get('tuKeyword') ?? '');
      setKeyword(searchParams.get('tuKeyword') ?? '');
      setStatus((searchParams.get('tuStatus') as 'active' | 'inactive' | null) ?? undefined);
      setRole(searchParams.get('tuRole') ?? undefined);
      setPage(Number(searchParams.get('tuPage') ?? '1') || 1);
      setPageSize(Number(searchParams.get('tuPageSize') ?? String(PAGE_SIZE_DEFAULT)) || PAGE_SIZE_DEFAULT);
    }
    if (!open) {
      initializedRef.current = false;
    }
    // 仅在开关变化时初始化，避免 URL 同步回环
  }, [open]);

  const syncUrl = useCallback(
    (next: { keyword?: string; status?: string; role?: string; page?: number; pageSize?: number }) => {
      const params = new URLSearchParams(window.location.search);
      const put = (key: string, value?: string) => {
        if (value) params.set(key, value);
        else params.delete(key);
      };
      put('tuKeyword', next.keyword ?? keyword ?? undefined);
      put('tuStatus', next.status ?? status ?? undefined);
      put('tuRole', next.role ?? role ?? undefined);
      put('tuPage', String(next.page ?? page));
      put('tuPageSize', String(next.pageSize ?? pageSize));
      setSearchParams(params, { replace: true });
    },
    [keyword, status, role, page, pageSize, setSearchParams]
  );

  // 300ms 防抖
  useEffect(() => {
    const timer = window.setTimeout(() => setKeyword(keywordInput.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [keywordInput]);

  const load = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    setLoadError(null);
    try {
      const resp = await TenantUserAdminApi.listUsers(tenantId, {
        page,
        pageSize,
        keyword: keyword || undefined,
        status: status as 'active' | 'inactive' | undefined,
        role: role || undefined,
      });
      setItems(resp.items ?? []);
      setTotal(resp.total ?? 0);
    } catch (error) {
      const e = error as { errorCode?: string; message?: string };
      setLoadError({ code: e?.errorCode, message: tenantUserAdminErrorMessage(error) });
      setItems([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
  }, [tenantId, page, pageSize, keyword, status, role]);

  useEffect(() => {
    if (open && tenantId) {
      void load();
    }
  }, [open, tenantId, load]);

  // 打开新租户时重置选择与失败面板
  useEffect(() => {
    setSelectedKeys([]);
    setBatchFailed([]);
  }, [tenantId]);

  const hasFilters = Boolean(keyword || status || role);

  const clearFilters = () => {
    setKeywordInput('');
    setKeyword('');
    setStatus(undefined);
    setRole(undefined);
    setPage(1);
    syncUrl({ keyword: '', status: '', role: '', page: 1 });
  };

  const activeAdminCount = useMemo(
    () => items.filter(i => i.active && i.role === 'admin').length,
    [items]
  );

  const isLastAdmin = (record: TenantUserItem) =>
    record.role === 'admin' && record.active && activeAdminCount <= 1;

  const handleToggleStatus = async (
    record: TenantUserItem,
    next: boolean,
    confirmCode?: string
  ) => {
    const snapshot = items;
    setItems(prev => prev.map(i => (i.id === record.id ? { ...i, active: next } : i)));
    setStatusLoadingId(record.id);
    try {
      await TenantUserAdminApi.setStatus(tenantId, record.id, { active: next, confirmCode });
      message.success(
        next ? `已启用「${record.username}」` : `已停用「${record.username}」，该用户已立即下线`
      );
    } catch (error) {
      setItems(snapshot); // 乐观更新回滚
      message.error(tenantUserAdminErrorMessage(error));
    } finally {
      setStatusLoadingId(null);
    }
  };

  const handleRequestToggle = (record: TenantUserItem, next: boolean) => {
    if (!next && isDefaultTenant) {
      setConfirmCodeInput('');
      setDefaultConfirm({ record });
      return;
    }
    void handleToggleStatus(record, next);
  };

  const handleForceLogout = async (record: TenantUserItem) => {
    setForceLoadingId(record.id);
    try {
      await TenantUserAdminApi.forceLogout(tenantId, record.id);
      message.success(`已强制下线「${record.username}」`);
    } catch (error) {
      message.error(tenantUserAdminErrorMessage(error));
    } finally {
      setForceLoadingId(null);
    }
  };

  // IP-P2-8 D3=B：平台侧离职编排（停用 + 全量会话失效 + 撤销全部分配；二次确认，幂等可重跑）。
  const handleOffboard = (record: TenantUserItem) => {
    // 走 App 上下文的 modal（静态 Modal.confirm 不消费主题/语言上下文，antd 会告警）。
    modal.confirm({
      title: '确认离职',
      content: `确定要让「${record.username}」离职吗？将停用账号、立即吊销全部会话并撤销其全部分配（可安全重跑）。`,
      okText: '确认离职',
      okButtonProps: { danger: true },
      onOk: async () => {
        setOffboardLoadingId(record.id);
        try {
          const res = await TenantUserAdminApi.offboardUser(tenantId, record.id);
          message.success(
            `已离职「${record.username}」：撤销 ${res.allocationsRevoked} 个分配` +
              (res.allocationsPending > 0 ? `，${res.allocationsPending} 个待重试` : '')
          );
          void load();
        } catch (error) {
          message.error(tenantUserAdminErrorMessage(error));
        } finally {
          setOffboardLoadingId(null);
        }
      },
    });
  };

  const runBatch = async (targets: TenantUserItem[], next: boolean) => {
    setBatchRunning(true);
    const failed: { id: number; name: string; message: string }[] = [];
    for (const target of targets) {
      try {
        await TenantUserAdminApi.setStatus(tenantId, target.id, { active: next });
      } catch (error) {
        failed.push({
          id: target.id,
          name: target.username,
          message: tenantUserAdminErrorMessage(error),
        });
      }
    }
    setBatchFailed(failed);
    setBatchRunning(false);
    setSelectedKeys([]);
    if (failed.length === 0) {
      message.success(`批量${next ? '启用' : '停用'}完成（${targets.length} 条）`);
    } else {
      message.warning(`批量完成：成功 ${targets.length - failed.length} 条，失败 ${failed.length} 条`);
    }
    void load();
  };

  const handleBatch = (next: boolean) => {
    const targets = items.filter(i => selectedKeys.includes(i.id));
    if (targets.length === 0) {
      message.warning('请先选择用户');
      return;
    }
    if (isDefaultTenant && !next) {
      message.warning('平台默认租户请在行内逐条停用（需输入租户编码确认）');
      return;
    }
    void runBatch(targets, next);
  };

  const retryBatchFailed = () => {
    const targets = items.filter(i => batchFailed.some(f => f.id === i.id));
    if (targets.length === 0) {
      message.info('失败项已不在当前页，请刷新后重试');
      return;
    }
    void runBatch(targets, false);
  };

  // 列布局（2026-10-06 优化；业务场景：平台管理员「找人 → 治理」动线）：
  // 身份优先 —— 用户名固定左列（首管标记随行）+ 姓名/邮箱次级行；
  // 角色合并 MSP 角色、状态合并「待改密」，压缩列数；操作固定右列始终可达。
  // 列宽合计 990 + 选择列 ≈ scroll.x；宽屏抽屉（1160）内不产生横向滚动。
  const columns: ColumnsType<TenantUserItem> = [
    {
      title: '用户',
      dataIndex: 'username',
      key: 'username',
      width: 250,
      fixed: 'left',
      render: (value: string, record) => {
        const secondary = [record.name, record.email].filter(Boolean).join(' · ');
        return (
          <div style={{ minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 4, minWidth: 0 }}>
              <Tooltip title={value} mouseEnterDelay={0.4}>
                <span
                  style={{
                    fontWeight: 600,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {value}
                </span>
              </Tooltip>
              {record.isBootstrapAdmin && (
                <Tag color="gold" style={{ marginInlineEnd: 0, flexShrink: 0 }}>
                  首管
                </Tag>
              )}
            </div>
            {secondary && (
              <Tooltip title={secondary} mouseEnterDelay={0.4}>
                <span
                  style={{
                    display: 'block',
                    fontSize: 12,
                    color: 'rgba(0, 0, 0, 0.45)',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {secondary}
                </span>
              </Tooltip>
            )}
          </div>
        );
      },
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 140,
      render: (value: string, record) => (
        <Space direction="vertical" size={2}>
          <Tag color="blue" style={{ marginInlineEnd: 0 }}>
            {PRIMARY_ROLE_LABEL[value] ?? value ?? '-'}
          </Tag>
          {record.mspRole && (
            <Tag color="purple" style={{ marginInlineEnd: 0 }}>
              {record.mspRole}
            </Tag>
          )}
        </Space>
      ),
    },
    {
      title: '部门',
      dataIndex: 'department',
      key: 'department',
      width: 130,
      ellipsis: true,
      render: (v?: string) => v || '-',
    },
    {
      title: '状态',
      dataIndex: 'active',
      key: 'active',
      width: 120,
      render: (active: boolean, record) => {
        const lastAdmin = isLastAdmin(record);
        return (
          <Space direction="vertical" size={2}>
            <UserStatusToggle
              checked={active}
              loading={statusLoadingId === record.id}
              disabled={!canWrite || lastAdmin}
              disabledReason={lastAdmin ? '该租户最后一个管理员不可停用' : undefined}
              checkedChildren="启用"
              unCheckedChildren="停用"
              disableConfirmDescription="该用户将立即掉线，且无法登录；存量会话全部吊销。"
              onToggle={next => handleRequestToggle(record, next)}
            />
            {record.mustChangePassword ? <Badge status="warning" text="待改密" /> : null}
          </Space>
        );
      },
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 150,
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 270,
      fixed: 'right',
      render: (_, record) => (
        <Space size="small">
          <Button size="small" disabled={!canWrite} onClick={() => setResetTarget(record)}>
            重置密码
          </Button>
          <Tooltip title="仅吊销会话，用户需重新登录">
            <Button
              size="small"
              icon={<LogOut size={14} />}
              loading={forceLoadingId === record.id}
              disabled={!canWrite || !record.active}
              onClick={() => void handleForceLogout(record)}
            >
              强制下线
            </Button>
          </Tooltip>
          <Tooltip title="停用账号 + 全量会话失效 + 撤销全部分配">
            <Button
              size="small"
              danger
              icon={<UserMinus size={14} />}
              loading={offboardLoadingId === record.id}
              disabled={!canWrite}
              onClick={() => handleOffboard(record)}
            >
              离职
            </Button>
          </Tooltip>
        </Space>
      ),
    },
  ];

  const errorState = loadError ? (
    loadError.code === 'PLATFORM_SCOPE_REQUIRED' ? (
      <Result
        status="403"
        title="需要平台管理员权限"
        subTitle="租户用户管理仅对平台超级管理员开放（TUM-D1）；租户内用户请前往「用户管理」页面操作。"
      />
    ) : (
      <Result
        status="warning"
        title="加载失败"
        subTitle={loadError.message}
        extra={
          <Button icon={<RefreshCw size={14} />} onClick={() => void load()}>
            重试
          </Button>
        }
      />
    )
  ) : null;

  const emptyState = !loading && items.length === 0 && !loadError;
  const emptyNode = emptyState ? (
    hasFilters ? (
      <Empty description="没有匹配的用户">
        <Button onClick={clearFilters}>清空筛选</Button>
      </Empty>
    ) : (
      <Empty description="该租户暂无用户">
        {onProvision && (
          <Button type="primary" icon={<UserPlus size={14} />} onClick={onProvision}>
            去建号
          </Button>
        )}
      </Empty>
    )
  ) : null;

  return (
    <>
      <Drawer
        title={
          tenant ? (
            <Space size="small" wrap>
              <Text strong>{tenant.name}</Text>
              <Tag>{tenant.code}</Tag>
              <Tag color="cyan">{TENANT_TYPE_LABEL[tenant.type] ?? tenant.type}</Tag>
              <Text type="secondary">共 {total} 个用户</Text>
            </Space>
          ) : (
            '租户用户管理'
          )
        }
        width={drawerWidth}
        open={open}
        onClose={onClose}
        destroyOnHidden
        extra={
          <Button
            icon={<RefreshCw size={14} />}
            onClick={() => void load()}
            aria-label="刷新用户列表"
          >
            刷新
          </Button>
        }
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Space wrap>
            <Input
              allowClear
              prefix={<Search size={14} />}
              placeholder="搜索用户名 / 姓名 / 邮箱"
              style={{ width: 260 }}
              value={keywordInput}
              onChange={e => setKeywordInput(e.target.value)}
              onPressEnter={() => {
                setPage(1);
                syncUrl({ keyword: keywordInput.trim(), page: 1 });
              }}
            />
            <Select
              allowClear
              placeholder="状态"
              style={{ width: 120 }}
              value={status}
              onChange={value => {
                setStatus(value);
                setPage(1);
                syncUrl({ status: value, page: 1 });
              }}
              options={[
                { value: 'active', label: '启用' },
                { value: 'inactive', label: '停用' },
              ]}
            />
            <Select
              allowClear
              placeholder="角色"
              style={{ width: 150 }}
              value={role}
              onChange={value => {
                setRole(value);
                setPage(1);
                syncUrl({ role: value, page: 1 });
              }}
              options={PRIMARY_ROLE_OPTIONS}
            />
          </Space>

          {canWrite && (
            <Space wrap>
              <Text type="secondary">已选 {selectedKeys.length} / {BATCH_LIMIT}</Text>
              <Button
                size="small"
                loading={batchRunning}
                disabled={selectedKeys.length === 0}
                onClick={() => handleBatch(true)}
              >
                批量启用
              </Button>
              <Button
                size="small"
                danger
                loading={batchRunning}
                disabled={selectedKeys.length === 0 || isDefaultTenant}
                onClick={() => handleBatch(false)}
              >
                批量停用
              </Button>
              {batchFailed.length > 0 && (
                <Button size="small" icon={<RotateCcw size={14} />} onClick={retryBatchFailed}>
                  重试失败项（{batchFailed.length}）
                </Button>
              )}
            </Space>
          )}

          {batchFailed.length > 0 && (
            <Alert
              type="warning"
              showIcon
              message={`批量操作有 ${batchFailed.length} 条失败`}
              description={
                <Space direction="vertical" size={2}>
                  {batchFailed.slice(0, 5).map(f => (
                    <Text key={f.id} type="secondary">
                      {f.name}（ID {f.id}）：{f.message}
                    </Text>
                  ))}
                  {batchFailed.length > 5 && (
                    <Text type="secondary">…另有 {batchFailed.length - 5} 条，请重试失败项</Text>
                  )}
                </Space>
              }
            />
          )}

          {errorState ?? (
            <Table<TenantUserItem>
              rowKey="id"
              size="small"
              tableLayout="fixed"
              scroll={{ x: 1024 }}
              loading={loading}
              columns={columns}
              dataSource={items}
              locale={{ emptyText: emptyNode ?? <Empty description="暂无数据" /> }}
              rowSelection={
                canWrite
                  ? {
                      selectedRowKeys: selectedKeys,
                      onChange: keys => {
                        if (keys.length > BATCH_LIMIT) {
                          message.warning(`批量操作上限 ${BATCH_LIMIT} 条`);
                          return;
                        }
                        setSelectedKeys(keys);
                      },
                    }
                  : undefined
              }
              pagination={{
                current: page,
                pageSize,
                total,
                showSizeChanger: true,
                pageSizeOptions: ['10', '20', '50', '100'],
                showTotal: t => `共 ${t} 条`,
                onChange: (nextPage, nextPageSize) => {
                  setPage(nextPage);
                  setPageSize(nextPageSize);
                  syncUrl({ page: nextPage, pageSize: nextPageSize });
                },
              }}
            />
          )}
        </Space>
      </Drawer>

      <ResetPasswordModal
        open={Boolean(resetTarget)}
        user={resetTarget ? { id: resetTarget.id, username: resetTarget.username, name: resetTarget.name } : null}
        modes={['generated', 'specified']}
        policyHint={passwordHint}
        buildRules={buildRules}
        mapError={tenantUserAdminErrorMessage}
        onSubmit={async ({ mode, newPassword }) => {
          if (!resetTarget) return;
          const result = await TenantUserAdminApi.resetPassword(tenantId, resetTarget.id, {
            mode,
            newPassword,
          });
          if (mode === 'specified') {
            message.success(`已重置「${resetTarget.username}」的密码`);
          }
          void load();
          return result;
        }}
        onClose={() => setResetTarget(null)}
      />

      {/* default 平台租户停用二次确认（TUM-D5） */}
      <Modal
        open={Boolean(defaultConfirm)}
        title="停用平台默认租户用户"
        okText="确认停用"
        okButtonProps={{ danger: true, disabled: confirmCodeInput !== 'default' }}
        cancelText="取消"
        onCancel={() => setDefaultConfirm(null)}
        onOk={() => {
          if (defaultConfirm) {
            const record = defaultConfirm.record;
            setDefaultConfirm(null);
            void handleToggleStatus(record, false, confirmCodeInput);
          }
        }}
      >
        <Space direction="vertical" size="small" style={{ width: '100%' }}>
          <Text>该租户为平台默认租户，停用用户需输入租户编码确认。</Text>
          <Input
            placeholder="请输入 default"
            value={confirmCodeInput}
            onChange={e => setConfirmCodeInput(e.target.value)}
          />
        </Space>
      </Modal>
    </>
  );
};

export default TenantUsersDrawer;
