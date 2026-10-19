/**
 * V1-02 邮件场景：把 V1-03 的口语理解（POST /ai/tickets/interpret，只读、确定性词典）
 * 带到邮件 intake 复核——「这封邮件讲的什么？分类/优先级建议」。
 *
 * 流程：复核人点「运行 AI 理解」→ 「我理解成：网络故障（高）」+ 证据片段 + 置信度 →
 * 「采纳到修正表单」只回填空白字段（标题/描述），仍需复核人自行「保存并重新核验」。
 * 边界：未命中降级（other/medium 且 matched=false）如实标注；失败不阻断人工流程；
 * 不自动开单、不自动保存——写边界与 V0-02/V1-03 一致。
 * 门禁：`ai:read` ∩ `ticket:read`（与后端路由同码），无权限不渲染。
 * 埋点：复用既有事件 suggestion_shown / suggestion_clicked / error_shown（事件字典零变更）。
 */
import React, { useCallback, useState } from 'react';
import { Alert, Button, Card, Tag, Typography, theme } from 'antd';
import { Bot, Sparkles } from 'lucide-react';

import { aiInterpretTicketIntent, type TicketIntentInterpretation } from '@/lib/api/ai-api';
import { useCan } from '@/components/auth/PermissionGate';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useI18n } from '@/lib/i18n/useI18n';

export const EMAIL_INTENT_SUGGEST_TESTID = 'email-intent-suggest';
export const EMAIL_INTENT_SUGGEST_RUN_TESTID = 'email-intent-suggest-run';
export const EMAIL_INTENT_SUGGEST_RESULT_TESTID = 'email-intent-suggest-result';
export const EMAIL_INTENT_SUGGEST_ADOPT_TESTID = 'email-intent-suggest-adopt';
export const EMAIL_INTENT_SUGGEST_DISMISS_TESTID = 'email-intent-suggest-dismiss';

/** 后端 interpret 上限 500 字（超长 400 fail-visible）；邮件正文先截断再送理解。 */
const MAX_UTTERANCE = 500;

const { Text } = Typography;

interface EmailIntentSuggestProps {
  /** 邮件意图文本（标题 + 描述，由调用方组装）。 */
  text: string;
  /** 「采纳」回调：只回填表单（本组件不保存、不开单）。 */
  onAdopt: (fields: { title?: string; description?: string }) => void;
  pageKey?: string;
}

const EmailIntentSuggest: React.FC<EmailIntentSuggestProps> = ({
  text,
  onAdopt,
  pageKey = 'email.intake',
}) => {
  const { token } = theme.useToken();
  const { t } = useI18n();
  const canReadAi = useCan(['ai:read']);
  const canReadTicket = useCan(['ticket:read']);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<TicketIntentInterpretation>();
  const [error, setError] = useState<string>();

  const utterance = text.trim().slice(0, MAX_UTTERANCE);

  const handleRun = useCallback(async () => {
    if (loading || !utterance) return;
    setLoading(true);
    setError(undefined);
    try {
      const interpreted = await aiInterpretTicketIntent(utterance);
      setResult(interpreted);
      trackAssistantEvent('suggestion_shown', {
        pageKey,
        source: 'email_intent',
        extra: { card_id: 'email_intent', kind: 'task', matched: interpreted.matched },
      });
    } catch {
      setResult(undefined);
      setError(t('assistant.email.runFailed'));
      trackAssistantEvent('error_shown', {
        pageKey,
        source: 'email_intent',
        errorCode: 'interpret_failed',
      });
    } finally {
      setLoading(false);
    }
  }, [loading, utterance, pageKey, t]);

  const handleAdopt = useCallback(() => {
    if (!result) return;
    trackAssistantEvent('suggestion_clicked', {
      pageKey,
      source: 'email_intent',
      extra: { card_id: 'email_intent', kind: 'task', matched: result.matched },
    });
    onAdopt({ title: result.fields.title, description: result.fields.description });
  }, [result, pageKey, onAdopt]);

  const handleDismiss = useCallback(() => {
    setResult(undefined);
  }, []);

  // 无权限 / 无可理解文本时不渲染（入口级降级，不报错）。
  if (!canReadAi || !canReadTicket || !utterance) return null;

  return (
    <Card
      size="small"
      data-testid={EMAIL_INTENT_SUGGEST_TESTID}
      title={
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13 }}>
          <Bot size={14} color={token.colorPrimary} />
          {t('assistant.email.title')}
        </span>
      }
      style={{ marginBottom: 12 }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('assistant.email.intro')}
        </Text>

        {!result ? (
          <span>
            <Button
              size="small"
              icon={<Sparkles size={14} />}
              loading={loading}
              onClick={() => void handleRun()}
              data-testid={EMAIL_INTENT_SUGGEST_RUN_TESTID}
            >
              {t('assistant.email.run')}
            </Button>
          </span>
        ) : (
          <div data-testid={EMAIL_INTENT_SUGGEST_RESULT_TESTID}>
            <div style={{ fontSize: 13 }}>
              {t('assistant.email.understood')}<Text strong>{result.summary}</Text>
              <Text type="secondary" style={{ marginLeft: 8, fontSize: 12 }}>
                {t('assistant.email.confidence', { pct: Math.round(result.confidence * 100) })}
              </Text>
            </div>
            {result.terms.length > 0 ? (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 6 }}>
                {result.terms.map((term, index) => (
                  <Tag key={`${term.field}-${index}`} color="geekblue">
                    {term.surface} → {term.term}
                  </Tag>
                ))}
              </div>
            ) : null}
            {!result.matched ? (
              <Alert
                type="warning"
                showIcon
                style={{ marginTop: 8 }}
                message={t('assistant.email.unmatched')}
              />
            ) : null}
            <div style={{ marginTop: 8, display: 'flex', gap: 8 }}>
              <Button
                type="primary"
                size="small"
                onClick={handleAdopt}
                data-testid={EMAIL_INTENT_SUGGEST_ADOPT_TESTID}
              >
                {t('assistant.email.adopt')}
              </Button>
              <Button size="small" onClick={handleDismiss} data-testid={EMAIL_INTENT_SUGGEST_DISMISS_TESTID}>
                {t('assistant.email.dismiss')}
              </Button>
            </div>
          </div>
        )}

        {error ? <Alert type="warning" showIcon message={error} /> : null}
      </div>
    </Card>
  );
};

export default EmailIntentSuggest;