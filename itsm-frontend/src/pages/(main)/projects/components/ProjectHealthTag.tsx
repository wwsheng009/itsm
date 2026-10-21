import { Popover, Space, Tag, Typography } from 'antd';
import type { HealthReason, ProjectHealth } from '@/lib/services/project-service';

/**
 * 健康度展示词表（PE-27：列表/详情共用；与后端派生词表 green|yellow|red 对齐）。
 */
export const HEALTH_META: Record<ProjectHealth, { label: string; color: string }> = {
  green: { label: '健康', color: 'success' },
  yellow: { label: '关注', color: 'warning' },
  red: { label: '风险', color: 'error' },
};

/**
 * 项目健康度标签（PE-27 派生健康度 + 可展开解释；A7 数据完备度）。
 *
 * 后端 health 为事实实时派生值（进度偏差/预算偏差/风险/逾期任务等），
 * healthReasons 为命中规则的解释项；healthDataQuality=insufficient 表示
 * 缺基线/缺归属/无任务——此时「健康」只代表「未命中风险规则」，不代表数据可信。
 */
export function ProjectHealthTag({
  health,
  reasons,
  dataQuality,
  dataGaps,
}: {
  health?: ProjectHealth | string;
  reasons?: HealthReason[];
  dataQuality?: 'sufficient' | 'insufficient';
  dataGaps?: string[];
}) {
  const key = (health ?? 'green') as ProjectHealth;
  const meta = HEALTH_META[key] ?? { label: String(health ?? '—'), color: 'default' };
  const insufficient = dataQuality === 'insufficient';
  const tag = (
    <Tag color={insufficient ? 'default' : meta.color}>{insufficient ? `${meta.label}（数据不足）` : meta.label}</Tag>
  );
  const hasReasons = Boolean(reasons && reasons.length > 0);
  const hasGaps = Boolean(dataGaps && dataGaps.length > 0);
  if (!hasReasons && !hasGaps) {
    return tag;
  }
  return (
    <Popover
      placement="right"
      title="健康度解释（事实派生）"
      content={
        <Space direction="vertical" size={2} style={{ maxWidth: 360 }}>
          {hasReasons
            ? reasons!.map((reason) => (
                <Typography.Text key={reason.rule} type={reason.level === 'red' ? 'danger' : 'warning'}>
                  · {reason.message}
                </Typography.Text>
              ))
            : null}
          {hasGaps
            ? dataGaps!.map((gap) => (
                <Typography.Text key={gap} type="secondary">
                  · 数据缺口：{gap}
                </Typography.Text>
              ))
            : null}
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {insufficient
              ? '以上缺口使健康度缺乏区分力：请补全归属/计划/基线/任务后再据此决策'
              : '由进度偏差、预算偏差、风险与任务/里程碑事实实时派生'}
          </Typography.Text>
        </Space>
      }
    >
      <span style={{ cursor: 'help' }}>{tag}</span>
    </Popover>
  );
}

export default ProjectHealthTag;
