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
 * 项目健康度标签（PE-27 派生健康度 + 可展开解释）。
 *
 * 后端 health 为事实实时派生值（进度偏差/预算偏差/风险/逾期任务等），
 * healthReasons 为命中规则的解释项；有解释项时悬浮可展开查看明细。
 */
export function ProjectHealthTag({
  health,
  reasons,
}: {
  health?: ProjectHealth | string;
  reasons?: HealthReason[];
}) {
  const key = (health ?? 'green') as ProjectHealth;
  const meta = HEALTH_META[key] ?? { label: String(health ?? '—'), color: 'default' };
  const tag = <Tag color={meta.color}>{meta.label}</Tag>;
  if (!reasons || reasons.length === 0) {
    return tag;
  }
  return (
    <Popover
      placement="right"
      title="健康度解释（事实派生）"
      content={
        <Space direction="vertical" size={2} style={{ maxWidth: 360 }}>
          {reasons.map((reason) => (
            <Typography.Text key={reason.rule} type={reason.level === 'red' ? 'danger' : 'warning'}>
              · {reason.message}
            </Typography.Text>
          ))}
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            由进度偏差、预算偏差、风险与任务/里程碑事实实时派生
          </Typography.Text>
        </Space>
      }
    >
      <span style={{ cursor: 'help' }}>{tag}</span>
    </Popover>
  );
}

export default ProjectHealthTag;
