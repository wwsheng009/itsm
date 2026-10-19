/**
 * I0-04 Outcome Contract 回执卡：展示本回合的可验证落点（对象/产物/草稿/跳转）并提供动作。
 * - 「打开」：跳转目标页（由调用方注入 SPA 导航；缺省 window.location.assign 兜底）；
 * - 「复制引用」：复制稳定引用（如 artifact:31），便于核验/工单备注；
 * - 埋点：inline_applied(panel=outcome_receipt, action=open|copy)。
 */
import React, { useCallback } from 'react';
import { Button, Card, Space, Tag, Typography, message } from 'antd';
import { Copy, ExternalLink, PackageCheck } from 'lucide-react';

import { trackAssistantEvent } from '@/lib/ai/telemetry';
import type { AssistantOutcome } from '@/lib/ai/outcome';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

const KIND_COLORS: Record<AssistantOutcome['kind'], string> = {
  entity: 'blue',
  artifact: 'geekblue',
  draft: 'gold',
  navigation: 'default',
};

const KIND_LABEL_KEYS: Record<AssistantOutcome['kind'], string> = {
  entity: 'assistant.outcome.kindEntity',
  artifact: 'assistant.outcome.kindArtifact',
  draft: 'assistant.outcome.kindDraft',
  navigation: 'assistant.outcome.kindNavigation',
};

export interface OutcomeReceiptProps {
  outcomes: AssistantOutcome[];
  /** 打开落点（SPA 导航）；缺省使用 window.location.assign。 */
  onOpen?: (outcome: AssistantOutcome) => void;
}

export const OutcomeReceipt: React.FC<OutcomeReceiptProps> = ({ outcomes, onOpen }) => {
  const { t } = useI18n();
  const handleOpen = useCallback(
    (outcome: AssistantOutcome) => {
      if (!outcome.route) return;
      trackAssistantEvent('inline_applied', {
        pageKey: 'ai.chat',
        outcomeType: outcome.kind === 'artifact' ? 'artifact' : 'navigation',
        extra: { panel: 'outcome_receipt', action: 'open', ref: outcome.ref },
      });
      if (onOpen) {
        onOpen(outcome);
        return;
      }
      window.location.assign(outcome.route);
    },
    [onOpen]
  );

  const handleCopy = useCallback(async (outcome: AssistantOutcome) => {
    // 先记录动作（剪贴板不可用时引用在卡片上仍可见，不影响可验证性）。
    trackAssistantEvent('inline_applied', {
      pageKey: 'ai.chat',
      extra: { panel: 'outcome_receipt', action: 'copy', ref: outcome.ref },
    });
    if (!navigator.clipboard?.writeText) return;
    try {
      await navigator.clipboard.writeText(outcome.ref);
      message.success(t('assistant.outcome.copied', { ref: outcome.ref }));
    } catch {
      // 剪贴板拒绝（权限/非安全上下文）：静默降级，不打断阅读。
    }
  }, [t]);

  if (outcomes.length === 0) return null;

  return (
    <Card
      size="small"
      data-testid="outcome-receipt"
      style={{ marginTop: 8, borderColor: '#d6e4ff', background: '#f5f8ff' }}
      title={
        <Space size={6}>
          <PackageCheck size={14} color="#2f54eb" />
          <span style={{ fontSize: 13 }}>{t('assistant.outcome.title')}</span>
        </Space>
      }
    >
      <Space direction="vertical" size={6} style={{ width: '100%' }}>
        {outcomes.map(outcome => (
          <div
            key={`${outcome.kind}-${outcome.ref}`}
            data-testid={`outcome-${outcome.kind}-${outcome.ref}`}
            style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}
          >
            <Tag color={KIND_COLORS[outcome.kind]} style={{ marginInlineEnd: 0 }}>
              {t(KIND_LABEL_KEYS[outcome.kind])}
            </Tag>
            <Text style={{ fontSize: 13 }}>{outcome.label}</Text>
            <Text type="secondary" code style={{ fontSize: 11 }}>
              {outcome.ref}
            </Text>
            <span style={{ flex: 1 }} />
            {outcome.route ? (
              <Button
                size="small"
                type="link"
                icon={<ExternalLink size={13} />}
                onClick={() => handleOpen(outcome)}
                data-testid={`outcome-open-${outcome.kind}-${outcome.ref}`}
              >
                {t('assistant.outcome.open')}
              </Button>
            ) : null}
            <Button
              size="small"
              type="link"
              icon={<Copy size={13} />}
              onClick={() => void handleCopy(outcome)}
              data-testid={`outcome-copy-${outcome.kind}-${outcome.ref}`}
            >
              {t('assistant.outcome.copyRef')}
            </Button>
          </div>
        ))}
      </Space>
    </Card>
  );
};

export default OutcomeReceipt;