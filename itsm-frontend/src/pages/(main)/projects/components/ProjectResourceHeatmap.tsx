import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Empty, InputNumber, Modal, Select, Space, Table, Tag, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { RefreshCw, Settings } from 'lucide-react';
import { projectService } from '@/lib/services/project-service';
import type {
  CapacityUpdateRequest,
  HeatmapMemberWeek,
  HeatmapWeek,
  MemberView,
  ResourceHeatmap,
} from '@/lib/services/project-service';

/**
 * 项目资源负荷只读热图（P2/PE-23）：
 * 成员 × 周（ISO 周）展示计划/剩余工时 vs 周容量；配色按 plannedRatio。
 * 口径：任务估算在计划窗口自然日均摊、剩余=estimate−actual（逾期落本周）；
 * v1 自然日 + 明确标注（不接租户工作日历），见 docs/project-management/plan/p2-resource-heatmap-design.md。
 */

/** 行模型：一行=一个成员，cells 与 weeks 对齐（缺失补 undefined）。 */
export interface HeatmapRow {
  key: number;
  userId: number;
  name: string;
  username: string;
  projectRole: string;
  memberStatus: 'active' | 'removed';
  cells: (HeatmapMemberWeek | undefined)[];
}

/** 把「周 × 成员」转置为「成员 × 周」表格行（同一成员跨周同序）。 */
export function buildHeatmapRows(weeks: HeatmapWeek[]): HeatmapRow[] {
  if (weeks.length === 0) return [];
  const rows: HeatmapRow[] = [];
  const indexByUser = new Map<number, number>();
  weeks[0].members.forEach((m) => {
    indexByUser.set(m.userId, rows.length);
    rows.push({
      key: m.userId,
      userId: m.userId,
      name: m.name || m.username || `用户#${m.userId}`,
      username: m.username,
      projectRole: m.projectRole,
      memberStatus: m.memberStatus,
      cells: new Array(weeks.length).fill(undefined),
    });
  });
  weeks.forEach((week, weekIndex) => {
    week.members.forEach((m) => {
      let idx = indexByUser.get(m.userId);
      if (idx === undefined) {
        idx = rows.length;
        indexByUser.set(m.userId, idx);
        rows.push({
          key: m.userId,
          userId: m.userId,
          name: m.name || m.username || `用户#${m.userId}`,
          username: m.username,
          projectRole: m.projectRole,
          memberStatus: m.memberStatus,
          cells: new Array(weeks.length).fill(undefined),
        });
      }
      rows[idx].cells[weekIndex] = m;
    });
  });
  return rows;
}

/** 负荷率 → 单元格底色（0 灰、≤50% 绿、≤80% 黄、≤100% 橙、>100% 红）。 */
export function heatmapCellColor(ratio: number): string {
  if (!Number.isFinite(ratio) || ratio <= 0) return '#fafafa';
  if (ratio <= 0.5) return '#d9f7be';
  if (ratio <= 0.8) return '#fff1b8';
  if (ratio <= 1) return '#ffe7ba';
  return '#ffccc7';
}

/** 工时展示：最多 1 位小数（整数不带 .0）。 */
export function formatHours(hours: number): string {
  if (!Number.isFinite(hours)) return '0';
  return (Math.round(hours * 10) / 10).toString();
}

const ROLE_META: Record<string, { label: string; color: string }> = {
  pm: { label: 'PM', color: 'blue' },
  member: { label: '成员', color: 'default' },
  stakeholder: { label: '干系人', color: 'purple' },
};

const WEEK_OPTIONS = [4, 6, 8, 12].map((v) => ({ value: v, label: `${v} 周` }));

interface ProjectResourceHeatmapProps {
  projectId: number;
  canWrite: boolean;
}

