
import React, { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router';
import {
  Card,
  Table,
  Select,
  Space,
  Tag,
  Typography,
  Progress,
  Drawer,
  Descriptions,
  Empty,
  App,
  Input,
  Tooltip,
} from 'antd';
import { BarChart3, RefreshCw, ShieldCheck, Target } from 'lucide-react';

import {
  aiGetToolApprovals,
  aiGetAuditLogs,
  aiGetEvaluation,
  type AIAuditEntry,
  type AIEvaluationReport,
  type ToolApproval,
} from '@/lib/api/ai-api';
import { mcpApi, type MCPServer } from '@/lib/api/mcp-api';
import {
  ToolInvocationDetail,
  ToolSourceTag,
  riskColor,
} from '@/components/ai/tool-invocation-detail';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { useAuthStoreHydration } from '@/lib/store/auth-store';

const { Title, Text } = Typography;

const SCENARIO_LABELS: Record<string, string> = {
  triage: '智能分诊',
  summarize: '工单总结',
  analyze: '根因分析',
  rag_search: '知识检索',
  chat: 'AI 对话',
  sla_forecast: 'SLA 预测',
};

const scenarioLabel = (kind: string): string => SCENARIO_LABELS[kind] ?? kind;

const pct = (v: number): string => `${Math.round(v * 100)}%`;

const healthColor = (score: number): string => {
  if (score >= 80) return '#52c41a';
  if (score >= 60) return '#faad14';
  return '#ff4d4f';
};

const AIAuditConsole: React.FC = () => {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const { hasPermission } = usePermissions();
  useAuthStoreHydration();

  const [report, setReport] = useState<AIEvaluationReport | null>(null);
  const [evalLoading, setEvalLoading] = useState(false);
  const [logs, setLogs] = useState<AIAuditEntry[]>([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [kindFilter, setKindFilter] = useState<string>('');
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 });
  const [detail, setDetail] = useState<AIAuditEntry | null>(null);

  // M1-07：工具调用审计（来源维度）。数据真源是 tool_invocations（非 AI 场景审计日志），
  // `state=all` 表示跨审批状态全量；过滤一律由后端完成。
  const [toolRows, setToolRows] = useState<ToolApproval[]>([]);
  const [toolLoading, setToolLoading] = useState(false);
  const [toolProvider, setToolProvider] = useState<string>('');
  const [toolServer, setToolServer] = useState<string>('');
  const [toolState, setToolState] = useState<string>('all');
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [serversUnavailable, setServersUnavailable] = useState(false);
  const [toolFilters, setToolFilters] = useState({ provider: '', server: '', state: 'all' });

  const fetchToolRows = useCallback(
    async (provider: string, server: string, state: string) => {
      setToolLoading(true);
      try {
        const res = await aiGetToolApprovals(state, { provider, server });
        setToolRows(res.items ?? []);
        // 回显后端生效的过滤条件（筛选可分享、空态可解释）。
        setToolFilters({
          provider: res.provider ?? provider,
          server: res.server ?? server,
          state: res.state ?? state,
        });
      } catch (e) {
        message.error(`加载工具调用审计失败：${(e as Error).message}`);
      } finally {
        setToolLoading(false);
      }
    },
    [message]
  );

  useEffect(() => {
    void fetchToolRows('', '', 'all');
  }, [fetchToolRows]);

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

  const fetchEvaluation = useCallback(async () => {
    setEvalLoading(true);
    try {
      setReport(await aiGetEvaluation(30));
    } catch (e) {
      message.error(`加载 AI 评估失败：${(e as Error).message}`);
    } finally {
      setEvalLoading(false);
    }
  }, [message]);

  const fetchLogs = useCallback(
    async (page: number, pageSize: number, kind: string) => {
      setLogsLoading(true);
      try {
        const res = await aiGetAuditLogs({ page, pageSize, kind: kind || undefined, days: 90 });
        setLogs(res.items ?? []);
        setPagination({ current: res.page || page, pageSize: res.pageSize || pageSize, total: res.total ?? 0 });
      } catch (e) {
        message.error(`加载 AI 审计日志失败：${(e as Error).message}`);
      } finally {
        setLogsLoading(false);
      }
    },
    [message]
  );

  useEffect(() => {
    fetchEvaluation();
    fetchLogs(1, 20, '');
  }, [fetchEvaluation, fetchLogs]);

  const handleKindChange = (kind: string) => {
    setKindFilter(kind);
    fetchLogs(1, pagination.pageSize, kind);
  };

  const columns = [
    {
      title: '时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 170,
      render: (v: string) => new Date(v).toLocaleString('zh-CN', { hour12: false }),
    },
    {
      title: '场景',
      dataIndex: 'scenario',
      key: 'scenario',
      width: 110,
      render: (v: string) => <Tag color="geekblue">{scenarioLabel(v)}</Tag>,
    },
    {
      title: '输入引用',
      dataIndex: 'inputRef',
      key: 'inputRef',
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      width: 130,
      render: (v: string) => v || '-',
    },
    {
      title: '策略版本',
      dataIndex: 'promptVersion',
      key: 'promptVersion',
      width: 110,
      render: (v: string) => (v ? <Tag>{v}</Tag> : '-'),
    },
    {
      title: '可信度',
      dataIndex: 'confidence',
      key: 'confidence',
      width: 110,
      render: (v: number) => pct(v ?? 0),
    },
    {
      title: '采纳',
      dataIndex: 'accepted',
      key: 'accepted',
      width: 80,
      render: (v: boolean) =>
        v ? <Tag color="success">已采纳</Tag> : <Tag color="default">未采纳</Tag>,
    },
    {
      title: '操作',
      key: 'action',
      width: 80,
      render: (_: unknown, record: AIAuditEntry) => (
        <a onClick={() => setDetail(record)}>详情</a>
      ),
    },
  ];

  /** M1-07：工具调用审计列（与 tool_invocations 扩展字段一一对应）。 */
  const toolColumns = [
    {
      title: '时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 170,
      render: (v: string) => new Date(v).toLocaleString('zh-CN', { hour12: false }),
    },
    {
      title: '来源',
      key: 'source',
      width: 140,
      render: (_: unknown, r: ToolApproval) => <ToolSourceTag record={r} />,
    },
    {
      title: '工具',
      dataIndex: 'toolName',
      key: 'toolName',
      width: 210,
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
      title: '风险',
      dataIndex: 'risk',
      key: 'risk',
      width: 90,
      render: (v?: string) => (v ? <Tag color={riskColor(v)}>{v}</Tag> : <Text type="secondary">-</Text>),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <Tag>{v || '-'}</Tag>,
    },
    {
      title: '耗时',
      dataIndex: 'durationMs',
      key: 'durationMs',
      width: 90,
      render: (v?: number) => (typeof v === 'number' ? `${v}ms` : '-'),
    },
    {
      title: '错误码',
      dataIndex: 'errorCode',
      key: 'errorCode',
      width: 150,
      render: (v?: string) => (v ? <Text type="danger">{v}</Text> : '-'),
    },
  ];

  return (
    <div className="space-y-6">
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Space align="center" style={{ justifyContent: 'space-between', width: '100%' }}>
          <Space align="center">
            <ShieldCheck size={22} color="#1677ff" />
            <Title level={4} style={{ margin: 0 }}>
              AI 评估与审计控制台
            </Title>
          </Space>
          <Space>
            <Select
              style={{ width: 160 }}
              placeholder="全部场景"
              allowClear
              value={kindFilter || undefined}
              onChange={handleKindChange}
              options={Object.entries(SCENARIO_LABELS).map(([k, label]) => ({
                value: k,
                label,
              }))}
            />
            <a onClick={() => { fetchEvaluation(); fetchLogs(1, pagination.pageSize, kindFilter); }}>
              <RefreshCw size={16} /> 刷新
            </a>
          </Space>
        </Space>

        {/* 评估总览 */}
        <Card title="评估总览（近 30 天）" loading={evalLoading} extra={<BarChart3 size={16} />}>
          {report?.hasData ? (
            <Space size={32} wrap>
              <div style={{ textAlign: 'center' }}>
                <Progress
                  type="dashboard"
                  percent={Math.round(report.healthScore)}
                  strokeColor={healthColor(report.healthScore)}
                  format={(p) => `${p}`}
                />
                <div>
                  <Text strong>健康分</Text>
                </div>
              </div>
              <div>
                <Descriptions column={2} size="small" colon={false}>
                  <Descriptions.Item label="反馈样本">
                    <Text strong>{report.totalFeedback}</Text>
                  </Descriptions.Item>
                  <Descriptions.Item label="有用率">
                    <Text strong>{pct(report.usefulRate)}</Text>
                  </Descriptions.Item>
                  <Descriptions.Item label="建议采纳率">
                    <Text strong>{pct(report.acceptedRate)}</Text>
                  </Descriptions.Item>
                  <Descriptions.Item label="平均置信度">
                    <Text strong>{pct(report.avgConfidence)}</Text>
                  </Descriptions.Item>
                  <Descriptions.Item label="模型调用次数">
                    <Text strong>{report.platform.llmCallCount} 次</Text>
                  </Descriptions.Item>
                  <Descriptions.Item label="调用成功率">
                    <Text strong>{pct(report.platform.successRate)}</Text>
                  </Descriptions.Item>
                  <Descriptions.Item label="平均响应时间">
                    <Text strong>{Math.round(report.platform.avgLatencyMs)} ms</Text>
                  </Descriptions.Item>
                </Descriptions>
              </div>
            </Space>
          ) : (
            <Empty
              description={
                <span>
                  AI 功能使用后会自动产生评估数据，可在此查看健康分和采纳率。
                </span>
              }
            />
          )}
        </Card>

        {/* 按场景 + 置信度校准 */}
        {report?.hasData && (
          <div style={{ display: 'flex', gap: 16, alignItems: 'flex-start' }}>
            <Card title="按场景评估" style={{ flex: 1 }}>
              <Table
                size="small"
                rowKey="kind"
                dataSource={report.byScenario}
                pagination={false}
                columns={[
                  { title: '场景', dataIndex: 'kind', render: (v: string) => scenarioLabel(v) },
                  { title: '样本数', dataIndex: 'count', width: 80 },
                  {
                    title: '有用率',
                    dataIndex: 'usefulRate',
                    width: 120,
                    render: (v: number) => (
                      <Progress percent={Math.round(v * 100)} size="small" strokeColor={v >= 0.6 ? '#52c41a' : '#faad14'} />
                    ),
                  },
                  {
                    title: '采纳率',
                    dataIndex: 'acceptedRate',
                    width: 100,
                    render: (v: number) => (v > 0 ? pct(v) : '-'),
                  },
                  {
                    title: '平均置信度',
                    dataIndex: 'avgConfidence',
                    width: 100,
                    render: (v: number) => pct(v),
                  },
                ]}
              />
            </Card>
            <Card
              title={
                <Space>
                  <Target size={16} />
                  置信度校准（高置信 ≠ 高有用）
                </Space>
              }
              style={{ flex: 1 }}
            >
              <Table
                size="small"
                rowKey="bucket"
                dataSource={report.confidenceCalibration}
                pagination={false}
                columns={[
                  { title: '置信区间', dataIndex: 'bucket', width: 100 },
                  { title: '样本数', dataIndex: 'count', width: 70 },
                  {
                    title: '实际有用率',
                    dataIndex: 'usefulRate',
                    width: 130,
                    render: (v: number, r: { calibrationError: number; count: number }) =>
                      r.count > 0 ? (
                        <Progress
                          percent={Math.round(v * 100)}
                          size="small"
                          strokeColor={r.calibrationError <= 0.15 ? '#52c41a' : '#ff4d4f'}
                        />
                      ) : (
                        '-'
                      ),
                  },
                  {
                    title: '校准偏差',
                    dataIndex: 'calibrationError',
                    width: 90,
                    render: (v: number, r: { count: number }) =>
                      r.count > 0 ? (
                        <Tooltip title="|实际有用率 - 置信度中点|，越小代表置信度越可信">
                          <Tag color={v <= 0.15 ? 'green' : v <= 0.3 ? 'orange' : 'red'}>
                            {Math.round(v * 100)}%
                          </Tag>
                        </Tooltip>
                      ) : (
                        '-'
                      ),
                  },
                ]}
              />
            </Card>
          </div>
        )}

        {/* 审计日志 */}
        <Card title={`AI 审计日志（近 90 天，共 ${pagination.total} 条）`}>
          <Table
            rowKey="id"
            size="middle"
            loading={logsLoading}
            dataSource={logs}
            columns={columns}
            pagination={{
              current: pagination.current,
              pageSize: pagination.pageSize,
              total: pagination.total,
              showSizeChanger: true,
              showTotal: (t) => `共 ${t} 条`,
              onChange: (page, pageSize) => fetchLogs(page, pageSize, kindFilter),
            }}
            locale={{ emptyText: '暂无 AI 审计记录' }}
          />
        </Card>

        {/* M1-07：工具调用审计（来源维度）——数据真源 tool_invocations，脱敏字段原样展示。 */}
        <Card
          title="工具调用审计（来源维度）"
          extra={
            <Space>
              <Select
                style={{ width: 140 }}
                value={toolProvider}
                onChange={v => {
                  setToolProvider(v);
                  const nextServer = v === 'mcp' ? toolServer : '';
                  setToolServer(nextServer);
                  void fetchToolRows(v, nextServer, toolState);
                }}
                options={[
                  { value: '', label: '全部来源' },
                  { value: 'builtin', label: '内置工具' },
                  { value: 'mcp', label: 'MCP 外部工具' },
                ]}
              />
              {serversUnavailable ? (
                <Input
                  style={{ width: 150 }}
                  placeholder="服务器标识"
                  allowClear
                  onPressEnter={e => {
                    const v = (e.target as HTMLInputElement).value.trim();
                    setToolServer(v);
                    if (v) setToolProvider('mcp');
                    void fetchToolRows(v ? 'mcp' : toolProvider, v, toolState);
                  }}
                />
              ) : (
                <Select
                  style={{ width: 150 }}
                  placeholder="全部服务器"
                  allowClear
                  showSearch
                  optionFilterProp="label"
                  value={toolServer || undefined}
                  onChange={v => {
                    const nextServer = v ?? '';
                    setToolServer(nextServer);
                    if (nextServer) setToolProvider('mcp');
                    void fetchToolRows(nextServer ? 'mcp' : toolProvider, nextServer, toolState);
                  }}
                  options={servers.map(s => ({
                    value: s.name,
                    label: s.display_name ? `${s.name}（${s.display_name}）` : s.name,
                  }))}
                />
              )}
              <Select
                style={{ width: 130 }}
                value={toolState}
                onChange={v => {
                  setToolState(v);
                  void fetchToolRows(toolProvider, toolServer, v);
                }}
                options={[
                  { value: 'all', label: '全部状态' },
                  { value: 'pending', label: '待审批' },
                  { value: 'approved', label: '已通过' },
                  { value: 'rejected', label: '已驳回' },
                  { value: 'auto', label: '自动执行' },
                ]}
              />
              <a onClick={() => void fetchToolRows(toolProvider, toolServer, toolState)}>
                <RefreshCw size={16} /> 刷新
              </a>
            </Space>
          }
        >
          <Table
            rowKey="id"
            size="middle"
            loading={toolLoading}
            dataSource={toolRows}
            columns={toolColumns}
            pagination={{ pageSize: 20, showTotal: t => `共 ${t} 条` }}
            expandable={{
              expandedRowRender: (r: ToolApproval) => (
                <ToolInvocationDetail
                  record={r}
                  canGovernTools={hasPermission('mcp', 'admin')}
                  onOpenGovernance={() => navigate('/admin/mcp-servers')}
                />
              ),
            }}
            locale={{
              emptyText: (
                <Empty
                  description={
                    toolFilters.provider || toolFilters.server || toolFilters.state !== 'all'
                      ? `当前筛选条件下没有工具调用记录（来源=${toolFilters.provider || '全部'}，服务器=${
                          toolFilters.server || '全部'
                        }，状态=${toolFilters.state}）`
                      : '暂无工具调用记录'
                  }
                />
              ),
            }}
          />
        </Card>
      </Space>

      {/* 审计详情 */}
      <Drawer
        title={`AI 审计详情 · ${detail ? scenarioLabel(detail.scenario) : ''}`}
        open={!!detail}
        onClose={() => setDetail(null)}
        width={520}
      >
        {detail && (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="请求 ID">{detail.requestId}</Descriptions.Item>
            <Descriptions.Item label="时间">
              {new Date(detail.createdAt).toLocaleString('zh-CN', { hour12: false })}
            </Descriptions.Item>
            <Descriptions.Item label="场景">{scenarioLabel(detail.scenario)}</Descriptions.Item>
            <Descriptions.Item label="输入引用">{detail.inputRef || '-'}</Descriptions.Item>
            <Descriptions.Item label="模型">{detail.model || '-'}</Descriptions.Item>
            <Descriptions.Item label="策略版本">{detail.promptVersion || '-'}</Descriptions.Item>
            <Descriptions.Item label="可信度">{pct(detail.confidence)}</Descriptions.Item>
            <Descriptions.Item label="采纳结果">
              {detail.accepted ? <Tag color="success">已采纳</Tag> : <Tag>未采纳</Tag>}
            </Descriptions.Item>
            <Descriptions.Item label="建议内容">
              <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: 12 }}>
                {detail.suggestion ? JSON.stringify(detail.suggestion, null, 2) : '-'}
              </pre>
            </Descriptions.Item>
            <Descriptions.Item label="原始备注">
              <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: 12 }}>
                {detail.notes || '-'}
              </pre>
            </Descriptions.Item>
          </Descriptions>
        )}
      </Drawer>
    </div>
  );
};

export default AIAuditConsole;
