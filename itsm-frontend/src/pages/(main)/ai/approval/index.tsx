
import React, { useState, useEffect, useCallback } from 'react';
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
import { CheckCircle2, Settings2, XCircle, RefreshCw, ShieldAlert } from 'lucide-react';

import {
  aiGetToolApprovals,
  aiApproveTool,
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
import { usePermissions } from '@/lib/hooks/use-permissions';
import { useAuthStoreHydration } from '@/lib/store/auth-store';

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

  const fetchList = useCallback(async () => {
    setLoading(true);
    try {
      const res: ToolApprovalListResponse = await aiGetToolApprovals(state, { provider, server });
      setItems(res.items ?? []);
    } catch (e) {
      message.error(`加载审批队列失败：${(e as Error).message}`);
    } finally {
      setLoading(false);
    }
  }, [state, provider, server, message]);

  useEffect(() => {
    fetchList();
  }, [fetchList]);

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
      title: '风险',
      dataIndex: 'risk',
      key: 'risk',
      width: 90,
      render: (v?: string) => (v ? <Tag color={riskColor(v)}>{v}</Tag> : <Text type="secondary">-</Text>),
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
        r.approvalState === 'pending' ? (
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
              // M1-06：审批详情完整展示来源三元组（与审计页共用同一展示件）。
              <ToolInvocationDetail
                record={r}
                canGovernTools={canGovernTools}
                onOpenGovernance={() => navigate('/admin/mcp-servers')}
              />
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
