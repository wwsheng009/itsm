/**
 * O1-01 条件项：会话维度轨迹回放（纯问答无 run 同样可回放）。
 *
 * 链路：会话档案 → 消息回合 →（若有）运行列表 → 工具调用与确认 → 产物。
 * 只读展示：原始工具参数不回显（仅后端已脱敏的 `argsRedacted`）；消息截断
 * （单条超长 / 窗口超限）显式标注，不冒充全量。
 */
import React, { useState } from 'react';
import { Button, Card, Descriptions, Empty, List, Space, Tag, Typography, theme } from 'antd';
import { Check, Copy } from 'lucide-react';

import type { AIConversationTrace } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useI18n } from '@/lib/i18n/useI18n';
import {
  confirmationColor,
  confirmationLabel,
  formatDuration,
  formatTraceTime,
  runStatusColor,
  targetRoute,
} from './trace-model';

const { Text, Title } = Typography;

export const CONVERSATION_TRACE_VIEW_TESTID = 'conversation-trace-view';

interface ConversationTraceViewProps {
  trace: AIConversationTrace;
  /** 埋点用页面键（缺省不落字段）。 */
  pageKey?: string;
}

const ROLE_COLORS: Record<string, string> = {
  user: 'blue',
  assistant: 'green',
  system: 'purple',
  tool: 'orange',
};

