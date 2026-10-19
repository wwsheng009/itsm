/**
 * D0-01 chat-core：单条消息渲染（用户气泡 / 助手 Markdown + 工具过程 + 确认抽屉 + 引用来源）。
 * 由 AIChat.tsx 逐字搬运（含确认单埋点），页面与后续 Dock 共用。
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Avatar, Button, Spin, Tag, Tooltip, Typography, theme } from 'antd';
import { Bot, Check, Copy, FileText, ThumbsDown, ThumbsUp } from 'lucide-react';

import { aiApproveTool, type RagAnswer, type ToolInvocationDetail } from '@/lib/api/ai-api';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import MarkdownMessage from '../MarkdownMessage';
import ToolApprovalCard from '../tool-approval-card';
import ConfirmationDrawer from '../confirmation-drawer';
import EvidencePanel, { type EvidenceTargetMeta } from '../evidence-panel';
import OutcomeReceipt from '../OutcomeReceipt';
import RunStatusBar from '../run-status-bar';
import ToolCallTimeline, { mergeToolEvents } from '../tool-call-timeline';
import { deriveAssistantOutcomes, type AssistantOutcome } from '@/lib/ai/outcome';
import { PENDING_HIDDEN_STATUSES, PROVIDER_SOURCE_LABELS } from './constants';
import type { ChatMessage } from './types';

const { Text } = Typography;

/** 助手的「补充为知识文章」入口依赖的裁剪条件（与迁移前一致）。 */
const canPromoteToArticle = (m: ChatMessage): boolean =>
  m.role === 'assistant' &&
  !m.streaming &&
  !m.error &&
  Boolean(m.content) &&
  (!m.sources || m.sources.length === 0);

interface ChatMessageItemProps {
  message: ChatMessage;
  providerLabel: (key?: string) => string;
  onCreateArticle: (message: ChatMessage) => void;
  /** M1-05：跳转外置审批页（一期边界 = 外置审批闭环，卡片只提示与跳转）。 */
  onOpenApproval: (invocationId: number) => void;
  /**
   * B1-07：对话内确认。缺省时保持一期行为（卡片只提示 + 跳转）。
   * 注入后由卡片提供「确认 / 拒绝」入口，抽屉负责倒计时与原因必填。
   */
  onRequestConfirm?: (invocationId: number) => void;
  /** B1-07：确认后的刷新回调（状态变化后让外层刷新会话/审计视图）。 */
  onConfirmed?: () => void;
  /** I0-04：结果落点回执的「打开」动作（SPA 导航由调用方注入；缺省整页跳转兜底）。 */
  onOpenOutcome?: (outcome: AssistantOutcome) => void;
}

