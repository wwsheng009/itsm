import { Link, useNavigate, useSearchParams } from 'react-router';

import React, { Suspense, useState, useEffect, useCallback } from 'react';
import { Card, Typography, Space, Button, Tabs, Badge, Skeleton } from 'antd';
import { Search, Plus, LayoutGrid, Bell, Table } from 'lucide-react';
import TicketList from '@/components/ticket/TicketList';
import TicketKanban from '@/components/ticket/TicketKanban';
import TicketAdvancedSearch, {
  type AdvancedSearchFilters,
} from '@/components/ticket/TicketAdvancedSearch';
import type { TicketQueryFilters } from '@/lib/hooks/useTickets';
import { TicketStatus } from '@/lib/services/ticket-service';
import {
  saveFilters,
  restoreFilters,
  clearFilters,
  getDefaultFilters,
} from '@/lib/utils/filter-persistence';
import { useI18n } from '@/lib/i18n/useI18n';

const { Title, Text } = Typography;

// 内容组件，使用 useSearchParams
function TicketsPageContent() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [activeTab, setActiveTab] = useState('list');
  const [showAdvancedSearch, setShowAdvancedSearch] = useState(false);

  // 从 localStorage 恢复筛选条件
  // 必须剔除 isOverdue：它只应由 URL 的 overdue 参数决定。
  // 否则旧版本写进缓存的 isOverdue=true 会在每次进页面时「复活」预警筛选。
  const [advancedFilters, setAdvancedFilters] = useState<Partial<TicketQueryFilters>>(() => {
    const defaults = getDefaultFilters('tickets') as Partial<TicketQueryFilters>;
    const restored = restoreFilters('tickets', defaults) as Partial<TicketQueryFilters>;
    const { isOverdue: _restoredOverdue, ...safeFilters } = restored;
    return safeFilters;
  });

  const [ticketStats, setTicketStats] = useState({
    total: 0,
    open: 0,
    overdue: 0,
    today: 0,
  });

  // 列表实际匹配总数（受筛选条件影响）。null 表示尚未拿到，
  // 用于让 Tab 计数与列表结果保持一致，避免「筛选中显示 8、列表只有 5」的矛盾。
  const [listTotal, setListTotal] = useState<number | null>(null);

  // 保存筛选条件到 localStorage
  // 注意：isOverdue 是「SLA 预警」的临时视图开关，由 URL 的 overdue 参数权威控制，
  // 不写入缓存，否则刷新/重新进入页面时会把已关闭的预警条件重新恢复出来。
  useEffect(() => {
    const { isOverdue: _ignoredOverdue, ...persistableFilters } = advancedFilters;
    saveFilters('tickets', persistableFilters as Record<string, unknown>);
  }, [advancedFilters]);

  // 从 URL 参数获取当前标签页和高级搜索状态
  useEffect(() => {
    const tab = searchParams.get('tab');
    // 分析视图是独立页面 /tickets/analytics，本页无对应内容区，
    // 直接渲染会空白，统一重导航
    if (tab === 'analytics') {
      navigate('/tickets/analytics', { replace: true });
      return;
    }
    if (tab && ['list', 'kanban', 'search'].includes(tab)) {
      setActiveTab(tab);
    }
    // 从 URL 恢复高级搜索面板状态
    const search = searchParams.get('search');
    if (search === 'advanced') {
      setShowAdvancedSearch(true);
    }
    // 支持 Dashboard KPI 磁贴下转：/tickets?status=xxx 直接按状态过滤列表
    const statusParam = searchParams.get('status');
    if (statusParam) {
      const validStatuses = Object.values(TicketStatus);
      if (validStatuses.includes(statusParam as TicketStatus)) {
        setAdvancedFilters(prev => ({ ...prev, status: statusParam as TicketStatus }));
        setActiveTab('list');
      }
    }
    // SLA 预警：以 URL 的 overdue 参数为权威做「双向同步」。
    // 旧实现只在 overdue=true 时置 true，参数被移除后筛选条件仍残留在 state 里，
    // 导致按钮看起来生效一次后永久失灵、且无法取消。
    const overdueFromUrl = searchParams.get('overdue') === 'true';
    setAdvancedFilters(prev => {
      const current = prev.isOverdue === true;
      if (current === overdueFromUrl) return prev;
      const next = { ...prev };
      if (overdueFromUrl) {
        next.isOverdue = true;
      } else {
        delete next.isOverdue;
      }
      return next;
    });
    if (overdueFromUrl) {
      setActiveTab('list');
    }
  }, [searchParams, navigate]);

  // 获取工单统计数据
  const fetchTicketStats = useCallback(async () => {
    try {
      const { ticketService } = await import('@/lib/services/ticket-service');
      const stats = await ticketService.getTicketStats();
      setTicketStats({
        total: stats.total,
        open: stats.open,
        overdue: stats.overdue || 0,
        today: 0, // 暂时没有今日新增的 API
      });
    } catch (error) {
      console.error('Failed to fetch ticket stats:', error);
    }
  }, []);

  useEffect(() => {
    fetchTicketStats();
  }, [fetchTicketStats]);

  // 处理标签页切换
  const handleTabChange = (tab: string) => {
    // 分析页是独立路由（带完整图表/导出），本页 tab 无内容区，点 analytics 直接导航
    if (tab === 'analytics') {
      navigate('/tickets/analytics');
      return;
    }
    setActiveTab(tab);
    const newParams = new URLSearchParams(searchParams.toString());
    newParams.set('tab', tab);
    navigate(`/tickets?${newParams.toString()}`, { preventScrollReset: true });
  };

  const mapAdvancedToQueryFilters = useCallback(
    (filters: AdvancedSearchFilters): Partial<TicketQueryFilters> => {
      const result: Partial<TicketQueryFilters> = {};

      if (filters.keyword) {
        result.keyword = filters.keyword;
      } else if (filters.ticketNumber) {
        result.keyword = filters.ticketNumber;
      } else if (filters.title) {
        result.keyword = filters.title;
      } else if (filters.description) {
        result.keyword = filters.description;
      }

      if (filters.status && filters.status.length > 0) {
        result.status = filters.status[0] as TicketQueryFilters['status'];
      }

      if (filters.priority && filters.priority.length > 0) {
        result.priority = filters.priority[0] as TicketQueryFilters['priority'];
      }

      if (filters.type && filters.type.length > 0) {
        result.type = filters.type[0] as TicketQueryFilters['type'];
      }

      if (filters.category && filters.category.length > 0) {
        result.category = filters.category[0];
      }

      if (typeof filters.assigneeId === 'number') {
        result.assigneeId = filters.assigneeId;
      }

      if (filters.createdAfter && filters.createdBefore) {
        result.dateRange = [filters.createdAfter, filters.createdBefore];
      }

      return result;
    },
    []
  );

  // 高级搜索应用/重置时，必须把 URL 里的 overdue 一并移除，
  // 否则 URL 与 advancedFilters 不一致，旧参数会把已清除的筛选「复活」。
  const stripOverdueParam = useCallback(() => {
    if (searchParams.get('overdue') !== 'true') return;
    const newParams = new URLSearchParams(searchParams.toString());
    newParams.delete('overdue');
    navigate(`/tickets?${newParams.toString()}`, { preventScrollReset: true });
  }, [searchParams, navigate]);

  const handleAdvancedSearch = (filters: AdvancedSearchFilters) => {
    const mapped = mapAdvancedToQueryFilters(filters);
    setAdvancedFilters(mapped);
    setActiveTab('list');
    stripOverdueParam();
  };

  const handleSearchReset = () => {
    clearFilters('tickets');
    setAdvancedFilters({});
    stripOverdueParam();
  };

  return (
    <div className="min-h-screen bg-[var(--color-background-primary)]">
      {/* 页面头部 */}
      <div className="page-chrome border-b">
        <div className="w-full px-6 py-4">
          <div className="flex items-center justify-between">
            <div>
              <Title level={2} style={{ marginBottom: 0 }}>
                {t('tickets.title')}
              </Title>
              <Text type="secondary">
                {t('tickets.description')}
              </Text>
            </div>
            <Space>
              <Button
                icon={<Search />}
                onClick={() => {
                  const newShow = !showAdvancedSearch;
                  setShowAdvancedSearch(newShow);
                  const newParams = new URLSearchParams(searchParams.toString());
                  if (newShow) {
                    newParams.set('search', 'advanced');
                  } else {
                    newParams.delete('search');
                  }
                  navigate(`/tickets?${newParams.toString()}`, { preventScrollReset: true });
                }}
              >
                {t('tickets.advancedSearch')}
              </Button>
              <Badge count={ticketStats.overdue} size="small">
                <Button
                  icon={<Bell />}
                  type={advancedFilters.isOverdue ? 'primary' : 'default'}
                  aria-pressed={advancedFilters.isOverdue === true}
                  onClick={() => {
                    const nextOn = advancedFilters.isOverdue !== true;
                    // 本地状态立即翻转，保证有即时视觉反馈（即使 URL 未变）
                    setAdvancedFilters(prev => {
                      const next = { ...prev };
                      if (nextOn) {
                        next.isOverdue = true;
                      } else {
                        delete next.isOverdue;
                      }
                      return next;
                    });
                    setActiveTab('list');
                    // 增量修改 URL 参数，不硬编码整串，避免丢掉 search/status 等其它参数
                    const newParams = new URLSearchParams(searchParams.toString());
                    newParams.set('tab', 'list');
                    if (nextOn) {
                      newParams.set('overdue', 'true');
                    } else {
                      newParams.delete('overdue');
                    }
                    navigate(`/tickets?${newParams.toString()}`, { preventScrollReset: true });
                  }}
                >
                  {t('tickets.slaWarning')}
                </Button>
              </Badge>
              <Link to="/tickets/create">
                <Button type="primary" icon={<Plus />}>
                  {t('tickets.create')}
                </Button>
              </Link>
            </Space>
          </div>

          {/* 统计数据栏 */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mt-4">
            <Card size="small" className="rounded-lg shadow-sm">
              <div className="flex items-center justify-between">
                <div>
                  <Text type="secondary">{t('tickets.stats.total')}</Text>
                  <div className="text-2xl font-bold">{ticketStats.total}</div>
                </div>
                <Table className="text-2xl text-blue-500" />
              </div>
            </Card>
            <Card size="small" className="rounded-lg shadow-sm">
              <div className="flex items-center justify-between">
                <div>
                  <Text type="secondary">{t('tickets.stats.open')}</Text>
                  <div className="text-2xl font-bold text-orange-500">{ticketStats.open}</div>
                </div>
                <Bell className="text-2xl text-orange-500" />
              </div>
            </Card>
            <Card size="small" className="rounded-lg shadow-sm">
              <div className="flex items-center justify-between">
                <div>
                  <Text type="secondary">{t('tickets.stats.overdue')}</Text>
                  <div className="text-2xl font-bold text-red-500">{ticketStats.overdue}</div>
                </div>
                <Bell className="text-2xl text-red-500" />
              </div>
            </Card>
            <Card size="small" className="rounded-lg shadow-sm">
              <div className="flex items-center justify-between">
                <div>
                  <Text type="secondary">{t('tickets.stats.today')}</Text>
                  <div className="text-2xl font-bold text-green-500">{ticketStats.today}</div>
                </div>
                <Plus className="text-2xl text-green-500" />
              </div>
            </Card>
          </div>
        </div>
      </div>

      {/* 高级搜索面板 */}
      {showAdvancedSearch && (
        <div className="bg-gray-50 border-b border-gray-200">
          <div className="w-full px-6 py-4 bg-[var(--color-background-tertiary)] border-b border-[var(--color-border-primary)]">
            <TicketAdvancedSearch onSearch={handleAdvancedSearch} onReset={handleSearchReset} />
          </div>
        </div>
      )}

      {/* 主内容区域 */}
      <div className="w-full px-6 py-6">
        {/* 标签页导航 */}
        <Tabs
          activeKey={activeTab}
          onChange={handleTabChange}
          size="large"
          className="mb-6"
          items={[
            {
              key: 'list',
              label: (
                <span className="flex items-center gap-2">
                  <Table />
                  {t('tickets.tabs.list')} ({listTotal ?? ticketStats.total})
                </span>
              ),
            },
            {
              key: 'kanban',
              label: (
                <span className="flex items-center gap-2">
                  <LayoutGrid />
                  {t('tickets.tabs.kanban')}
                </span>
              ),
            },
            {
              key: 'analytics',
              label: t('tickets.tabs.analytics'),
            },
          ]}
        />

        {/* 标签页内容 */}
        {activeTab === 'list' && (
          <TicketList
            showHeader={false}
            pageSize={20}
            advancedFilters={advancedFilters}
            onTotalChange={setListTotal}
          />
        )}

        {activeTab === 'kanban' && (
          <TicketKanban onTicketSelect={ticket => navigate(`/tickets/${ticket.id}`)} />
        )}
      </div>

      {/* 快捷操作浮动按钮 */}
      <div className="fixed bottom-6 right-6 z-50">
        <Space orientation="vertical" size="middle">
          <Button
            type="primary"
            shape="circle"
            size="large"
            icon={<Plus />}
            onClick={() => navigate('/tickets/create')}
            className="shadow-lg hover:scale-110 transition-transform"
          />
        </Space>
      </div>
    </div>
  );
}

// Loading fallback 组件
function TicketsPageSkeleton() {
  return (
    <div className="min-h-screen bg-[var(--color-background-primary)] p-6">
      <Card className="mb-6">
        <Skeleton active paragraph={{ rows: 2 }} />
      </Card>
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
        {[1, 2, 3, 4].map(i => (
          <Card key={i} size="small">
            <Skeleton active paragraph={{ rows: 1 }} />
          </Card>
        ))}
      </div>
      <Card>
        <Skeleton active paragraph={{ rows: 10 }} />
      </Card>
    </div>
  );
}

// 主页面组件，用 Suspense 包裹
export default function TicketsPage() {
  return (
    <Suspense fallback={<TicketsPageSkeleton />}>
      <TicketsPageContent />
    </Suspense>
  );
}
