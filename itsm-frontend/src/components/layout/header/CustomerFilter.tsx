/**
 * CustomerFilter —— 顶栏"全局过滤器"（IP-P0-8 / WB-A2、WB-A6）。
 *
 * 定位（方案 §0/§2.1）：多选客户 + "全部客户" + 搜索 + 每客户计数徽标；
 * - 只改视图（URL query customerTenantIds），**不改会话作用域**；
 * - 数据源：GET /api/v1/msp/customers（可访问集合，服务端 allocation 收口）；
 *   徽标：GET /api/v1/msp/workbench/summary（open/slaRisk/unassigned）；
 * - 深度态指示：当前会话已切到客户租户时显示"当前客户：X（深度操作中）"+ 返回工作台，
 *   与工作台态（过滤器生效）文案区分（WB-A6）；
 * - "进入客户"深度入口：POST /api/v1/auth/switch-tenant，成功后整页重载重新引导会话
 *   （会话/store 刷新链路由父会话负责，本组件不依赖未定 API）。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router';
import { Badge, Button, Checkbox, Empty, Input, Popconfirm, Popover, Spin, Tag, Tooltip, message } from 'antd';
import { LogIn, Search, Undo2, Users } from 'lucide-react';
import { DESIGN } from '@/design-system/tokens';
import { useAuthStore } from '@/lib/store/auth-store';
import {
  CUSTOMER_TENANT_IDS_PARAM,
  MSP_WORKBENCH_PATH,
  getWorkbenchSummary,
  listMspCustomers,
  parseCustomerTenantIds,
  type CustomerTenantIdsFilter,
  type MspCustomer,
  type WorkbenchSummaryCustomer,
} from '@/lib/api/msp-workbench-api';

/** summary 契约 ttlSeconds=30，按 TTL 轮询徽标。 */
const SUMMARY_REFRESH_INTERVAL_MS = 30_000;

export interface CustomerFilterProps {
  /** 预留：外部容器定制（Header 集成不需要传参）。 */
  className?: string;
}

function selectedIdsOf(filter: CustomerTenantIdsFilter): number[] {
  return filter === 'all' ? [] : filter;
}