const ConversationTraceView: React.FC<ConversationTraceViewProps> = ({ trace, pageKey }) => {
  const { t } = useI18n();
  const { token } = theme.useToken();
  const [copied, setCopied] = useState(false);
  const conversation = trace.conversation;
  const targets = React.useMemo(() => {
    const refs: { key: string; type: string; id: string; route?: string }[] = [];
    const seen = new Set<string>();
    for (const tool of trace.toolInvocations ?? []) {
      if (!tool.targetType || !tool.targetId) continue;
      const key = `${tool.targetType}#${tool.targetId}`;
      if (seen.has(key)) continue;
      seen.add(key);
      refs.push({ key, type: tool.targetType, id: tool.targetId, route: targetRoute(tool.targetType, tool.targetId) });
    }
    return refs;
  }, [trace.toolInvocations]);

  const handleCopyConversationId = () => {
    // 报障可复制的会话 ID（埋点先行，剪贴板失败静默降级）。
    trackAssistantEvent('handoff', {
      pageKey,
      source: 'trace_copy',
      extra: { reference: 'conversation' },
    });
    void (async () => {
      try {
        await navigator.clipboard?.writeText(String(conversation?.id ?? ''));
      } catch {
        /* 剪贴板不可用（隐私模式/非安全上下文）：静默降级，仍提示已复制以保持流程 */
      }
    })();
    setCopied(true);
    window.setTimeout(() => setCopied(false), 2000);
  };

  const sectionEmpty = (description: string) => (
    <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={description} />
  );

  return (
    <div data-testid={CONVERSATION_TRACE_VIEW_TESTID}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Space wrap align="center">
          <Title level={5} style={{ margin: 0 }}>
            {t('aiObservability.conversationTraceTitle')} #{conversation?.id}
          </Title>
          {conversation?.title ? <Tag>{conversation.title}</Tag> : null}
          <Button
            size="small"
            icon={copied ? <Check size={14} /> : <Copy size={14} />}
            onClick={handleCopyConversationId}
            data-testid="conversation-trace-copy"
          >
            {copied ? t('aiObservability.traceCopied') : t('aiObservability.conversationTraceCopyId')}
          </Button>
        </Space>

        {/* 会话档案 */}
        <Card size="small" title={t('aiObservability.traceConversation')} data-testid="conversation-trace-meta">
          <Descriptions size="small" column={2}>
            <Descriptions.Item label={t('aiObservability.traceConversation')}>
              #{conversation?.id}
            </Descriptions.Item>
            <Descriptions.Item label={t('aiObservability.conversationTraceMessageCount', { count: trace.messageCount })}>
              {trace.messages.length}
            </Descriptions.Item>
            <Descriptions.Item label={t('aiObservability.traceStartedAt')}>
              {formatTraceTime(conversation?.createdAt)}
            </Descriptions.Item>
            <Descriptions.Item label="Bot">{conversation?.botId ? `#${conversation.botId}` : '—'}</Descriptions.Item>
          </Descriptions>
        </Card>

        {/* 消息回合（纯问答主体） */}
        <Card
          size="small"
          title={t('aiObservability.conversationTraceMessages')}
          data-testid="conversation-trace-messages"
        >
          {trace.messages.length === 0 ? (
            sectionEmpty(t('aiObservability.conversationTraceEmptyMessages'))
          ) : (
            <Space direction="vertical" size="small" style={{ width: '100%' }}>
              {trace.messages.map(message => (
                <div key={`message-${message.id}`} data-testid={`conversation-trace-message-${message.id}`}>
                  <Space wrap size={6}>
                    <Tag color={ROLE_COLORS[message.role] ?? 'default'}>{message.role}</Tag>
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {formatTraceTime(message.createdAt)}
                    </Text>
                    {message.truncated ? (
                      <Tag color="orange" data-testid={`conversation-trace-message-${message.id}-truncated`}>
                        {t('aiObservability.conversationTraceTruncated')}
                      </Tag>
                    ) : null}
                  </Space>
                  <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{message.content}</div>
                </div>
              ))}
            </Space>
          )}
          {trace.messagesTruncated ? (
            <Text type="secondary" data-testid="conversation-trace-messages-truncated">
              {t('aiObservability.conversationTraceWindowTruncated', {
                shown: trace.messages.length,
                total: trace.messageCount,
              })}
            </Text>
          ) : null}
        </Card>

        {/* 运行记录（若有）：跳 run 维度轨迹 */}
        <Card size="small" title={t('aiObservability.conversationTraceRuns')} data-testid="conversation-trace-runs">
          {trace.runs.length === 0 ? (
            <Text type="secondary" data-testid="conversation-trace-pure-qa">
              {t('aiObservability.conversationTracePureQA')}
            </Text>
          ) : (
            <List
              size="small"
              dataSource={trace.runs}
              renderItem={run => (
                <List.Item key={run.id} data-testid={`conversation-trace-run-${run.id}`}>
                  <Space wrap>
                    <a href={`/ai/observability?runId=${run.id}`}>{t('aiObservability.traceRun')} #{run.id}</a>
                    <Tag color={runStatusColor(run.status)}>{run.status}</Tag>
                    {run.entrypoint ? <Text type="secondary">{run.entrypoint}</Text> : null}
                    {run.errorCode ? <Tag color="red">{run.errorCode}</Tag> : null}
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {formatTraceTime(run.startedAt)} → {formatTraceTime(run.finishedAt)}
                    </Text>
                  </Space>
                </List.Item>
              )}
            />
          )}
        </Card>

        {/* 工具调用与确认 */}
        <Card size="small" title={t('aiObservability.traceTools')} data-testid="conversation-trace-tools">
          {trace.toolInvocations.length === 0 ? (
            sectionEmpty(t('aiObservability.traceEmptySection'))
          ) : (
            <List
              size="small"
              dataSource={trace.toolInvocations}
              renderItem={tool => (
                <List.Item key={tool.id} data-testid={`conversation-trace-tool-${tool.id}`}>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space wrap>
                      <Text strong>{tool.toolName}</Text>
                      <Tag
                        data-testid={`conversation-trace-tool-${tool.id}-confirmation`}
                        color={confirmationColor(tool.confirmationState)}
                      >
                        {confirmationLabel(tool.confirmationState)}
                      </Tag>
                      {tool.risk ? <Tag>{tool.risk}</Tag> : null}
                      <Text type="secondary">{formatDuration(tool.durationMs)}</Text>
                    </Space>
                    {tool.argsRedacted ? (
                      <Text code style={{ fontSize: 12 }}>
                        {tool.argsRedacted}
                      </Text>
                    ) : null}
                    {tool.outputSummary ? (
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        {tool.outputSummary}
                      </Text>
                    ) : null}
                    {tool.errorCode || tool.error ? (
                      <Text type="danger" style={{ fontSize: 12 }}>
                        {tool.errorCode ? `${tool.errorCode}: ` : ''}
                        {tool.error || ''}
                      </Text>
                    ) : null}
                  </Space>
                </List.Item>
              )}
            />
          )}
        </Card>

        {/* 产物 */}
        <Card size="small" title={t('aiObservability.traceArtifacts')} data-testid="conversation-trace-artifacts">
          {trace.artifacts.length === 0 ? (
            sectionEmpty(t('aiObservability.traceEmptySection'))
          ) : (
            <List
              size="small"
              dataSource={trace.artifacts}
              renderItem={artifact => (
                <List.Item key={artifact.id} data-testid={`conversation-trace-artifact-${artifact.id}`}>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space wrap>
                      <Text code>artifact:{artifact.id}</Text>
                      <Tag color="cyan">{artifact.kind}</Tag>
                      {artifact.title ? <Text strong>{artifact.title}</Text> : null}
                    </Space>
                  </Space>
                </List.Item>
              )}
            />
          )}
        </Card>

        {/* 业务对象（工具调用落点） */}
        <Card size="small" title={t('aiObservability.traceTargets')} data-testid="conversation-trace-targets">
          {targets.length === 0 ? (
            sectionEmpty(t('aiObservability.traceEmptySection'))
          ) : (
            <Space wrap>
              {targets.map(target =>
                target.route ? (
                  <a key={target.key} href={target.route}>
                    {target.type} #{target.id}
                  </a>
                ) : (
                  <Text key={target.key}>{`${target.type} #${target.id}`}</Text>
                )
              )}
            </Space>
          )}
        </Card>

        <Text type="secondary" style={{ fontSize: 12, color: token.colorTextTertiary }}>
          {t('aiObservability.conversationTraceDescription')}
        </Text>
      </Space>
    </div>
  );
};

export default ConversationTraceView;