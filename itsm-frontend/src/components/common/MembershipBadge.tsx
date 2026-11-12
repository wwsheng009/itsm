import React from 'react';
import { Tag, Tooltip } from 'antd';

import { useI18n } from '@/lib/i18n/useI18n';

export interface MembershipBadgeProps {
  /** 操作者在当前租户内的 membership ID；缺省（历史数据）不渲染。 */
  membershipId?: number | null;
  /** 可选前缀，如「作者」「审批人」；缺省只显示身份 ID。 */
  prefix?: string;
}

/**
 * MembershipBadge —— C 方案租户内身份的显式展示。
 *
 * 目标：让评论作者、附件上传人、审批人、通知接收人的 membership 身份在 UI 上可见，
 * 而不是只存在于响应字段中。provider 深度切换到客户租户后，这里展示的是客户租户
 * 内的 provider membership（不是 provider 家租户的 users.id）。
 */
export default function MembershipBadge({ membershipId, prefix }: MembershipBadgeProps) {
  const { t } = useI18n();
  const id = Number(membershipId);
  if (!Number.isFinite(id) || id <= 0) return null;

  const label = t('detailTabs.membershipIdentity', { id });
  return (
    <Tooltip title={t('detailTabs.membershipIdentityTooltip')}>
      <Tag
        color="geekblue"
        style={{ margin: 0 }}
        data-testid={`membership-badge-${id}`}
      >
        {prefix ? `${prefix} · ${label}` : label}
      </Tag>
    </Tooltip>
  );
}
