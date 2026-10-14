import React, { useEffect, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  DatePicker,
  Descriptions,
  Empty,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Download, Lock } from 'lucide-react';
import dayjs, { Dayjs } from 'dayjs';
import { MSPAPI } from '@/lib/api/msp-api';
import { SLAApi } from '@/lib/api/sla-api';
import type { SLASettlementReport, SLASettlementRow } from '@/lib/api/sla-api';

const { Title, Text } = Typography;
const { RangePicker } = DatePicker;

/**
 * SLA 结算报表（服务商视角，D-SLA-5/D-SLA-9，P2-2 后端契约）。
 *
 * 口径（§13.2/§13.5/§13.6）全部由后端判定，前端只渲染、不重算：
 *   - 白名单分母 = 合同覆盖类型工单（归属校验在服务端取数前完成，越权 403 无计数）；
 *   - 双口径对账三列：contractVerdict（合同重算）/ internalVerdict（客户内部快照）/ driftFlag；
 *   - 未覆盖/豁免单列桶；内部域违约（L2/L3*）不进结算；信用额按合同 penalties 计算；
 *   - 导出为客户端 CSV（快照最小实现，§13.3）；锁定为显式动作（锁定后不可再变更）。
 * 非 MSP 部署 / 无生效合同 → 后端 404：页面渲染解释性空态，不报错。
 */

type PageState = 'idle' | 'ready' | 'unavailable';

const VERDICT_META: Record<string, { color: string; label: string }> = {
  met: { color: 'green', label: '达成' },
  breached: { color: 'red', label: '违约' },
  unknown: { color: 'default', label: '未知' },
};

const fmt = (value?: string): string =>
  value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';

const errStatus = (e: unknown): number | undefined => {
  const err = e as { status?: number; httpStatus?: number } | undefined;
  return err?.status ?? err?.httpStatus;
};