/** 单条消息：用户 = 右对齐气泡；助手 = 头像 + Markdown 正文 + 操作区。 */
export const ChatMessageItem: React.FC<ChatMessageItemProps> = ({
  message,
  providerLabel,
  onCreateArticle,
  onOpenApproval,
  onRequestConfirm,
  onConfirmed,
  onOpenOutcome,
}) => {
  const { token } = theme.useToken();
  // 入口级门禁：消息级「补充为知识文章」→ knowledge:write（落点 /knowledge/articles/new 路由级同码）。
  const { hasPermission: hasMessagePermission } = usePermissions();
  const canPromoteKnowledge = hasMessagePermission('knowledge', 'write');
  const [copied, setCopied] = useState(false);
  // T5-06a：回答级反馈（点踩率数据源；单条消息只允许反馈一次）。
  const [answerFeedback, setAnswerFeedback] = useState<'helpful' | 'unhelpful' | null>(null);
  const copyTimerRef = useRef<number | null>(null);
  // B1-07：对话内确认抽屉（仅在 BusinessBot 注入 onRequestConfirm 时启用）。
  const [confirmTarget, setConfirmTarget] = useState<ToolInvocationDetail | null>(null);
  const [confirmError, setConfirmError] = useState<string | undefined>(undefined);
  // B1-08：invocation id → 目标对象元信息（卡片拉取详情时顺带汇总，零额外请求）。
  const [detailsById, setDetailsById] = useState<Record<number, EvidenceTargetMeta>>({});

  useEffect(
    () => () => {
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
    },
    []
  );

  const handleCopyAnswer = useCallback(async () => {
    try {
      if (!navigator.clipboard?.writeText) return;
      await navigator.clipboard.writeText(message.content);
      setCopied(true);
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
      copyTimerRef.current = window.setTimeout(() => {
        setCopied(false);
        copyTimerRef.current = null;
      }, 1600);
    } catch {
      // 剪贴板不可用：静默降级，不打断阅读。
    }
  }, [message.content]);

  const handleAnswerFeedback = useCallback(
    (feedback: 'helpful' | 'unhelpful') => {
      if (answerFeedback !== null) return;
      setAnswerFeedback(feedback);
      trackAssistantEvent('message_feedback', {
        pageKey: 'ai.chat',
        conversationId: message.conversationId,
        queryHash: message.queryHash,
        feedback,
        extra: { helpful: feedback === 'helpful' },
      });
    },
    [answerFeedback, message.conversationId, message.queryHash]
  );

  /**
   * 待审批条目（M1-05）：由 SSE 事件配对得到，只取「已提交审批且有 invocationId」的写调用。
   * 事件缺失时不产生卡片（降级为纯文本回答），与时间线同一套配对规则。
   */
  const pendingEntries = useMemo(
    () =>
      mergeToolEvents(message.toolEvents).filter(
        entry => entry.status === 'pending' && typeof entry.invocationId === 'number' && entry.invocationId > 0
      ),
    [message.toolEvents]
  );

  // I0-04：结果落点回执（产物/草稿/审批/跳转；无落点时不渲染）。
  const outcomes = useMemo(
    () =>
      deriveAssistantOutcomes({
        toolEvents: message.toolEvents,
        pendingApprovals: message.pendingApprovals,
      }),
    [message.toolEvents, message.pendingApprovals]
  );

  if (message.role === 'user') {
    return (
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <div
          style={{
            maxWidth: '80%',
            padding: '10px 14px',
            borderRadius: 16,
            background: token.colorFillTertiary,
            color: token.colorText,
            fontSize: 14,
            lineHeight: 1.7,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          {message.content}
        </div>
      </div>
    );
  }

  const providerSourceLabel =
    PROVIDER_SOURCE_LABELS[message.providerInfo?.providerSource ?? ''] ||
    message.providerInfo?.providerSource ||
    '未知';

  return (
    <div className="ai-chat-item" style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
      <Avatar
        size={28}
        icon={<Bot size={16} />}
        style={{
          flexShrink: 0,
          background: token.colorPrimaryBg,
          color: token.colorPrimary,
        }}
      />
      <div style={{ flex: 1, minWidth: 0 }}>
        {message.content ? (
          <MarkdownMessage content={message.content} streaming={message.streaming} />
        ) : message.streaming ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              color: token.colorTextSecondary,
              fontSize: 13,
              padding: '2px 0',
            }}
          >
            <Spin size="small" />
            <span>检索知识库并生成回答…</span>
          </div>
        ) : null}

        {message.error ? (
          <Alert
            type="error"
            showIcon
            message={message.error}
            style={{ marginTop: message.content ? 8 : 0 }}
          />
        ) : null}

        {/* 待审批卡片（M1-05）：写工具提交审批后可见；状态由卡片按需拉取（不轮询）。 */}
        {pendingEntries.map(entry => (
          <ToolApprovalCard
            key={`approval-${entry.invocationId}`}
            invocationId={entry.invocationId as number}
            tool={entry.tool}
            provider={entry.provider}
            server={entry.server}
            onOpenApproval={onOpenApproval}
            onLoaded={detail => {
              setDetailsById(prev => ({
                ...prev,
                [detail.id]: {
                  targetType: detail.targetType,
                  targetId: detail.targetId,
                  supportRef: detail.supportRef,
                },
              }));
            }}
            onRequestConfirm={
              onRequestConfirm
                ? detail => {
                    setConfirmError(undefined);
                    setConfirmTarget(detail);
                    trackAssistantEvent('confirmation_shown', {
                      pageKey: 'ai.chat',
                      extra: { invocationId: detail.id },
                    });
                  }
                : undefined
            }
          />
        ))}

        <OutcomeReceipt
          outcomes={outcomes}
          onOpen={
            onOpenOutcome ??
            (outcome => {
              if (outcome.route) window.location.assign(outcome.route);
            })
          }
        />

        {/* B1-08 运行状态条：仅在收到 v2 `run_started` 时渲染（旧后端不产生 → 无变化）。 */}
        <RunStatusBar
          info={
            message.run
              ? {
                  ...message.run,
                  providerLabel: message.providerInfo?.provider
                    ? providerLabel(message.providerInfo.provider)
                    : undefined,
                }
              : undefined
          }
        />

        {/* 过程证据（B1-08）：有 v2 步骤时用证据面板（含目标/依据），否则回退 M1-04 时间线。
            pending 条目由上方卡片承载，两者都不重复展示。 */}
        {message.steps && message.steps.length > 0 ? (
          <EvidencePanel
            steps={message.steps}
            toolEvents={message.toolEvents}
            detailsByInvocation={detailsById}
          />
        ) : (
          <ToolCallTimeline events={message.toolEvents} hideStatuses={PENDING_HIDDEN_STATUSES} />
        )}

        {/* B1-07：对话内确认抽屉（仅注入 onRequestConfirm 时可用；决策走 B1-05 状态机）。 */}
        {onRequestConfirm ? (
          <ConfirmationDrawer
            open={confirmTarget !== null}
            detail={confirmTarget}
            errorMessage={confirmError}
            onClose={() => {
              setConfirmTarget(null);
              setConfirmError(undefined);
            }}
            onDecision={async (approve, reason) => {
              if (!confirmTarget) return;
              try {
                await aiApproveTool(confirmTarget.id, { approve, reason: reason || undefined });
                trackAssistantEvent(approve ? 'confirmation_accepted' : 'confirmation_rejected', {
                  pageKey: 'ai.chat',
                  extra: { invocationId: confirmTarget.id },
                });
                setConfirmTarget(null);
                setConfirmError(undefined);
                onConfirmed?.();
              } catch (err) {
                // 过期/冲突/队列不可用等：错误语义由后端给出，抽屉内可见地失败并保留上下文。
                setConfirmError((err as Error)?.message || '确认操作失败，请稍后重试');
              }
            }}
          />
        ) : null}

        {message.sources && message.sources.length > 0 ? (
          <SourceList sources={message.sources} />
        ) : null}

        {!message.streaming && message.content ? (
          <div
            className="ai-chat-actions"
            style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 8, flexWrap: 'wrap' }}
          >
            <Button
              type="text"
              size="small"
              icon={copied ? <Check size={12} /> : <Copy size={12} />}
              onClick={handleCopyAnswer}
              style={{ height: 'auto', padding: 0, fontSize: 12, color: token.colorTextSecondary }}
            >
              {copied ? '已复制' : '复制回答'}
            </Button>
            {/* T5-06a：回答级反馈（点踩率）；反馈后固化，避免重复计数。 */}
            <Button
              type="text"
              size="small"
              icon={<ThumbsUp size={12} />}
              onClick={() => handleAnswerFeedback('helpful')}
              disabled={answerFeedback !== null}
              data-testid="answer-feedback-helpful"
              style={{ height: 'auto', padding: 0, fontSize: 12, color: token.colorTextSecondary }}
            >
              有用
            </Button>
            <Button
              type="text"
              size="small"
              icon={<ThumbsDown size={12} />}
              onClick={() => handleAnswerFeedback('unhelpful')}
              disabled={answerFeedback !== null}
              data-testid="answer-feedback-unhelpful"
              style={{ height: 'auto', padding: 0, fontSize: 12, color: token.colorTextSecondary }}
            >
              没用
            </Button>
            {answerFeedback ? (
              <Text type="secondary" style={{ fontSize: 12 }} data-testid="answer-feedback-thanks">
                已反馈
              </Text>
            ) : null}
            {message.providerInfo?.provider ? (
              <Tooltip title={`生效来源：${providerSourceLabel}`}>
                <span style={{ fontSize: 12, color: token.colorTextSecondary }}>
                  由 {providerLabel(message.providerInfo.provider)} 回答
                </span>
              </Tooltip>
            ) : null}
            {canPromoteToArticle(message) && canPromoteKnowledge ? (
              <Tooltip title="未引用知识库文章，可沉淀为知识">
                <Button
                  type="text"
                  size="small"
                  icon={<FileText size={12} />}
                  onClick={() => onCreateArticle(message)}
                  style={{ height: 'auto', padding: 0, fontSize: 12, color: token.colorTextSecondary }}
                >
                  补充为知识文章
                </Button>
              </Tooltip>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
};

// objectType → Tag 颜色映射（业务实体类型着色，未识别类型回退蓝色）
const OBJECT_TYPE_TAG_COLORS: Record<string, string> = {
  ticket: 'orange',
  incident: 'red',
  problem: 'purple',
  change: 'geekblue',
  release: 'cyan',
  ci: 'magenta',
  kb: 'blue',
  knowledge: 'blue',
};

const objectTypeTagColor = (objectType?: string): string =>
  (objectType && OBJECT_TYPE_TAG_COLORS[objectType.toLowerCase()]) || 'blue';

/** 助手消息内的引用来源（objectType Tag + 标题 + 相关度 + 可展开片段）。 */
const SourceList: React.FC<{ sources: RagAnswer[] }> = ({ sources }) => {
  const { token } = theme.useToken();
  return (
    <div
      style={{
        marginTop: 10,
        padding: '8px 12px',
        borderRadius: 10,
        background: token.colorFillQuaternary,
        border: `1px solid ${token.colorBorderSecondary}`,
      }}
    >
      <Text type="secondary" style={{ fontSize: 12 }}>
        引用来源 · {sources.length}
      </Text>
      <ul style={{ paddingLeft: 18, marginTop: 6, marginBottom: 0 }}>
        {sources.map((s, idx) => {
          const title = s.title || s.snippet?.slice(0, 30) || `来源 ${idx + 1}`;
          const scoreLabel =
            typeof s.score === 'number' ? `${(s.score * 100).toFixed(0)}%` : undefined;
          return (
            <li key={`${s.objectType}-${s.id}-${idx}`} style={{ marginBottom: 6 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                <Tag color={objectTypeTagColor(s.objectType)} style={{ marginInlineEnd: 0 }}>
                  {s.objectType || 'source'}
                </Tag>
                <Text strong style={{ fontSize: 13 }}>
                  {title}
                </Text>
                {scoreLabel ? (
                  <Tag color="green" style={{ marginInlineEnd: 0 }}>
                    相关度 {scoreLabel}
                  </Tag>
                ) : null}
              </div>
              {s.snippet ? (
                <Typography.Paragraph
                  type="secondary"
                  style={{ marginTop: 4, marginBottom: 0, fontSize: 12 }}
                  ellipsis={{ rows: 2, expandable: true, symbol: '展开' }}
                >
                  {s.snippet}
                </Typography.Paragraph>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
};
