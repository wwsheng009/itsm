import { useState } from 'react';
import { Alert, Button, Card, Empty, Input, InputNumber, Select, Space, Table, Tag, Tooltip, Typography, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Copy, RefreshCw } from 'lucide-react';
import { projectService } from '@/lib/services/project-service';
import type {
  AIGeneratedBy,
  AIRiskSignal,
  AIRiskWarning,
  AIRiskWarningsResponse,
  AISeverity,
  AISummaryResponse,
  AIReportCadence,
  AITaskProposal,
  TaskPriority,
} from '@/lib/services/project-service';

const CADENCE_OPTIONS: { value: AIReportCadence; label: string }[] = [
  { value: 'weekly', label: '周报' },
  { value: 'biweekly', label: '双周报' },
  { value: 'monthly', label: '月报' },
];

const SEVERITY_META: Record<AISeverity, { label: string; color: string }> = {
  info: { label: '提示', color: 'default' },
  warning: { label: '警告', color: 'warning' },
  critical: { label: '严重', color: 'error' },
};

const SIGNAL_TYPE_LABEL: Record<string, string> = {
  overdue_project: '项目逾期',
  overdue_task: '任务逾期',
  blocked_task: '任务阻塞',
  overdue_milestone: '里程碑逾期',
  high_risk: '高风险未关闭',
  gate_rejected: '阶段门被拒',
};

const GENERATED_BY_META: Record<AIGeneratedBy, { label: string; color: string }> = {
  ai: { label: 'AI', color: 'green' },
  deterministic: { label: '事实派生', color: 'default' },
  rules: { label: '规则信号', color: 'blue' },
  none: { label: '无结果', color: 'default' },
};

const DEGRADE_HINT: Record<string, string> = {
  ai_unavailable: 'AI 服务未配置，已降级为确定性输出（不影响主流程）。',
  llm_error: 'AI 调用失败，已降级为确定性输出；可稍后重试。',
  parse_error: 'AI 输出无法解析，已降级为确定性输出；可稍后重试。',
};

const MAX_MINUTES_CHARS = 20000;

interface ProjectAITabProps {
  projectId: number;
  /** project:write（创建选中任务需要；生成摘要/预警只需 project:read）。 */
  canWrite: boolean;
  /** 任务创建成功后回调（父级刷新任务列表）。 */
  onTasksCreated?: () => void;
}

/**
 * 项目 AI 助手面板（P2/PE-25）：
 * ① 状态摘要（周报/双周报/月报；AI 不可用时降级为事实派生）；
 * ② 风险预警（规则信号恒返回 + AI 判断增强段）；
 * ③ 纪要转任务（只出建议不落库；勾选后走既有建任务端点逐条创建）。
 */