const csvCell = (v: unknown): string => {
  const s = v === undefined || v === null ? '' : String(v);
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

const SLASettlementReportPage: React.FC = () => {
  const { message, modal } = App.useApp();
  const [customers, setCustomers] = useState<{ id: number; code?: string; name: string }[]>([]);
  const [customerId, setCustomerId] = useState<number | undefined>(undefined);
  const [range, setRange] = useState<[Dayjs, Dayjs]>(() => [dayjs().subtract(29, 'day'), dayjs()]);
  const [report, setReport] = useState<SLASettlementReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [locking, setLocking] = useState(false);
  const [pageState, setPageState] = useState<PageState>('idle');

  useEffect(() => {
    (async () => {
      try {
        const resp = await MSPAPI.getCustomers();
        setCustomers(resp?.customers ?? []);
      } catch {
        setCustomers([]); // 非 MSP 员工/部署：下拉为空，页面保持空态
      }
    })();
  }, []);

  const windowParams = (): { from: string; to: string } => ({
    from: range[0].startOf('day').toISOString(),
    to: range[1].endOf('day').toISOString(),
  });

  const loadReport = async (cid: number) => {
    setLoading(true);
    try {
      const { from, to } = windowParams();
      const data = await SLAApi.getSettlementReport({ customerTenantId: cid, from, to });
      setReport(data);
      setPageState('ready');
    } catch (error) {
      const status = errStatus(error);
      setReport(null);
      if (status === 404) {
        setPageState('unavailable'); // 非 MSP 部署或该客户无生效合同
      } else {
        setPageState('idle');
        message.error(status === 403 ? '该客户不属于当前服务商，拒绝访问' : '加载结算报表失败');
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (customerId && customerId > 0) {
      void loadReport(customerId);
    }
  }, [customerId]);

  const exportCsv = () => {
    if (!report) return;
    const headers = [
      '工单号',
      '标题',
      '类型',
      '匹配层级',
      '合同判定',
      '内部判定',
      '对账漂移',
      '裁决/排除',
      '建单时间',
      '首次响应',
      '解决时间',
    ];
    const lines = report.rows.map((r) =>
      [
        r.ticketNumber,
        r.title,
        r.ticketTypeCode || r.ticketTypeId,
        r.matchedLayer || '',
        VERDICT_META[r.contractVerdict]?.label || r.contractVerdict,
        r.internalVerdict === 'unknown' ? '未知' : VERDICT_META[r.internalVerdict]?.label || r.internalVerdict,
        r.driftFlag ? '是' : '否',
        r.excludedReason ? `已排除(${r.excludedReason})` : r.violationState || '',
        fmt(r.createdAt),
        fmt(r.firstResponseAt),
        fmt(r.resolvedAt),
      ]
        .map(csvCell)
        .join(',')
    );
    const csv = '\uFEFF' + [headers.join(','), ...lines].join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `sla-settlement-${report.customerTenantId}-${range[0].format('YYYYMMDD')}-${range[1].format('YYYYMMDD')}.csv`;
    link.click();
    URL.revokeObjectURL(url);
  };

  const handleLock = () => {
    if (!report || !customerId) return;
    modal.confirm({
      title: '锁定本期结算快照？',
      content: '锁定后该周期违约的归责状态不可再变更（新争议走下一周期，§13.3）。',
      okText: '锁定',
      cancelText: '取消',
      onOk: async () => {
        setLocking(true);
        try {
          const { from, to } = windowParams();
          const res = await SLAApi.lockSettlementSnapshot({ customerTenantId: customerId, from, to });
          message.success(`已锁定 ${res?.lockedViolations ?? 0} 条违约记录`);
          await loadReport(customerId);
        } catch {
          message.error('锁定失败：可能无权限（sla:violation:settle）或已被锁定');
        } finally {
          setLocking(false);
        }
      },
    });
  };

  const columns: ColumnsType<SLASettlementRow> = [
    {
      title: '工单号',
      dataIndex: 'ticketNumber',
      width: 150,
      render: (v: string) => v || '-',
    },
    { title: '标题', dataIndex: 'title', ellipsis: true },
    {
      title: '类型',
      key: 'type',
      width: 130,
      render: (_, r) => r.ticketTypeCode || (r.ticketTypeId ? `#${r.ticketTypeId}` : '-'),
    },
    {
      title: '层级',
      dataIndex: 'matchedLayer',
      width: 80,
      render: (v?: string) => (v ? <Tag>{v}</Tag> : '-'),
    },
    {
      title: '合同判定',
      key: 'contractVerdict',
      width: 100,
      render: (_, r) => (
        <Tag color={VERDICT_META[r.contractVerdict]?.color}>{VERDICT_META[r.contractVerdict]?.label}</Tag>
      ),
    },
    {
      title: '内部判定',
      key: 'internalVerdict',
      width: 100,
      render: (_, r) => (
        <Tag color={VERDICT_META[r.internalVerdict]?.color}>{VERDICT_META[r.internalVerdict]?.label}</Tag>
      ),
    },
    {
      title: '对账',
      key: 'drift',
      width: 90,
      render: (_, r) => (r.driftFlag ? <Tag color="warning">漂移</Tag> : '一致'),
    },
    {
      title: '裁决/排除',
      key: 'state',
      width: 130,
      render: (_, r) =>
        r.excludedReason ? (
          <Tag color="default">已排除({r.excludedReason})</Tag>
        ) : r.violationState ? (
          <Tag>{r.violationState}</Tag>
        ) : (
          '-'
        ),
    },
    { title: '建单时间', dataIndex: 'createdAt', width: 170, render: fmt },
  ];

  return (
    <div className="p-6 min-h-screen bg-gray-50">
      <div className="mb-6">
        <Title level={2} style={{ marginBottom: 4 }}>
          SLA 结算报表（服务商视角）
        </Title>
        <Text type="secondary">
          白名单口径（合同覆盖工单）+ 双口径对账（合同重算 vs 客户内部快照）+ 违约域过滤；数据由后端判定，界面不重算
        </Text>
      </div>

      <Card className="mb-4">
        <Space wrap size="middle">
          <Select
            style={{ width: 280 }}
            placeholder="选择服务客户"
            showSearch
            optionFilterProp="label"
            value={customerId}
            onChange={(v) => setCustomerId(v)}
            options={customers.map((c) => ({
              value: c.id,
              label: `${c.name}${c.code ? `（${c.code}）` : ''}`,
            }))}
            loading={loading && customers.length === 0}
          />
          <RangePicker
            value={range}
            allowClear={false}
            onChange={(v) => {
              if (v && v[0] && v[1]) setRange([v[0], v[1]]);
            }}
          />
          <Button type="primary" loading={loading} disabled={!customerId} onClick={() => customerId && void loadReport(customerId)}>
            查询
          </Button>
          <Button icon={<Download size={16} />} disabled={!report} onClick={exportCsv}>
            导出 CSV
          </Button>
          <Button danger icon={<Lock size={16} />} loading={locking} disabled={!report} onClick={handleLock}>
            锁定快照
          </Button>
        </Space>
      </Card>

      {pageState === 'unavailable' && (
        <Alert
          type="info"
          showIcon
          className="mb-4"
          message="结算报表不可用"
          description="当前部署未启用 MSP（RequireMSPDeploymentMode），或该客户在所选期间没有生效合同。请确认演示环境为 saas_msp 且合同已生效。"
        />
      )}

      {!customerId && (
        <Card>
          <Empty description="请选择服务客户后查询结算报表" />
        </Card>
      )}

      {report && (
        <>
          <Row gutter={[16, 16]} className="mb-4">
            <Col xs={24} sm={8} lg={4}>
              <Card className="rounded-lg shadow-sm" style={{ height: '100%' }}>
                <Statistic title="合同覆盖工单（分母）" value={report.coveredTickets} />
              </Card>
            </Col>
            <Col xs={24} sm={8} lg={4}>
              <Card className="rounded-lg shadow-sm" style={{ height: '100%' }}>
                <Statistic title="合同达成" value={report.contractMet} styles={{ content: { color: '#52c41a' } }} />
              </Card>
            </Col>
            <Col xs={24} sm={8} lg={4}>
              <Card className="rounded-lg shadow-sm" style={{ height: '100%' }}>
                <Statistic title="计违约" value={report.settlementBreaches} styles={{ content: { color: '#ff4d4f' } }} />
              </Card>
            </Col>
            <Col xs={24} sm={8} lg={4}>
              <Card className="rounded-lg shadow-sm" style={{ height: '100%' }}>
                <Statistic title="合规率" value={report.complianceRate} precision={1} suffix="%" />
              </Card>
            </Col>
            <Col xs={24} sm={8} lg={4}>
              <Card className="rounded-lg shadow-sm" style={{ height: '100%' }}>
                <Statistic title="认账违约" value={report.acceptedViolations} />
              </Card>
            </Col>
            <Col xs={24} sm={8} lg={4}>
              <Card className="rounded-lg shadow-sm" style={{ height: '100%' }}>
                <Statistic
                  title="信用额"
                  value={report.credit?.amount ?? 0}
                  precision={2}
                  suffix={report.credit?.currency || ''}
                />
              </Card>
            </Col>
          </Row>

          <Card className="mb-4" size="small">
            <Space wrap size="large">
              <Text>
                合同：<Text strong>{report.contractNo}</Text>（{report.contractName}）
              </Text>
              <Text type="secondary">
                周期：{fmt(report.reportPeriod?.startDate)} ~ {fmt(report.reportPeriod?.endDate)}
              </Text>
              <Text>
                豁免桶 <Tag>{report.exemptTickets}</Tag> 未覆盖桶 <Tag>{report.uncoveredTickets}</Tag>
                内部域排除违约 <Tag>{report.excludedInternalViolations}</Tag>
              </Text>
              <Text type="secondary">
                违约裁决分布：认账 {report.acceptedViolations} / 豁免 {report.waivedViolations} / 争议{' '}
                {report.disputedViolations} / 待处理 {report.pendingViolations}
              </Text>
            </Space>
          </Card>

          <Card className="mb-4" size="small" title="信用额（按合同 penalties）">
            <Descriptions size="small" column={4}>
              <Descriptions.Item label="费率 creditRate">{report.credit?.creditRate ?? 0}</Descriptions.Item>
              <Descriptions.Item label="上限 cap">{report.credit?.cap ?? 0}</Descriptions.Item>
              <Descriptions.Item label="计违约数">{report.credit?.breachUnits ?? 0}</Descriptions.Item>
              <Descriptions.Item label="金额">{report.credit?.amount ?? 0}</Descriptions.Item>
            </Descriptions>
            {report.credit?.note && <Text type="secondary">{report.credit.note}</Text>}
          </Card>

          <Card title="对账明细（双口径）" size="small">
            <Table<SLASettlementRow>
              columns={columns}
              dataSource={report.rows}
              rowKey="ticketId"
              size="small"
              loading={loading}
              pagination={{ pageSize: 10, showSizeChanger: true }}
              scroll={{ x: 1300 }}
              locale={{ emptyText: '所选期间无合同覆盖工单' }}
            />
          </Card>
        </>
      )}
    </div>
  );
};

export default SLASettlementReportPage;
