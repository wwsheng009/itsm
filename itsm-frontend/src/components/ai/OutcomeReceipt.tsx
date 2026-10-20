/**
 * I0-04 Outcome Contract 回执卡：展示本回合的可验证落点（对象/产物/草稿/跳转）并提供动作。
 *
 * 渲染规则（本次修复的验收口径）：
 *  1. 标题回退链：title → name → 摘要首句 → 类型标签（不再拼「产物 #id」占位名）；
 *     走到最后一档时上报一次 `inline_preview`（action=title_fallback），让缺名称可观测。
 *  2. 内部标识（artifact:31 / draft:/tickets/create）不进正文；只在「复制引用」时写入剪贴板。
 *  3. 动作：可跳转才有「打开」；「复制」「引用」恒可用（引用=把稳定 ref 复制为核验线索）。
 *  4. 类型标签与标题相同（或标题已包含类型词）时隐藏标签，避免「产物 产物」撞车。
 *  5. 证据为空时不再渲染孤立复制按钮，改为一行说明；整卡无可用落点则不渲染。
 */
import React, { useCallback, useMemo, useState } from 'react';
import { Button, Card, Space, Tag, Tooltip, Typography, message } from 'antd';
import { Copy, ExternalLink, PackageCheck, Quote } from 'lucide-react';

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

/** 标题回退：title/name → 摘要首句 → 空（由调用方决定是否用类型标签兜底）。 */
export const resolveOutcomeTitle = (outcome: AssistantOutcome): string => {
  const direct = (outcome.title ?? outcome.name ?? '').trim();
  if (direct) return direct;
  const summary = (outcome.description ?? '').trim();
  if (!summary) return '';
  const firstSentence = summary.split(/[\n。！？.!?]/).map(part => part.trim()).filter(Boolean)[0];
  return firstSentence ? firstSentence.slice(0, 40) : '';
};

/** 标题是否与类型标签撞车（相同/以类型词开头/仅含类型+序号）。 */
export const shouldHideKindTag = (kindLabel: string, title: string): boolean => {
  const normalizedTitle = title.trim();
  if (!normalizedTitle) return true;
  if (normalizedTitle === kindLabel) return true;
  return new RegExp(`^${kindLabel}\\s*[#＃]?\\s*\\d*$`).test(normalizedTitle);
};

/** 引用文案：稳定引用用于核验，不展示在正文。 */
export const buildCitationText = (outcome: AssistantOutcome): string => {
  const title = resolveOutcomeTitle(outcome) || outcome.ref;
  return outcome.route ? `${title}（${outcome.ref}，${outcome.route}）` : `${title}（${outcome.ref}）`;
};

export interface OutcomeReceiptProps {
  outcomes: AssistantOutcome[];
  /** 打开落点（SPA 导航）；缺省使用 window.location.assign。 */
  onOpen?: (outcome: AssistantOutcome) => void;
}

