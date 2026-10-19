
import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router';
import {
  Alert,
  Card,
  Descriptions,
  Divider,
  Table,
  Select,
  Space,
  Tag,
  Typography,
  Button,
  Empty,
  App,
  Input,
  Modal,
  Tooltip,
} from 'antd';
import {
  Activity,
  Bell,
  CheckCircle2,
  Clock,
  PauseCircle,
  RotateCcw,
  Settings2,
  XCircle,
  RefreshCw,
  ShieldAlert,
} from 'lucide-react';

import {
  aiGetToolApprovals,
  aiApproveTool,
  aiListAutopilotRuns,
  aiCancelAutopilotRun,
  aiRollbackAutopilotRun,
  type AutopilotRunSummary,
  type ToolApproval,
  type ToolApprovalListResponse,
} from '@/lib/api/ai-api';
import { mcpApi, type MCPServer } from '@/lib/api/mcp-api';
import {
  ToolInvocationDetail,
  ToolSourceTag,
  formatToolSource,
  prettyArgs,
  riskColor,
} from '@/components/ai/tool-invocation-detail';
import { targetUrl } from '@/components/ai/evidence-panel';
import { formatRemaining } from '@/components/ai/confirmation-drawer';
import DryRunSnapshot from '@/components/ai/dry-run-snapshot';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { useAuthStoreHydration } from '@/lib/store/auth-store';
import { runStatusColor } from '@/components/ai/run-trace/trace-model';

const { Title, Text } = Typography;

const STATE_LABELS: Record<string, string> = {
  pending: '待审批',
  approved: '已通过',
  rejected: '已驳回',
  auto: '自动执行',
};

const stateColor = (s: string): string => {
  switch (s) {
    case 'pending':
      return 'gold';
    case 'approved':
      return 'green';
    case 'rejected':
      return 'red';
    default:
      return 'default';
  }
};

const permissionColor = (p?: string): string => {
  if (p === 'passed') return 'green';
  if (p === 'denied') return 'red';
  if (p === 'skipped') return 'default';
  return 'default';
};

/** 来源筛选（M1-06）：与后端 `?provider=` 取值对齐。 */
const PROVIDER_OPTIONS = [
  { value: '', label: '全部来源' },
  { value: 'builtin', label: '内置工具' },
  { value: 'mcp', label: 'MCP 外部工具' },
];

/** 来源徽标口径与审计页共用（见 `components/ai/tool-invocation-detail`）。 */
export const formatSource = formatToolSource;

