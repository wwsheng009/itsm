import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Col, DatePicker, Input, Row, Select, Space, Table, Tabs, Tag, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { RefreshCw } from 'lucide-react';
import { departmentService } from '@/lib/services/department-service';
import type { Department } from '@/lib/services/department-service';
import { projectService } from '@/lib/services/project-service';
import { formatBusinessDate, formatBusinessDateTime } from '@/lib/formatters';
import { ProjectHealthTag } from './ProjectHealthTag';
import type {
  DeliveryDetail,
  GateDetail,
  MetricsQuery,
  MetricsResponse,
  MilestoneDetail,
  PortfolioBucket,
  PortfolioResponse,
  Project,
  ResourceProjectRow,
} from '@/lib/services/project-service';

/**
 * 项目组合视图与度量（P2/PE-24）：
 * 组合聚合（状态/健康度/负责人/部门/类型）+ 四项度量（按时交付率/里程碑达成率/阶段门一次通过率/资源利用率）。
 * 口径：实时派生、与列表同谓词/同可见性；资源利用率复用 PE-23 热图口径（planned/capacity，按项目）。
 * 权威设计：docs/project-management/plan/p2-portfolio-metrics-design.md（D-40~D-43）。
 */

/** 比率展示：null → 「—」（分母为 0，不臆造 0%）。 */
export function formatRate(rate: number | null | undefined): string {
  if (rate === null || rate === undefined) return '—';
  return `${(rate * 100).toFixed(1)}%`;
}

/** 负责人桶标签（name 由后端 users 富化）。 */
export function managerBucketLabel(bucket: PortfolioBucket): string {
  if (bucket.name) return bucket.name;
  if (bucket.key === '0') return '未设置';
  return `用户#${bucket.key}`;
}

/** 部门桶标签（后端不跨域查询；由前端部门树映射，缺失降级为 ID）。 */
export function departmentBucketLabel(bucket: PortfolioBucket, names: Map<number, string>): string {
  if (bucket.key === '0') return '未设置';
  return names.get(Number(bucket.key)) ?? `部门#${bucket.key}`;
}

/** 部门树 → id→名称映射（含子部门；用于部门桶展示）。 */
export function flattenDepartmentNames(tree: Department[]): Map<number, string> {
  const out = new Map<number, string>();
  const walk = (nodes: Department[]) => {
    nodes.forEach((node) => {
      out.set(node.id, node.name);
      if (node.children && node.children.length > 0) walk(node.children);
    });
  };
  walk(tree);
  return out;
}

const STATUS_LABEL: Record<string, string> = {
  planning: '规划中',
  active: '进行中',
  suspended: '已暂停',
  completed: '已完成',
  cancelled: '已取消',
};

const HEALTH_LABEL: Record<string, { label: string; color: string }> = {
  green: { label: '健康', color: 'success' },
  yellow: { label: '关注', color: 'warning' },
  red: { label: '风险', color: 'error' },
};

const TYPE_LABEL: Record<string, string> = {
  internal: 'IT 内部项目',
  improvement: '服务改进',
  delivery: '交付项目',
};

const GATE_LABEL: Record<string, string> = {
  initiation: '立项门',
  plan: '计划门',
  acceptance: '验收门',
  closure: '结项门',
};

const MILESTONE_STATUS_META: Record<string, { label: string; color: string }> = {
  planned: { label: '计划中', color: 'default' },
  reached: { label: '已达成', color: 'success' },
  missed: { label: '未达成', color: 'error' },
};

/** 度量卡片（比率 + 分子/分母 + 备注）。 */
function MetricCard(props: { title: string; rate: number | null; detail: string; extra?: string }) {
  return (
    <Card size="small" data-testid={`metric-${props.title}`}>
      <div style={{ fontSize: 12, color: '#8c8c8c' }}>{props.title}</div>
      <div style={{ fontSize: 28, fontWeight: 600, lineHeight: 1.3 }}>{formatRate(props.rate)}</div>
      <div style={{ fontSize: 12, color: '#595959' }}>{props.detail}</div>
      {props.extra && <div style={{ fontSize: 12, color: '#bfbfbf' }}>{props.extra}</div>}
    </Card>
  );
}