export const OutcomeReceipt: React.FC<OutcomeReceiptProps> = ({ outcomes, onOpen }) => {
  const { t } = useI18n();
  const [fallbackReported, setFallbackReported] = useState<Record<string, true>>({});

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

  /** 复制引用的容错：空字符串视为失败（避免「复制成功但剪贴板为空」）。 */
  const writeClipboard = useCallback(async (text: string): Promise<boolean> => {
    if (!navigator.clipboard?.writeText || !text) return false;
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      return false;
    }
  }, []);

  const handleCopy = useCallback(
    async (outcome: AssistantOutcome) => {
      // 「复制」复制人类可读标题（用于粘贴到聊天/工单描述）；稳定引用交给「引用」。
      const text = resolveOutcomeTitle(outcome) || buildCitationText(outcome);
      trackAssistantEvent('inline_applied', {
        pageKey: 'ai.chat',
        extra: { panel: 'outcome_receipt', action: 'copy', ref: outcome.ref },
      });
      const ok = await writeClipboard(text);
      if (ok) {
        message.success(t('assistant.outcome.copyEntry', { title: text }));
      } else if (!navigator.clipboard?.writeText) {
        // 剪贴板不可用：引用仍可见，不打断阅读。
      } else {
        message.warning(t('assistant.outcome.copyFailed'));
      }
    },
    [t, writeClipboard]
  );

  const handleCite = useCallback(
    async (outcome: AssistantOutcome) => {
      const text = buildCitationText(outcome);
      trackAssistantEvent('inline_applied', {
        pageKey: 'ai.chat',
        extra: { panel: 'outcome_receipt', action: 'cite', ref: outcome.ref },
      });
      const ok = await writeClipboard(text);
      if (ok) {
        message.success(t('assistant.outcome.cited', { ref: text }));
        return;
      }
      message.warning(t('assistant.outcome.copyFailed'));
    },
    [t, writeClipboard]
  );

  const rows = useMemo(
    () =>
      outcomes.map(outcome => {
        const kindLabel = t(KIND_LABEL_KEYS[outcome.kind]);
        // 「正式标题」只认 title/name；摘要首句与类型标签都属于回退档，需打点。
        const hasDirectTitle = Boolean((outcome.title ?? outcome.name ?? '').trim());
        const fallbackTitle = resolveOutcomeTitle(outcome);
        // 最后一档：类型标签（若类型标签本身为空则退回稳定引用，但绝不拼 #id）。
        const title = fallbackTitle || kindLabel || outcome.ref;
        return {
          outcome,
          kindLabel,
          title,
          hideKindTag: shouldHideKindTag(kindLabel, title),
          usedFallback: !hasDirectTitle,
        };
      }),
    [outcomes, t]
  );

  // 缺名称可观测：只在首次渲染时逐条上报，避免每次重渲染重复计数。
  React.useEffect(() => {
    rows.forEach(row => {
      if (!row.usedFallback) return;
      const key = `${row.outcome.kind}:${row.outcome.ref}`;
      if (fallbackReported[key]) return;
      setFallbackReported(prev => ({ ...prev, [key]: true }));
      trackAssistantEvent('inline_preview', {
        pageKey: 'ai.chat',
        outcomeType: row.outcome.kind === 'artifact' ? 'artifact' : undefined,
        extra: { panel: 'outcome_receipt', action: 'title_fallback', ref: row.outcome.ref },
      });
    });
  }, [rows, fallbackReported]);

  if (rows.length === 0) return null;

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
      <Space orientation="vertical" size={6} style={{ width: '100%' }}>
        {rows.map(({ outcome, kindLabel, title, hideKindTag }) => (
          <div
            key={`${outcome.kind}-${outcome.ref}`}
            data-testid={`outcome-${outcome.kind}-${outcome.ref}`}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              flexWrap: 'wrap',
              padding: '2px 0',
            }}
          >
            {hideKindTag ? null : (
              <Tag color={KIND_COLORS[outcome.kind]} style={{ marginInlineEnd: 0 }}>
                {kindLabel}
              </Tag>
            )}
            <Text style={{ fontSize: 13, fontWeight: 500 }}>{title}</Text>
            <span style={{ flex: 1, minWidth: 8 }} />
            {outcome.route ? (
              <Button
                size="small"
                type="link"
                icon={<ExternalLink size={13} />}
                onClick={() => handleOpen(outcome)}
                aria-label={`${t('assistant.outcome.open')}：${title}`}
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
              aria-label={`${t('assistant.outcome.copyRef')}：${title}`}
              data-testid={`outcome-copy-${outcome.kind}-${outcome.ref}`}
            >
              {t('assistant.outcome.copyRef')}
            </Button>
            <Button
              size="small"
              type="link"
              icon={<Quote size={13} />}
              onClick={() => void handleCite(outcome)}
              aria-label={`${t('assistant.outcome.cite')}：${title}`}
              data-testid={`outcome-cite-${outcome.kind}-${outcome.ref}`}
            >
              {t('assistant.outcome.cite')}
            </Button>
          </div>
        ))}
        <Text type="secondary" style={{ fontSize: 11 }} data-testid="outcome-hint">
          {t('assistant.outcome.hint')}
        </Text>
      </Space>
    </Card>
  );
};

export default OutcomeReceipt;