const AIApprovalQueue: React.FC = () => {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const { hasPermission } = usePermissions();
  useAuthStoreHydration();

  const [items, setItems] = useState<ToolApproval[]>([]);
  const [loading, setLoading] = useState(false);
  const [state, setState] = useState<string>('pending');
  // 来源维度筛选（M1-06）：过滤由后端完成，前端不做二次筛选。
  const [provider, setProvider] = useState<string>('');
  const [server, setServer] = useState<string>('');
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [serversUnavailable, setServersUnavailable] = useState(false);
  const [rejectId, setRejectId] = useState<number | null>(null);
  const [rejectReason, setRejectReason] = useState('');
  // B1-09：过期倒计时（仅当列表存在「待审批且有 expiresAt」的行时起 1s 定时器）。
  const [nowTick, setNowTick] = useState(() => Date.now());
  const hasExpiring = useMemo(
    () => items.some(r => r.approvalState === 'pending' && !!(r.expiresAt && !Number.isNaN(Date.parse(r.expiresAt)))),
    [items]
  );

  useEffect(() => {
    if (!hasExpiring) return undefined;
    const timer = window.setInterval(() => setNowTick(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [hasExpiring]);

  /** 待审批但已过确认单有效期（B1-05 状态机：过期不会被执行，也不会被补批准）。 */
  const isExpired = useCallback(
    (r: ToolApproval): boolean => {
      if (r.approvalState !== 'pending' || !r.expiresAt) return false;
      const exp = Date.parse(r.expiresAt);
      return !Number.isNaN(exp) && nowTick >= exp;
    },
    [nowTick]
  );

  // I2-01 Autopilot 监督面：运行列表 + 暂停/回滚 + 按运行联动审批。
  const [runs, setRuns] = useState<AutopilotRunSummary[]>([]);
  const [runsLoading, setRunsLoading] = useState(false);
  const [runStatusFilter, setRunStatusFilter] = useState('running');
  const [actingRunId, setActingRunId] = useState<number | null>(null);
  const [runIdFilter, setRunIdFilter] = useState<number | null>(null);

  const fetchRuns = useCallback(async () => {
    setRunsLoading(true);
    try {
      const res = await aiListAutopilotRuns({ days: 7, status: runStatusFilter, limit: 20 });
      setRuns(res.items);
    } catch (e) {
      message.error(`加载运行列表失败：${(e as Error).message}`);
    } finally {
      setRunsLoading(false);
    }
  }, [runStatusFilter, message]);

  useEffect(() => {
    void fetchRuns();
  }, [fetchRuns]);

  const fetchList = useCallback(async () => {
    setLoading(true);
    try {
      // I2-01 审批联动：runId 仅在选中时携带（保持既有调用形状与断言不变）。
      const filter = { provider, server } as { provider: string; server: string; runId?: number };
      if (runIdFilter) filter.runId = runIdFilter;
      const res: ToolApprovalListResponse = await aiGetToolApprovals(state, filter);
      setItems(res.items ?? []);
    } catch (e) {
      message.error(`加载审批队列失败：${(e as Error).message}`);
    } finally {
      setLoading(false);
    }
  }, [state, provider, server, runIdFilter, message]);

  useEffect(() => {
    fetchList();
  }, [fetchList]);

  const handlePauseRun = useCallback(
    async (run: AutopilotRunSummary) => {
      setActingRunId(run.id);
      try {
        const res = await aiCancelAutopilotRun(run.id, 'paused_from_supervision');
        message.success(res.cancelled ? `已暂停运行 #${run.id}` : `运行 #${run.id} 已收口，无需暂停`);
        await Promise.all([fetchRuns(), fetchList()]);
      } catch (e) {
        message.error(`暂停失败：${(e as Error).message}`);
      } finally {
        setActingRunId(null);
      }
    },
    [fetchRuns, fetchList, message]
  );

  const handleRollbackRun = useCallback(
    async (run: AutopilotRunSummary) => {
      setActingRunId(run.id);
      try {
        const res = await aiRollbackAutopilotRun(run.id, 'rollback_from_supervision');
        // 回滚是写动作闸门：终止运行 + 冻结待审批（结果同步投递通知中心）。
        message.success(
          `已回滚运行 #${run.id}：冻结 ${res.rejectedInvocations} 项待审批，通知 ${res.notifications} 人`
        );
        await Promise.all([fetchRuns(), fetchList()]);
      } catch (e) {
        message.error(`回滚失败：${(e as Error).message}`);
      } finally {
        setActingRunId(null);
      }
    },
    [fetchRuns, fetchList, message]
  );

  // 服务器筛选项：治理面列表（失败不阻塞审批，退化为手填标识）。
  useEffect(() => {
    let cancelled = false;
    mcpApi
      .listServers()
      .then(res => {
        if (!cancelled) setServers(res.items ?? []);
      })
      .catch(() => {
        if (!cancelled) setServersUnavailable(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // 治理跳转入口：无 mcp:admin 的用户不显示（后端路由亦有 RBAC 兜底）。
  const canGovernTools = hasPermission('mcp', 'admin');
  // 审批决策入口：POST /agent/tools/:id/approve 需要 ai:write（无权限时只读展示）。
  const canApproveTools = hasPermission('ai', 'write');

  const handleApprove = async (id: number) => {
    try {
      await aiApproveTool(id, { approve: true });
      message.success('已通过');
      fetchList();
    } catch (e) {
      message.error(`操作失败：${(e as Error).message}`);
    }
  };

  const handleRejectOk = async () => {
    if (rejectId == null) return;
    try {
      await aiApproveTool(rejectId, { approve: false, reason: rejectReason });
      message.success('已驳回');
      setRejectId(null);
      setRejectReason('');
      fetchList();
    } catch (e) {
      message.error(`操作失败：${(e as Error).message}`);
    }
  };

  const columns = [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 70,
    },
    {
      title: '工具',
      dataIndex: 'toolName',
      key: 'toolName',
      width: 200,
      // M1-06：MCP 工具展示投影名（可调用名），原始工具名作为次要信息保留可追溯性。
      render: (v: string, r: ToolApproval) => (
        <Space direction="vertical" size={0}>
          <Tag color={r.provider === 'mcp' ? 'geekblue' : 'default'} style={{ marginInlineEnd: 0 }}>
            {r.callableName || v}
          </Tag>
          {r.provider === 'mcp' && r.rawToolName ? (
            <Text type="secondary" style={{ fontSize: 10 }}>
              原始名 {r.rawToolName}
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: '来源',
      key: 'source',
      width: 140,
      render: (_: unknown, r: ToolApproval) => <ToolSourceTag record={r} />,
    },
    {
      title: '目标对象',
      key: 'target',
      width: 160,
      render: (_: unknown, r: ToolApproval) => {
        if (!r.targetType || !r.targetId) return <Text type="secondary">-</Text>;
        const url = targetUrl(r.targetType, r.targetId);
        return (
          <Space size={4} wrap>
            {url ? (
              <Typography.Link
                data-testid={`target-link-${r.id}`}
                style={{ fontSize: 12 }}
                onClick={() => navigate(url)}
              >
                {r.targetType}#{r.targetId}
              </Typography.Link>
            ) : (
              // 未核实路由的类型只展示标识（B1-09：不生成猜测链接）。
              <Text style={{ fontSize: 12 }}>
                {r.targetType}#{r.targetId}
              </Text>
            )}
            {r.supportRef ? (
              <Tooltip title="支撑信息引用（证据/来源）">
                <Tag color="purple" style={{ marginInlineEnd: 0 }}>
                  依据 {r.supportRef}
                </Tag>
              </Tooltip>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: '风险',
      dataIndex: 'risk',
      key: 'risk',
      width: 90,
      render: (v?: string) => (v ? <Tag color={riskColor(v)}>{v}</Tag> : <Text type="secondary">-</Text>),
    },
    {
      title: '确认时限',
      key: 'expiresAt',
      width: 150,
      render: (_: unknown, r: ToolApproval) => {
        // dry-run 标识与审批状态无关（预览记录通常是 auto），任何状态都要可见。
        const dryRunTag = r.dryRun ? (
          <Tooltip title="dry-run 预览：只生成预览记录，不写业务数据">
            <Tag color="purple" style={{ marginInlineEnd: 0 }}>
              预览
            </Tag>
          </Tooltip>
        ) : null;
        if (r.approvalState !== 'pending') return dryRunTag ?? <Text type="secondary">-</Text>;
        if (!r.expiresAt) return <Text type="secondary">未设置</Text>;
        const expired = isExpired(r);
        return (
          <Space size={4}>
            <Tag color={expired ? 'red' : 'blue'} icon={<Clock size={12} />} style={{ marginInlineEnd: 0 }}>
              {expired ? '已过期' : formatRemaining(r.expiresAt, nowTick)}
            </Tag>
            {dryRunTag}
          </Space>
        );
      },
    },
    {
      title: '参数',
      dataIndex: 'argsRedacted',
      key: 'argsRedacted',
      ellipsis: true,
      render: (v: string) => (
        <Text type="secondary" style={{ fontSize: 12 }}>
          {prettyArgs(v).slice(0, 120)}
          {prettyArgs(v).length > 120 ? '…' : ''}
        </Text>
      ),
    },
    {
      title: '权限校验',
      dataIndex: 'permissionCheck',
      key: 'permissionCheck',
      width: 120,
      render: (v: string, r: ToolApproval) => (
        <TooltipWrapper text={r.permissionReason}>
          <Tag color={permissionColor(v)}>{v || '-'}</Tag>
        </TooltipWrapper>
      ),
    },
    {
      title: '状态',
      dataIndex: 'approvalState',
      key: 'approvalState',
      width: 100,
      render: (v: string) => <Tag color={stateColor(v)}>{STATE_LABELS[v] ?? v}</Tag>,
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 170,
      render: (v: string) => new Date(v).toLocaleString('zh-CN', { hour12: false }),
    },
    {
      title: '操作',
      key: 'action',
      width: 150,
      render: (_: unknown, r: ToolApproval) =>
        r.approvalState === 'pending' && isExpired(r) ? (
          <Tooltip title="确认单已过期：不会被执行，也不会被补批准；请重新发起">
            <Space>
              <Button type="primary" size="small" icon={<CheckCircle2 size={14} />} disabled>
                通过
              </Button>
              <Button danger size="small" icon={<XCircle size={14} />} disabled>
                驳回
              </Button>
            </Space>
          </Tooltip>
        ) : r.approvalState === 'pending' ? (
          canApproveTools ? (
            <Space>
              <Button
                type="primary"
                size="small"
                icon={<CheckCircle2 size={14} />}
                onClick={() => handleApprove(r.id)}
              >
                通过
              </Button>
              <Button danger size="small" icon={<XCircle size={14} />} onClick={() => setRejectId(r.id)}>
                驳回
              </Button>
            </Space>
          ) : (
            <Text type="secondary">无审批权限</Text>
          )
        ) : (
          <Text type="secondary">已处理</Text>
        ),
    },
  ];

  return (
    <div className="space-y-6">
      <Space align="center" style={{ justifyContent: 'space-between', width: '100%' }}>
        <Space align="center">
          <ShieldAlert size={22} color="#1677ff" />
          <Title level={4} style={{ margin: 0 }}>
            AI 工具审批队列
          </Title>
        </Space>
        <Space>
          {runIdFilter ? (
            <Tag
              color="blue"
              closable
              data-testid="approval-run-filter"
              onClose={() => setRunIdFilter(null)}
            >
              运行 #{runIdFilter}
            </Tag>
          ) : null}
          <Select
            style={{ width: 150 }}
            value={provider}
            onChange={v => {
              setProvider(v);
              // 来源切回内置时清掉服务器条件（内置记录没有服务器维度）。
              if (v !== 'mcp') setServer('');
            }}
            options={PROVIDER_OPTIONS}
          />
          {serversUnavailable ? (
            <Input
              style={{ width: 160 }}
              placeholder="服务器标识"
              allowClear
              value={server}
              onChange={e => setServer(e.target.value.trim())}
            />
          ) : (
            <Select
              style={{ width: 160 }}
              value={server}
              onChange={v => {
                setServer(v);
                if (v) setProvider('mcp');
              }}
              allowClear
              showSearch
              placeholder="全部服务器"
              optionFilterProp="label"
              options={servers.map(s => ({ value: s.name, label: s.display_name ? `${s.name}（${s.display_name}）` : s.name }))}
            />
          )}
          <Select
            style={{ width: 140 }}
            value={state}
            onChange={(v) => setState(v)}
            options={Object.entries(STATE_LABELS).map(([k, label]) => ({ value: k, label }))}
          />
          <a onClick={fetchList}>
            <RefreshCw size={16} /> 刷新
          </a>
          {canGovernTools ? (
            <Button size="small" icon={<Settings2 size={14} />} onClick={() => navigate('/admin/mcp-servers')}>
              工具治理
            </Button>
          ) : null}
        </Space>
      </Space>

      {/* I2-01 Autopilot 监督面：运行列表 + 暂停/回滚（写动作闸门），与审批队列按运行联动。 */}
      <Card
        data-testid="autopilot-supervision-card"
        title={
          <Space>
            <Activity size={16} />
            <span>Autopilot 运行监督</span>
            <Text type="secondary" style={{ fontSize: 12 }}>
              只读任务与写动作闸门；回滚会冻结该运行全部待审批
            </Text>
          </Space>
        }
        extra={
          <Space>
            <Select
              data-testid="autopilot-status-filter"
              size="small"
              style={{ width: 130 }}
              value={runStatusFilter}
              onChange={setRunStatusFilter}
              options={[
                { value: '', label: '全部状态' },
                { value: 'running', label: '运行中' },
                { value: 'completed', label: '已完成' },
                { value: 'failed', label: '已中断' },
                { value: 'cancelled', label: '已取消' },
              ]}
            />
            <Button
              size="small"
              icon={<RefreshCw size={14} />}
              loading={runsLoading}
              onClick={() => void fetchRuns()}
              data-testid="autopilot-refresh"
            >
              刷新
            </Button>
            <Button
              size="small"
              type="link"
              icon={<Bell size={14} />}
              onClick={() => navigate('/notifications')}
              data-testid="autopilot-notifications"
            >
              通知中心
            </Button>
          </Space>
        }
      >
        <Table<AutopilotRunSummary>
          data-testid="autopilot-runs-table"
          rowKey="id"
          size="small"
          loading={runsLoading}
          dataSource={runs}
          pagination={false}
          locale={{ emptyText: <Empty description="窗口内没有符合条件的运行" /> }}
          columns={[
            {
              title: '运行',
              key: 'run',
              width: 150,
              render: (_: unknown, r: AutopilotRunSummary) => (
                <Space size={4}>
                  <Text code data-testid={`autopilot-run-${r.id}`}>
                    run#{r.id}
                  </Text>
                  <Typography.Link
                    data-testid={`autopilot-trace-${r.id}`}
                    style={{ fontSize: 12 }}
                    onClick={() => navigate(`/ai/observability?runId=${r.id}`)}
                  >
                    轨迹
                  </Typography.Link>
                </Space>
              ),
            },
            { title: '入口', dataIndex: 'entrypoint', key: 'entrypoint', width: 110 },
            {
              title: '状态',
              dataIndex: 'status',
              key: 'status',
              width: 100,
              render: (v: string) => <Tag color={runStatusColor(v)}>{v}</Tag>,
            },
            {
              title: '计数',
              key: 'counts',
              render: (_: unknown, r: AutopilotRunSummary) => (
                <Space size={6} wrap style={{ fontSize: 12 }}>
                  <Text type="secondary">步骤 {r.steps}</Text>
                  <Text type="secondary">工具 {r.toolCalls}</Text>
                  <Tag
                    color={r.pendingApprovals > 0 ? 'gold' : 'default'}
                    data-testid={`autopilot-pending-${r.id}`}
                  >
                    待审批 {r.pendingApprovals}
                  </Tag>
                  <Text type="secondary">产物 {r.artifacts}</Text>
                </Space>
              ),
            },
            {
              title: '开始时间',
              dataIndex: 'startedAt',
              key: 'startedAt',
              width: 170,
              render: (v: string) => new Date(v).toLocaleString('zh-CN', { hour12: false }),
            },
            {
              title: '操作',
              key: 'actions',
              width: 260,
              render: (_: unknown, r: AutopilotRunSummary) => (
                <Space size={4} wrap>
                  {r.status === 'running' && canApproveTools ? (
                    <Button
                      size="small"
                      icon={<PauseCircle size={14} />}
                      loading={actingRunId === r.id}
                      onClick={() => void handlePauseRun(r)}
                      data-testid={`autopilot-pause-${r.id}`}
                    >
                      暂停
                    </Button>
                  ) : null}
                  {canApproveTools ? (
                    <Button
                      size="small"
                      danger
                      icon={<RotateCcw size={14} />}
                      loading={actingRunId === r.id}
                      onClick={() => void handleRollbackRun(r)}
                      data-testid={`autopilot-rollback-${r.id}`}
                    >
                      回滚
                    </Button>
                  ) : null}
                  <Button
                    size="small"
                    type="link"
                    onClick={() => {
                      // 审批联动：只看该运行的待审批（清空来源筛选，避免组合出空集）。
                      setProvider('');
                      setServer('');
                      setState('pending');
                      setRunIdFilter(r.id);
                    }}
                    data-testid={`autopilot-approvals-${r.id}`}
                  >
                    查看待审批
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Card>
        <Table
          rowKey="id"
          size="middle"
          loading={loading}
          dataSource={items}
          columns={columns}
          pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
          expandable={{
            expandedRowRender: (r) => (
              <>
                {/* M1-06：审批详情完整展示来源三元组（与审计页共用同一展示件）。 */}
                <ToolInvocationDetail
                  record={r}
                  canGovernTools={canGovernTools}
                  onOpenGovernance={() => navigate('/admin/mcp-servers')}
                />
                {/* B1-09：dry-run 预览快照（仅预览记录；按需拉取一次详情，不轮询）。 */}
                {r.dryRun ? <DryRunSnapshot invocationId={r.id} /> : null}
              </>
            ),
          }}
          locale={{
            emptyText: (
              <Empty
                description={
                  provider || server
                    ? `当前筛选条件下没有待处理项（来源=${provider || '全部'}，服务器=${server || '全部'}）`
                    : '当前状态下列表为空'
                }
              />
            ),
          }}
        />
      </Card>

      <Modal
        title="驳回工具调用"
        open={rejectId != null}
        onOk={handleRejectOk}
        onCancel={() => {
          setRejectId(null);
          setRejectReason('');
        }}
        okText="确认驳回"
        okButtonProps={{ danger: true }}
      >
        <Input.TextArea
          rows={3}
          placeholder="驳回原因（可选）"
          value={rejectReason}
          onChange={(e) => setRejectReason(e.target.value)}
        />
      </Modal>
    </div>
  );
};

// 轻量 Tooltip 包装：避免 antd Tooltip 在 SSR/严格模式下对 children 的告警
const TooltipWrapper: React.FC<{ text?: string; children: React.ReactElement }> = ({ text, children }) => {
  if (!text) return children;
  return (
    <span title={text} style={{ cursor: 'help' }}>
      {children}
    </span>
  );
};

export default AIApprovalQueue;