export const CustomerFilter: React.FC<CustomerFilterProps> = ({ className }) => {
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const currentTenant = useAuthStore(state => state.currentTenant);
  const switchTenant = useAuthStore(state => state.switchTenant);

  const [customers, setCustomers] = useState<MspCustomer[]>([]);
  const [loading, setLoading] = useState(false);
  const [summaryByCustomer, setSummaryByCustomer] = useState<
    Map<number, WorkbenchSummaryCustomer>
  >(new Map());
  const [open, setOpen] = useState(false);
  const [searchTerm, setSearchTerm] = useState('');
  const [enteringId, setEnteringId] = useState<number | null>(null);

  const customerParam = searchParams.get(CUSTOMER_TENANT_IDS_PARAM);
  const selected = useMemo(() => parseCustomerTenantIds(customerParam), [customerParam]);
  const selectedIds = selectedIdsOf(selected);

  // 客户列表（仅挂载时拉取；服务端保证只返回有效分配集合）
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listMspCustomers()
      .then(response => {
        if (!cancelled) setCustomers(response?.customers ?? []);
      })
      .catch(() => {
        if (!cancelled) setCustomers([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // 计数徽标（summary 自带 30s TTL，按 TTL 轮询即可）
  const loadSummary = useCallback(async () => {
    try {
      const response = await getWorkbenchSummary();
      setSummaryByCustomer(
        new Map((response?.customers ?? []).map(item => [item.customerTenantId, item]))
      );
    } catch {
      // 徽标失败不影响过滤与列表主链路
    }
  }, []);

  useEffect(() => {
    void loadSummary();
    const timer = window.setInterval(() => {
      void loadSummary();
    }, SUMMARY_REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [loadSummary]);

  const commitSelection = useCallback(
    (nextIds: number[]) => {
      const params = new URLSearchParams(searchParams.toString());
      if (nextIds.length === 0) {
        params.set(CUSTOMER_TENANT_IDS_PARAM, 'all');
      } else {
        params.set(CUSTOMER_TENANT_IDS_PARAM, nextIds.join(','));
      }
      // 过滤条件变化后旧游标失效
      params.delete('cursor');

      if (location.pathname === MSP_WORKBENCH_PATH) {
        setSearchParams(params);
      } else {
        // 顶栏过滤器是工作台的全局过滤器：在其他页面调整后进入工作台查看结果
        const query = params.toString();
        navigate(`${MSP_WORKBENCH_PATH}${query ? `?${query}` : ''}`);
      }
    },
    [location.pathname, navigate, searchParams, setSearchParams]
  );

  const toggleCustomer = useCallback(
    (tenantId: number, checked: boolean) => {
      const base = selectedIds;
      const next = checked ? [...base, tenantId] : base.filter(id => id !== tenantId);
      commitSelection(next);
    },
    [commitSelection, selectedIds]
  );

  const handleEnterCustomer = useCallback(
    async (customer: MspCustomer) => {
      setEnteringId(customer.id);
      try {
        // IP-P0-8 切换链路：取消在途 → 重签 JWT → 重拉 /auth/me → 清空缓存 → 目标首页。
        await switchTenant(customer.id);
        navigate('/');
      } catch (error) {
        message.error(error instanceof Error ? error.message : '进入客户失败');
      } finally {
        setEnteringId(null);
      }
    },
    [navigate, switchTenant]
  );

  const displayCustomers = useMemo(() => {
    const keyword = searchTerm.trim().toLowerCase();
    if (!keyword) return customers;
    return customers.filter(
      customer =>
        customer.name.toLowerCase().includes(keyword) ||
        customer.code.toLowerCase().includes(keyword)
    );
  }, [customers, searchTerm]);

  const badgeCount = useMemo(() => {
    const scope = selected === 'all' ? customers.map(item => item.id) : selectedIds;
    return scope.reduce(
      (sum, tenantId) => sum + (summaryByCustomer.get(tenantId)?.open ?? 0),
      0
    );
  }, [customers, selected, selectedIds, summaryByCustomer]);

  // 深度态：会话已切到客户租户（后端类型为 msp_customer；兼容 customer/saas_customer 旧值）。
  const currentTenantType = (currentTenant as unknown as { type?: string } | null)?.type;
  const isDeepCustomerTenant =
    !!currentTenantType && ['customer', 'msp_customer', 'saas_customer'].includes(currentTenantType);
  const deepTenantName =
    (currentTenant as unknown as { name?: string } | null)?.name ?? '客户';

  const panel = (
    <div style={{ width: 336 }} data-testid="customer-filter-panel">
      <Input
        size="small"
        allowClear
        prefix={<Search size={14} style={{ color: DESIGN.colors.textMuted }} />}
        placeholder="搜索客户名称/编码"
        value={searchTerm}
        onChange={event => setSearchTerm(event.target.value)}
        data-testid="customer-filter-search"
      />

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          margin: '10px 0 4px',
          paddingBottom: 8,
          borderBottom: `1px solid ${DESIGN.colors.border}`,
        }}
      >
        <Checkbox
          checked={selected === 'all'}
          onChange={() => commitSelection([])}
          data-testid="customer-filter-all"
        >
          全部客户
        </Checkbox>
        <span style={{ fontSize: 12, color: DESIGN.colors.textMuted }}>
          共 {customers.length} 个可访问客户
        </span>
      </div>

      <div style={{ maxHeight: 300, overflowY: 'auto' }}>
        {loading ? (
          <div style={{ padding: 24, textAlign: 'center' }}>
            <Spin size="small" />
          </div>
        ) : displayCustomers.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={customers.length === 0 ? '暂无可访问客户' : '无匹配客户'}
            style={{ margin: '16px 0' }}
          />
        ) : (
          displayCustomers.map(customer => {
            const counts = summaryByCustomer.get(customer.id);
            const checked = selectedIds.includes(customer.id);
            return (
              <div
                key={customer.id}
                data-testid={`customer-row-${customer.id}`}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  padding: '8px 4px',
                  borderRadius: DESIGN.radius.md,
                  background: checked ? DESIGN.colors.bgSubtle : 'transparent',
                }}
              >
                <Checkbox
                  checked={checked}
                  onChange={event => toggleCustomer(customer.id, event.target.checked)}
                  data-testid={`customer-checkbox-${customer.id}`}
                />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      fontSize: 13,
                      fontWeight: 500,
                      whiteSpace: 'nowrap',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {customer.name}
                  </div>
                  <div style={{ fontSize: 12, color: DESIGN.colors.textMuted }}>
                    {customer.code}
                  </div>
                </div>
                <Tooltip
                  title={
                    counts
                      ? `待处理 ${counts.open} · 超SLA ${counts.slaRisk} · 未指派 ${counts.unassigned}`
                      : '暂无计数'
                  }
                >
                  <span
                    data-testid={`customer-counts-${customer.id}`}
                    style={{ display: 'inline-flex', gap: 4, flexShrink: 0 }}
                  >
                    <Tag color="blue" style={{ margin: 0 }}>
                      待处理 {counts?.open ?? 0}
                    </Tag>
                    {(counts?.slaRisk ?? 0) > 0 && (
                      <Tag color="red" style={{ margin: 0 }}>
                        超SLA {counts?.slaRisk}
                      </Tag>
                    )}
                  </span>
                </Tooltip>
                <Popconfirm
                  title={`进入客户 ${customer.name}`}
                  description="将切换会话作用域（深度操作），当前工作台过滤不变。"
                  okText="进入"
                  cancelText="取消"
                  onConfirm={() => handleEnterCustomer(customer)}
                >
                  <Button
                    type="text"
                    size="small"
                    loading={enteringId === customer.id}
                    icon={<LogIn size={14} />}
                    aria-label={`进入客户 ${customer.name}`}
                    title="进入客户（深度操作，切换会话）"
                    data-testid={`enter-customer-${customer.id}`}
                  />
                </Popconfirm>
              </div>
            );
          })
        )}
      </div>

      <div
        style={{
          marginTop: 8,
          paddingTop: 8,
          borderTop: `1px solid ${DESIGN.colors.border}`,
          fontSize: 12,
          color: DESIGN.colors.textMuted,
        }}
      >
        已选 {selected === 'all' ? '全部客户' : `${selectedIds.length} 个客户`}
        ；徽标为未关闭工单数（待处理/超SLA）。
      </div>
    </div>
  );

  return (
    <span className={className} style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      {isDeepCustomerTenant && (
        <>
          <Tag color="orange" data-testid="deep-tenant-indicator" style={{ margin: 0 }}>
            当前客户：{deepTenantName}（深度操作中）
          </Tag>
          <Button
            size="small"
            icon={<Undo2 size={14} />}
            onClick={() => navigate(MSP_WORKBENCH_PATH)}
            data-testid="back-to-workbench"
          >
            返回工作台
          </Button>
        </>
      )}
      <Popover
        open={open}
        onOpenChange={setOpen}
        trigger="click"
        placement="bottomLeft"
        arrow={false}
        content={panel}
      >
        <Button
          type="text"
          data-testid="customer-filter-trigger"
          aria-label="客户过滤器"
          title="服务商工作台 · 全局过滤器（只改视图，不改会话）"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            height: 32,
            padding: '0 10px',
            borderRadius: DESIGN.radius.md,
            border: `1px solid ${DESIGN.colors.border}`,
            background: DESIGN.colors.bgSubtle,
            fontSize: 13,
          }}
        >
          <Users size={15} />
          <span>{selected === 'all' ? '全部客户' : `${selectedIds.length} 个客户`}</span>
          <Badge count={badgeCount} size="small" overflowCount={999} />
        </Button>
      </Popover>
    </span>
  );
};

export default CustomerFilter;
