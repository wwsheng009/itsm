/**
 * I1-03 页面内联建议条（§3.6 允许形态 2：可关闭、「不再提示」记忆）。
 *
 * 非打断约束：仅在静默模式关闭时出现；不会自动打开面板/自动发送（点击「让 AI 看」只预填）；
 * `role="status"` + `aria-live="polite"`（不抢焦点）。
 */
import React from 'react';
import { Button, theme } from 'antd';
import { Sparkles, X } from 'lucide-react';

import type { ProactiveSignal } from '@/lib/ai/proactive/signals';
import { useI18n } from '@/lib/i18n/useI18n';

export const PROACTIVE_BAR_TESTID = 'assistant-proactive-bar';

interface ProactiveSuggestionBarProps {
  signal: ProactiveSignal;
  onAction: (signal: ProactiveSignal) => void;
  /** 本次会话关闭（不记忆）。 */
  onDismiss: (signal: ProactiveSignal) => void;
  /** 「不再提示」（持久化记忆）。 */
  onDismissForever: (signal: ProactiveSignal) => void;
}

const ProactiveSuggestionBar: React.FC<ProactiveSuggestionBarProps> = ({
  signal,
  onAction,
  onDismiss,
  onDismissForever,
}) => {
  const { token } = theme.useToken();
  const { t } = useI18n();
  const actionLabel =
    signal.action.type === 'navigate'
      ? t('assistant.suggestions.proactiveActionNavigate')
      : t('assistant.suggestions.proactiveActionPrompt');

  return (
    <div
      data-testid={PROACTIVE_BAR_TESTID}
      role="status"
      aria-live="polite"
      aria-label={t('assistant.suggestions.proactiveAria')}
      style={{
        position: 'fixed',
        right: 24,
        bottom: 84,
        width: 340,
        maxWidth: 'calc(100vw - 48px)',
        zIndex: 979,
        display: 'flex',
        gap: 10,
        padding: '10px 12px',
        borderRadius: 10,
        background: token.colorBgElevated,
        border: `1px solid ${token.colorBorderSecondary}`,
        boxShadow: token.boxShadowSecondary,
      }}
    >
      <Sparkles size={16} color={token.colorPrimary} style={{ flexShrink: 0, marginTop: 2 }} />
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: token.colorText }}>
          {signal.label}
        </div>
        {signal.description ? (
          <div
            style={{
              marginTop: 2,
              fontSize: 12,
              color: token.colorTextSecondary,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {signal.description}
          </div>
        ) : null}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 6 }}>
          <Button size="small" type="primary" onClick={() => onAction(signal)}>
            {actionLabel}
          </Button>
          <Button size="small" type="link" onClick={() => onDismissForever(signal)}>
            {t('assistant.suggestions.proactiveDismissForever')}
          </Button>
        </div>
      </div>
      <Button
        data-testid="assistant-proactive-bar-close"
        type="text"
        size="small"
        aria-label={t('assistant.suggestions.proactiveClose')}
        icon={<X size={12} />}
        onClick={() => onDismiss(signal)}
      />
    </div>
  );
};

export default ProactiveSuggestionBar;