export function ProjectAITab({ projectId, canWrite, onTasksCreated }: ProjectAITabProps) {
  // ① 状态摘要
  const [cadence, setCadence] = useState<AIReportCadence>('weekly');
  const [summaryLoading, setSummaryLoading] = useState(false);
  const [summary, setSummary] = useState<AISummaryResponse | null>(null);

  // ② 风险预警
  const [riskLoading, setRiskLoading] = useState(false);
  const [risk, setRisk] = useState<AIRiskWarningsResponse | null>(null);

  // ③ 纪要转任务
  const [minutes, setMinutes] = useState('');
  const [proposalLimit, setProposalLimit] = useState(10);
  const [minutesLoading, setMinutesLoading] = useState(false);
  const [minutesResult, setMinutesResult] = useState<{ proposals: AITaskProposal[]; degraded: boolean; degradeReason?: string; model?: string } | null>(null);
  const [selectedKeys, setSelectedKeys] = useState<number[]>([]);
  const [creating, setCreating] = useState(false);

  const generateSummary = async () => {
    setSummaryLoading(true);
    try {
      setSummary(await projectService.aiSummary(projectId, cadence));
    } catch (error) {
      console.error('Failed to generate AI summary:', error);
      message.error('生成摘要失败');
    } finally {
      setSummaryLoading(false);
    }
  };

  const generateRiskWarnings = async () => {
    setRiskLoading(true);
    try {
      setRisk(await projectService.aiRiskWarnings(projectId));
    } catch (error) {
      console.error('Failed to generate AI risk warnings:', error);
      message.error('生成风险预警失败');
    } finally {
      setRiskLoading(false);
    }
  };

  const extractProposals = async () => {
    const content = minutes.trim();
    if (!content) {
      message.warning('请先粘贴会议纪要内容');
      return;
    }
    if (content.length > MAX_MINUTES_CHARS) {
      message.warning(`纪要内容不能超过 ${MAX_MINUTES_CHARS} 字符`);
      return;
    }
    setMinutesLoading(true);
    try {
      const result = await projectService.aiMinutesTasks(projectId, content, proposalLimit);
      setMinutesResult(result);
      setSelectedKeys(result.proposals.map((_, index) => index));
    } catch (error) {
      console.error('Failed to extract tasks from minutes:', error);
      message.error('纪要解析失败');
    } finally {
      setMinutesLoading(false);
    }
  };

  const createSelectedTasks = async () => {
    const proposals = minutesResult?.proposals ?? [];
    const chosen = proposals.filter((_, index) => selectedKeys.includes(index));
    if (chosen.length === 0) {
      message.warning('请先勾选要创建的任务建议');
      return;
    }
    setCreating(true);
    let created = 0;
    let failed = 0;
    for (const proposal of chosen) {
      try {
        await projectService.createTask(projectId, {
          name: proposal.name,
          ...(proposal.description ? { description: proposal.description } : {}),
          ...(proposal.priority ? { priority: proposal.priority as TaskPriority } : {}),
          ...(proposal.estimateHours ? { estimateHours: proposal.estimateHours } : {}),
        });
        created += 1;
      } catch (error) {
        failed += 1;
        console.error('Failed to create task from proposal:', proposal.name, error);
      }
    }
    setCreating(false);
    if (created > 0) {
      message.success(failed > 0 ? `已创建 ${created} 个任务，${failed} 个失败` : `已创建 ${created} 个任务`);
      setSelectedKeys([]);
      onTasksCreated?.();
    } else {
      message.error('任务创建失败');
    }
  };

  const copySummary = async () => {
    if (!summary?.summary) {
      return;
    }
    try {
      await navigator.clipboard.writeText(summary.summary);
      message.success('已复制摘要');
    } catch {
      message.warning('复制失败，请手动选择文本复制');
    }
  };

  const signalColumns: ColumnsType<AIRiskSignal> = [
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      width: 130,
      render: (value: string) => <Tag color="blue">{SIGNAL_TYPE_LABEL[value] ?? value}</Tag>,
    },
    {
      title: '严重度',
      dataIndex: 'severity',
      key: 'severity',
      width: 90,
      render: (value: AISeverity) => <Tag color={SEVERITY_META[value]?.color}>{SEVERITY_META[value]?.label ?? value}</Tag>,
    },
    { title: '标题', dataIndex: 'title', key: 'title', width: 260 },
    {
      title: '明细',
      dataIndex: 'detail',
      key: 'detail',
      render: (value: string, row) =>
        row.refType && row.refId ? (
          <Space size={4}>
            <Tag>{`${row.refType}#${row.refId}`}</Tag>
            <span>{value}</span>
          </Space>
        ) : (
          value
        ),
    },
  ];

  const warningColumns: ColumnsType<AIRiskWarning> = [
    {
      title: '严重度',
      dataIndex: 'severity',
      key: 'severity',
      width: 90,
      render: (value: AISeverity) => <Tag color={SEVERITY_META[value]?.color}>{SEVERITY_META[value]?.label ?? value}</Tag>,
    },
    { title: '标题', dataIndex: 'title', key: 'title', width: 260 },
    { title: '说明', dataIndex: 'detail', key: 'detail' },
    { title: '建议', dataIndex: 'suggestion', key: 'suggestion' },
  ];

  const proposalColumns: ColumnsType<AITaskProposal> = [
    { title: '任务名称', dataIndex: 'name', key: 'name', width: 240 },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 90,
      render: (value?: string) => (value ? <Tag color="orange">{value}</Tag> : '—'),
    },
    {
      title: '估算工时',
      dataIndex: 'estimateHours',
      key: 'estimateHours',
      width: 100,
      render: (value?: number) => (value ? `${value}h` : '—'),
    },
    {
      title: '提及负责人',
      dataIndex: 'ownerHint',
      key: 'ownerHint',
      width: 130,
      render: (value?: string) => value || '—',
    },
    { title: '说明', dataIndex: 'description', key: 'description', render: (value?: string) => value || '—' },
  ];

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        size="small"
        title="状态摘要"
        extra={
          <Space>
            <Select
              style={{ width: 110 }}
              value={cadence}
              options={CADENCE_OPTIONS}
              onChange={(value) => setCadence(value)}
            />
            <Button type="primary" loading={summaryLoading} onClick={() => void generateSummary()}>
              生成摘要
            </Button>
          </Space>
        }
      >
        {summary ? (
          <>
            <Space size={8} style={{ marginBottom: 8 }}>
              <Tag color={GENERATED_BY_META[summary.generatedBy]?.color}>
                {GENERATED_BY_META[summary.generatedBy]?.label ?? summary.generatedBy}
              </Tag>
              {summary.model ? <Tag>模型：{summary.model}</Tag> : null}
              <Tooltip title="复制摘要">
                <Button size="small" icon={<Copy size={14} />} onClick={() => void copySummary()} />
              </Tooltip>
            </Space>
            {summary.degraded ? (
              <Alert
                type="info"
                showIcon
                style={{ marginBottom: 8 }}
                message="已降级为事实派生摘要"
                description={DEGRADE_HINT[summary.degradeReason ?? ''] ?? 'AI 输出不可用，已改用事实数据派生（不影响主流程）。'}
              />
            ) : null}
            <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', marginBottom: 0 }}>{summary.summary}</Typography.Paragraph>
          </>
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未生成；点击「生成摘要」按周报/双周报/月报口径生成" />
        )}
      </Card>

      <Card
        size="small"
        title="风险预警"
        extra={
          <Button icon={<RefreshCw size={14} />} loading={riskLoading} onClick={() => void generateRiskWarnings()}>
            生成预警
          </Button>
        }
      >
        {risk ? (
          <>
            <Space size={8} style={{ marginBottom: 8 }}>
              <Tag color={GENERATED_BY_META[risk.generatedBy]?.color}>
                {GENERATED_BY_META[risk.generatedBy]?.label ?? risk.generatedBy}
              </Tag>
              {risk.model ? <Tag>模型：{risk.model}</Tag> : null}
              <Tag>{`规则信号 ${risk.signalTotal} 条`}</Tag>
            </Space>
            {risk.degraded ? (
              <Alert
                type="info"
                showIcon
                style={{ marginBottom: 8 }}
                message="AI 判断不可用，以下为规则信号（不影响主流程）"
                description={DEGRADE_HINT[risk.degradeReason ?? ''] ?? undefined}
              />
            ) : null}
            {risk.truncated ? (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 8 }}
                message={`仅展示每类前 5 条明细，共 ${risk.signalTotal} 条信号（未静默省略）`}
              />
            ) : null}
            <Table
              rowKey={(row) => `${row.type}-${row.refType ?? ''}-${row.refId ?? 0}-${row.title}`}
              columns={signalColumns}
              dataSource={risk.signals}
              pagination={false}
              size="small"
              locale={{ emptyText: '暂无逾期/阻塞/高风险信号' }}
            />
            {risk.warnings.length > 0 ? (
              <Table
                style={{ marginTop: 12 }}
                rowKey={(row) => `${row.severity}-${row.title}`}
                columns={warningColumns}
                dataSource={risk.warnings}
                pagination={false}
                size="small"
                title={() => 'AI 判断'}
              />
            ) : null}
          </>
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未生成；规则信号始终可用，AI 判断为增强段" />
        )}
      </Card>

      <Card size="small" title="纪要转任务（只出建议，不自动创建）">
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Input.TextArea
            rows={6}
            value={minutes}
            maxLength={MAX_MINUTES_CHARS}
            showCount
            placeholder="粘贴会议纪要正文（≤20000 字符）；AI 从中抽取可执行任务建议"
            onChange={(event) => setMinutes(event.target.value)}
          />
          <Space>
            <span>建议条数</span>
            <InputNumber min={2} max={20} value={proposalLimit} onChange={(value) => setProposalLimit(value ?? 10)} />
            <Button type="primary" loading={minutesLoading} onClick={() => void extractProposals()}>
              生成建议
            </Button>
            <Button loading={creating} disabled={!canWrite || !minutesResult?.proposals.length} onClick={() => void createSelectedTasks()}>
              创建选中任务
            </Button>
          </Space>
          {!canWrite ? <Alert type="info" showIcon message="当前账号无 project:write 权限，只能查看建议、不能直接创建任务" /> : null}
          {minutesResult ? (
            <>
              {minutesResult.degraded ? (
                <Alert
                  type="warning"
                  showIcon
                  message="AI 不可用，未能抽取任务建议"
                  description={`${DEGRADE_HINT[minutesResult.degradeReason ?? ''] ?? ''}可手动创建任务，主流程不受影响。`}
                />
              ) : null}
              <Table
                rowKey={(_, index) => String(index)}
                columns={proposalColumns}
                dataSource={minutesResult.proposals}
                pagination={false}
                size="small"
                locale={{ emptyText: '未抽取到任务建议（可稍后重试或手动创建）' }}
                rowSelection={{
                  selectedRowKeys: selectedKeys,
                  onChange: (keys) => setSelectedKeys(keys as number[]),
                }}
              />
            </>
          ) : null}
        </Space>
      </Card>
    </Space>
  );
}