export function ProjectResourceHeatmap({ projectId, canWrite }: ProjectResourceHeatmapProps) {
  const [data, setData] = useState<ResourceHeatmap | null>(null);
  const [loading, setLoading] = useState(false);
  const [weeks, setWeeks] = useState(6);

  const [capacityOpen, setCapacityOpen] = useState(false);
  const [capacityLoading, setCapacityLoading] = useState(false);
  const [capacitySaving, setCapacitySaving] = useState(false);
  const [defaultCapacity, setDefaultCapacity] = useState<number | null>(null);
  const [memberInputs, setMemberInputs] = useState<{ userId: number; label: string; value: number | null }[]>([]);

  const fetchHeatmap = useCallback(async () => {
    setLoading(true);
    try {
      setData(await projectService.resourceHeatmap(projectId, { weeks }));
    } catch (error) {
      console.error('Failed to fetch resource heatmap:', error);
      message.error('加载资源负荷失败');
    } finally {
      setLoading(false);
    }
  }, [projectId, weeks]);

  useEffect(() => {
    void fetchHeatmap();
  }, [fetchHeatmap]);

  const openCapacity = async () => {
    setCapacityOpen(true);
    setCapacityLoading(true);
    try {
      const [members, heatmap] = await Promise.all([
        projectService.listMembers(projectId),
        projectService.resourceHeatmap(projectId, { weeks: 1 }),
      ]);
      const active: MemberView[] = members.filter((m) => m.status === 'active');
      setDefaultCapacity(heatmap.defaults.source === 'project' ? heatmap.defaults.defaultWeeklyCapacityHours : null);
      setMemberInputs(
        active.map((m) => ({
          userId: m.userId,
          label: m.name || m.username || `用户#${m.userId}`,
          value: m.weeklyCapacityHours ?? null,
        }))
      );
    } catch (error) {
      console.error('Failed to load capacity config:', error);
      message.error('加载容量配置失败');
      setCapacityOpen(false);
    } finally {
      setCapacityLoading(false);
    }
  };

  const saveCapacity = async () => {
    setCapacitySaving(true);
    try {
      const payload: CapacityUpdateRequest = {
        defaultWeeklyCapacityHours: defaultCapacity,
        members: memberInputs.map((m) => ({ userId: m.userId, weeklyCapacityHours: m.value })),
      };
      await projectService.updateCapacity(projectId, payload);
      message.success('容量配置已保存');
      setCapacityOpen(false);
      await fetchHeatmap();
    } catch (error) {
      console.error('Failed to save capacity config:', error);
      message.error('保存容量配置失败');
    } finally {
      setCapacitySaving(false);
    }
  };

  const rows = buildHeatmapRows(data?.weeks ?? []);
  const columns: ColumnsType<HeatmapRow> = [
    {
      title: '成员',
      dataIndex: 'name',
      key: 'name',
      width: 180,
      fixed: 'left',
      render: (_: unknown, row: HeatmapRow) => (
        <Space size={4} wrap>
          <span>{row.name}</span>
          <Tag color={ROLE_META[row.projectRole]?.color}>{ROLE_META[row.projectRole]?.label ?? row.projectRole}</Tag>
          {row.memberStatus !== 'active' ? <Tag color="default">已移除</Tag> : null}
        </Space>
      ),
    },
    ...(data?.weeks ?? []).map((week, index) => ({
      title: `${week.weekStart.slice(5)} ~ ${week.weekEnd.slice(5)}`,
      key: week.weekStart,
      width: 130,
      render: (_: unknown, row: HeatmapRow) => {
        const cell = row.cells[index];
        if (!cell) return '-';
        return (
          <div
            style={{
              background: heatmapCellColor(cell.plannedRatio),
              borderRadius: 4,
              padding: '4px 6px',
              lineHeight: 1.4,
            }}
          >
            <div style={{ fontWeight: 600 }}>{formatHours(cell.plannedHours)} h</div>
            <div style={{ fontSize: 12, color: '#595959' }}>
              剩 {formatHours(cell.remainingHours)} / 容 {formatHours(cell.capacityHours)}
            </div>
          </div>
        );
      },
    })),
  ];

  return (
    <>
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          message="日历口径（v1 自然日）"
          description={data?.calendar.note ?? 'v1 按自然日均摊（含周末），未接入租户工作日历；周容量为整周口径'}
        />
        {data && data.unplanned.taskCount > 0 ? (
          <Alert
            type="warning"
            showIcon
            message={`${data.unplanned.taskCount} 个有估算的任务未设计划日期（共 ${formatHours(data.unplanned.estimateHours)} h），未计入周负荷`}
          />
        ) : null}
        <Space wrap>
          <Select value={weeks} options={WEEK_OPTIONS} onChange={setWeeks} style={{ width: 110 }} />
          <Button icon={<RefreshCw />} onClick={() => void fetchHeatmap()} loading={loading}>
            刷新
          </Button>
          {canWrite ? (
            <Button icon={<Settings />} onClick={() => void openCapacity()}>
              容量设置
            </Button>
          ) : null}
          <span style={{ color: '#8c8c8c' }}>
            口径：计划=任务估算按计划日期自然日均摊；剩余=估算−实际（逾期落本周）。只读视图，不做自动均衡。
          </span>
        </Space>
        <Table<HeatmapRow>
          rowKey="key"
          columns={columns}
          dataSource={rows}
          pagination={false}
          loading={loading}
          size="small"
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description="暂无成员或周数据" /> }}
        />
      </Space>

      <Modal
        title="周容量设置"
        open={capacityOpen}
        onOk={() => void saveCapacity()}
        confirmLoading={capacitySaving}
        onCancel={() => setCapacityOpen(false)}
        okText="保存"
        cancelText="取消"
      >
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <div>
            <div style={{ marginBottom: 4 }}>项目默认周容量（小时；留空=内置 40）</div>
            <InputNumber
              min={0}
              max={168}
              style={{ width: 200 }}
              value={defaultCapacity}
              onChange={(v) => setDefaultCapacity(v ?? null)}
              placeholder="内置 40"
              disabled={capacityLoading}
            />
          </div>
          <div>
            <div style={{ marginBottom: 4 }}>成员覆盖（留空=继承项目默认）</div>
            <Space direction="vertical" size={8} style={{ width: '100%' }}>
              {memberInputs.map((m, index) => (
                <Space key={m.userId}>
                  <span style={{ display: 'inline-block', minWidth: 120 }}>{m.label}</span>
                  <InputNumber
                    min={0}
                    max={168}
                    style={{ width: 160 }}
                    value={m.value}
                    onChange={(v) =>
                      setMemberInputs((prev) =>
                        prev.map((item, i) => (i === index ? { ...item, value: v ?? null } : item))
                      )
                    }
                    placeholder="继承默认"
                    disabled={capacityLoading}
                  />
                </Space>
              ))}
              {memberInputs.length === 0 && !capacityLoading ? <span style={{ color: '#8c8c8c' }}>暂无存活成员</span> : null}
            </Space>
          </div>
          <span style={{ color: '#8c8c8c' }}>取值 0~168 小时；0 或留空表示清除覆盖（回继承/回内置 40）。</span>
        </Space>
      </Modal>
    </>
  );
}