/** 分布表（维度桶：名称/数量）。 */
function BucketTable(props: {
  title: string;
  buckets: PortfolioBucket[];
  label: (bucket: PortfolioBucket) => string;
  testId: string;
}) {
  const columns: ColumnsType<PortfolioBucket> = [
    { title: props.title, key: 'label', render: (_, record) => props.label(record) },
    { title: '项目数', dataIndex: 'count', key: 'count', width: 90, align: 'right' },
  ];
  return (
    <Table
      rowKey={(record) => `${props.testId}-${record.key}`}
      columns={columns}
      dataSource={props.buckets}
      pagination={false}
      size="small"
      data-testid={props.testId}
    />
  );
}

export default function ProjectPortfolioMetrics() {
  const [loading, setLoading] = useState(false);
  const [portfolio, setPortfolio] = useState<PortfolioResponse | null>(null);
  const [metrics, setMetrics] = useState<MetricsResponse | null>(null);
  const [deptNames, setDeptNames] = useState<Map<number, string>>(new Map());
  const [status, setStatus] = useState<string>('');
  const [type, setType] = useState<string>('');
  const [keyword, setKeyword] = useState<string>('');
  const [range, setRange] = useState<[string, string]>(['', '']);
  const [weeks, setWeeks] = useState<number>(6);

  const fetchAll = useCallback(async () => {
    setLoading(true);
    const base: MetricsQuery = {
      status: status || undefined,
      type: type || undefined,
      q: keyword || undefined,
      from: range[0] || undefined,
      to: range[1] || undefined,
      weeks,
    };
    try {
      const [portfolioData, metricsData] = await Promise.all([
        projectService.portfolio({
          status: status || undefined,
          type: type || undefined,
          q: keyword || undefined,
        }),
        projectService.metrics(base),
      ]);
      setPortfolio(portfolioData);
      setMetrics(metricsData);
    } catch (error) {
      console.error('Failed to fetch portfolio metrics:', error);
      message.error('组合视图加载失败（请确认已有 project:read 权限）');
    } finally {
      setLoading(false);
    }
  }, [status, type, keyword, range, weeks]);

  useEffect(() => {
    void fetchAll();
  }, [fetchAll]);

  useEffect(() => {
    departmentService
      .getDepartmentTree()
      .then((tree) => setDeptNames(flattenDepartmentNames(tree)))
      .catch(() => setDeptNames(new Map()));
  }, []);

  const delivery = metrics?.onTimeDelivery;
  const milestone = metrics?.milestoneAchievement;
  const gate = metrics?.gateFirstPass;
  const resource = metrics?.resourceUtilization;

  const projectColumns: ColumnsType<Project> = [
    { title: '编号', dataIndex: 'code', key: 'code', width: 160 },
    { title: '项目名称', dataIndex: 'name', key: 'name', width: 240 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (value: string) => STATUS_LABEL[value] ?? value,
    },
    {
      title: '健康度',
      dataIndex: 'health',
      key: 'health',
      width: 100,
      render: (_: string, record: Project) => (
        <ProjectHealthTag
          health={record.health}
          reasons={record.healthReasons}
          dataQuality={record.healthDataQuality}
          dataGaps={record.healthDataGaps}
        />
      ),
    },
    { title: '类型', dataIndex: 'type', key: 'type', width: 120, render: (v: string) => TYPE_LABEL[v] ?? v },
    { title: '进度', dataIndex: 'progress', key: 'progress', width: 90, render: (v: number) => `${v}%` },
    // B3：负责人姓名由后端富化（缺失回退用户 #ID）。
    {
      title: '项目经理',
      key: 'manager',
      width: 130,
      render: (_: unknown, r: Project) => r.managerName || (r.managerId ? `用户 #${r.managerId}` : '未设置'),
    },
    {
      title: '所属部门',
      key: 'department',
      width: 140,
      render: (_: unknown, r: Project) =>
        r.departmentName || (r.departmentId ? `部门 #${r.departmentId}` : '未设置'),
    },
    // A6：日期按本地时区格式化，避免 UTC 截断导致的跨天偏移。
    { title: '计划结束', dataIndex: 'endDate', key: 'endDate', width: 120, render: (v?: string) => formatBusinessDate(v) || '—' },
  ];

  const deliveryColumns: ColumnsType<DeliveryDetail> = [
    { title: '项目', key: 'project', render: (_, r) => `${r.code ? `${r.code} ` : ''}${r.name}` },
    { title: '计划结束', dataIndex: 'planEnd', key: 'planEnd', width: 120, render: (v: string) => formatBusinessDate(v) || '—' },
    { title: '实际结束', dataIndex: 'actualEnd', key: 'actualEnd', width: 120, render: (v: string) => formatBusinessDate(v) || '—' },
    {
      title: '是否按期',
      dataIndex: 'onTime',
      key: 'onTime',
      width: 100,
      render: (v: boolean) => (v ? <Tag color="success">按期</Tag> : <Tag color="error">逾期</Tag>),
    },
  ];

  const milestoneColumns: ColumnsType<MilestoneDetail> = [
    { title: '项目', dataIndex: 'projectId', key: 'projectId', width: 90, render: (v: number) => `#${v}` },
    { title: '里程碑', dataIndex: 'name', key: 'name' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => {
        const meta = MILESTONE_STATUS_META[v];
        return meta ? <Tag color={meta.color}>{meta.label}</Tag> : v;
      },
    },
    { title: '目标日期', dataIndex: 'dueDate', key: 'dueDate', width: 120, render: (v: string) => formatBusinessDate(v) || '—' },
    {
      title: '达成时间',
      dataIndex: 'reachedAt',
      key: 'reachedAt',
      width: 120,
      render: (v?: string) => formatBusinessDate(v) || '—',
    },
    { title: '达成', dataIndex: 'achieved', key: 'achieved', width: 80, render: (v: boolean) => (v ? '✓' : '✗') },
    { title: '按期', dataIndex: 'onTime', key: 'onTime', width: 80, render: (v: boolean) => (v ? '✓' : '—') },
  ];

  const gateColumns: ColumnsType<GateDetail> = [
    { title: '项目', dataIndex: 'projectId', key: 'projectId', width: 90, render: (v: number) => `#${v}` },
    { title: '阶段门', dataIndex: 'gate', key: 'gate', width: 110, render: (v: string) => GATE_LABEL[v] ?? v },
    { title: '打回次数', dataIndex: 'rejectCount', key: 'rejectCount', width: 100, align: 'right' },
    {
      title: '一次通过',
      dataIndex: 'firstPass',
      key: 'firstPass',
      width: 100,
      // B12：自动放行不计入一次通过率，明细里显式标注，避免误读为「审批失败」。
      render: (v: boolean, row) =>
        row.autoApproved ? <Tag color="default">自动放行（不计入）</Tag> : v ? <Tag color="success">是</Tag> : <Tag color="warning">否</Tag>,
    },
    {
      title: '裁决时间',
      dataIndex: 'decidedAt',
      key: 'decidedAt',
      width: 160,
      render: (v?: string) => formatBusinessDateTime(v) || '—',
    },
  ];

  const resourceColumns: ColumnsType<ResourceProjectRow> = [
    { title: '项目', key: 'project', render: (_, r) => `${r.code ? `${r.code} ` : ''}${r.name}` },
    { title: '计划工时', dataIndex: 'plannedHours', key: 'plannedHours', width: 110, align: 'right' },
    { title: '容量', dataIndex: 'capacityHours', key: 'capacityHours', width: 110, align: 'right' },
    {
      title: '利用率',
      dataIndex: 'rate',
      key: 'rate',
      width: 110,
      align: 'right',
      render: (v: number | null) => formatRate(v),
    },
  ];

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle" data-testid="project-portfolio">
      <Alert
        type="info"
        showIcon
        message="口径说明"
        description={
          <span>
            度量由执行数据实时派生（无手工填报），与项目列表同过滤/同可见性；资源利用率按<b>项目</b>口径汇总
            （同一成员参与多个项目会重复计容量，跨项目成员级利用率需按人按日工时），
            {resource?.calendar?.note ? `且 ${resource.calendar.note}。` : '周容量为整周口径。'}
          </span>
        }
      />

      <Card
        size="small"
        title="过滤"
        extra={
          <Button icon={<RefreshCw size={16} />} loading={loading} onClick={fetchAll}>
            刷新
          </Button>
        }
      >
        <Space wrap>
          <Select
            allowClear
            placeholder="状态"
            style={{ width: 130 }}
            value={status || undefined}
            onChange={(v) => setStatus(v ?? '')}
            options={Object.entries(STATUS_LABEL).map(([value, label]) => ({ value, label }))}
          />
          <Select
            allowClear
            placeholder="类型"
            style={{ width: 150 }}
            value={type || undefined}
            onChange={(v) => setType(v ?? '')}
            options={Object.entries(TYPE_LABEL).map(([value, label]) => ({ value, label }))}
          />
          <Input.Search
            allowClear
            placeholder="编号/名称关键字"
            style={{ width: 220 }}
            onSearch={(v) => setKeyword(v.trim())}
          />
          <DatePicker.RangePicker
            onChange={(_dates, dateStrings) =>
              setRange([dateStrings?.[0] ?? '', dateStrings?.[1] ?? ''])
            }
            placeholder={['完成日期从', '完成日期到']}
          />
          <Select
            style={{ width: 150 }}
            value={weeks}
            onChange={(v) => setWeeks(v)}
            options={[4, 6, 8, 12].map((v) => ({ value: v, label: `资源窗口 ${v} 周` }))}
          />
        </Space>
      </Card>

      <Row gutter={[12, 12]}>
        <Col xs={12} md={6}>
          <MetricCard
            title="按时交付率"
            rate={delivery?.rate ?? null}
            detail={`按期 ${delivery?.numerator ?? 0} / 已完成 ${delivery?.denominator ?? 0}`}
            extra={delivery?.noPlanDate ? `无计划结束日 ${delivery.noPlanDate}（未计入分母）` : undefined}
          />
        </Col>
        <Col xs={12} md={6}>
          <MetricCard
            title="里程碑达成率"
            rate={milestone?.rate ?? null}
            detail={`达成 ${milestone?.reached ?? 0} / 已判定 ${milestone?.judged ?? 0}`}
            extra={`按期达成 ${milestone?.onTimeReached ?? 0} · 未达成 ${milestone?.missed ?? 0}（按期率 ${formatRate(
              milestone?.onTimeRate ?? null
            )}）`}
          />
        </Col>
        <Col xs={12} md={6}>
          <MetricCard
            title="阶段门一次通过率"
            rate={gate?.rate ?? null}
            detail={`一次通过 ${gate?.firstPass ?? 0} / 人工审批通过 ${gate?.passed ?? 0}`}
            extra={
              gate?.autoApproved
                ? `另有 ${gate.autoApproved} 道门未配置审批流程、自动放行（已剔除，不计入分子/分母）`
                : undefined
            }
          />
        </Col>
        <Col xs={12} md={6}>
          <MetricCard
            title="资源利用率"
            rate={resource?.rate ?? null}
            detail={`计划 ${resource?.plannedHours ?? 0}h / 容量 ${resource?.capacityHours ?? 0}h`}
            extra={
              resource
                ? `窗口 ${resource.window.start} ~ ${resource.window.end}（${resource.window.weeks} 周）` +
                  (resource.unplanned.taskCount > 0
                    ? ` · 未排期 ${resource.unplanned.taskCount} 项（${resource.unplanned.estimateHours}h）`
                    : '')
                : undefined
            }
          />
        </Col>
      </Row>

      <Card size="small" title="组合分布（与列表同过滤 / 同可见性）">
        <Tabs
          items={[
            {
              key: 'status',
              label: '状态',
              children: (
                <BucketTable
                  title="状态"
                  buckets={portfolio?.summary.byStatus ?? []}
                  label={(b) => STATUS_LABEL[b.key] ?? b.key}
                  testId="bucket-status"
                />
              ),
            },
            {
              key: 'health',
              label: '健康度',
              children: (
                <BucketTable
                  title="健康度"
                  buckets={portfolio?.summary.byHealth ?? []}
                  label={(b) => HEALTH_LABEL[b.key]?.label ?? b.key ?? '—'}
                  testId="bucket-health"
                />
              ),
            },
            {
              key: 'type',
              label: '类型',
              children: (
                <BucketTable
                  title="类型"
                  buckets={portfolio?.summary.byType ?? []}
                  label={(b) => TYPE_LABEL[b.key] ?? b.key}
                  testId="bucket-type"
                />
              ),
            },
            {
              key: 'manager',
              label: '负责人',
              children: (
                <BucketTable
                  title="负责人"
                  buckets={portfolio?.summary.byManager ?? []}
                  label={managerBucketLabel}
                  testId="bucket-manager"
                />
              ),
            },
            {
              key: 'department',
              label: '部门',
              children: (
                <BucketTable
                  title="部门"
                  buckets={portfolio?.summary.byDepartment ?? []}
                  label={(b) => departmentBucketLabel(b, deptNames)}
                  testId="bucket-department"
                />
              ),
            },
          ]}
        />
      </Card>

      <Card
        size="small"
        title={`组合项目（共 ${portfolio?.summary.total ?? 0} 个${portfolio?.truncated ? '，已达聚合上限、结果截断' : ''}）`}
      >
        <Table
          rowKey="id"
          columns={projectColumns}
          dataSource={portfolio?.projects ?? []}
          loading={loading}
          size="small"
          scroll={{ x: 1200 }}
          pagination={{ pageSize: 10, showSizeChanger: false }}
          data-testid="portfolio-projects-table"
        />
      </Card>

      <Card size="small" title="度量明细（抽样对账）">
        <Tabs
          items={[
            {
              key: 'delivery',
              label: `交付明细（${delivery?.detailsTotal ?? 0}）`,
              children: (
                <Table
                  rowKey={(r) => `d-${r.projectId}`}
                  columns={deliveryColumns}
                  dataSource={delivery?.details ?? []}
                  size="small"
                  pagination={false}
                  data-testid="detail-delivery"
                />
              ),
            },
            {
              key: 'milestone',
              label: `里程碑明细（${milestone?.detailsTotal ?? 0}）`,
              children: (
                <Table
                  rowKey={(r) => `m-${r.milestoneId}`}
                  columns={milestoneColumns}
                  dataSource={milestone?.details ?? []}
                  size="small"
                  pagination={false}
                  data-testid="detail-milestone"
                />
              ),
            },
            {
              key: 'gate',
              label: `阶段门明细（${gate?.detailsTotal ?? 0}）`,
              children: (
                <Table
                  rowKey={(r) => `g-${r.projectId}-${r.gate}`}
                  columns={gateColumns}
                  dataSource={gate?.details ?? []}
                  size="small"
                  pagination={false}
                  data-testid="detail-gate"
                />
              ),
            },
            {
              key: 'resource',
              label: `资源明细（${resource?.projects.length ?? 0}）`,
              children: (
                <Table
                  rowKey={(r) => `r-${r.projectId}`}
                  columns={resourceColumns}
                  dataSource={resource?.projects ?? []}
                  size="small"
                  pagination={false}
                  data-testid="detail-resource"
                />
              ),
            },
          ]}
        />
      </Card>
    </Space>
  );
}
