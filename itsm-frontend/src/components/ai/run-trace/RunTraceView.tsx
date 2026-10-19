/**
 * O1-01 轨迹回放视图（管理侧；O2-02 面板诊断抽屉复用同一组件）。
 *
 * 链路六段（方案 §13.4）：会话 → 运行 → 步骤/事件 → 工具调用与确认 → 产物 → 业务对象。
 * 只读展示：不提供任何写操作；原始工具参数不回显（仅后端已脱敏的 `argsRedacted`）。
 */
import React, { useState } from 'react';
import { Button, Card, Descriptions, Empty, List, Space, Tag, Typography, theme } from 'antd';
import { Check, Copy } from 'lucide-react';

import type { AIRunTrace } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useI18n } from '@/lib/i18n/useI18n';
import {
  collectTraceTargets,
  confirmationColor,
  confirmationLabel,
  formatDuration,
  formatTraceTime,
  runDurationMs,
  runStatusColor,
  summarizePayload,
} from './trace-model';

const { Text, Title } = Typography;

export const RUN_TRACE_VIEW_TESTID = 'run-trace-view';

interface RunTraceViewProps {
  trace: AIRunTrace;
  /** 埋点用页面键（缺省不落字段）。 */
  pageKey?: string;
}

const RunTraceView: React.FC<RunTraceViewProps> = ({ trace, pageKey }) => {
  const { t } = useI18n();
  const { token } = theme.useToken();
  const [copied, setCopied] = useState(false);
  const run = trace.run;
  const targets = collectTraceTargets(trace);
  const duration = formatDuration(runDurationMs(run));

  const handleCopyRunId = () => {
    // O2-02 前置：报障可复制的轨迹 ID（埋点先行，剪贴板失败静默降级）。
    trackAssistantEvent('handoff', {
      pageKey,
      source: 'trace_copy',
      runId: String(run?.id ?? ''),
    });
    void (async () => {
      try {
        await navigator.clipboard?.writeText(String(run?.id ?? ''));
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
    <div data-testid={RUN_TRACE_VIEW_TESTID}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Space wrap align="center">
          <Title level={5} style={{ margin: 0 }}>
            {t('aiObservability.traceRunLabel')} #{run?.id}
          </Title>
          <Tag color={runStatusColor(run?.status)} data-testid="run-trace-status">
            {run?.status || 'unknown'}
          </Tag>
          {run?.errorCode ? <Tag color="red">{run.errorCode}</Tag> : null}
          <Button
            size="small"
            icon={copied ? <Check size={14} /> : <Copy size={14} />}
            onClick={handleCopyRunId}
            data-testid="run-trace-copy"
          >
            {copied ? t('aiObservability.traceCopied') : t('aiObservability.traceCopyId')}
          </Button>
        </Space>

        {/* 会话 → 运行 */}
        <Card size="small" title={t('aiObservability.traceConversation')} data-testid="run-trace-conversation">
          <Text>
            {t('aiObservability.traceConversation')} #{run?.conversationId ?? 0}
          </Text>
          {run?.botId ? (
            <Text type="secondary" style={{ marginInlineStart: 12 }}>
              Bot #{run.botId}
            </Text>
          ) : null}
        </Card>

        <Card size="small" title={t('aiObservability.traceRun')} data-testid="run-trace-run">
          <Descriptions size="small" column={2}>
            <Descriptions.Item label={t('aiObservability.traceEntrypoint')}>
              {run?.entrypoint || '—'}
            </Descriptions.Item>
            <Descriptions.Item label={t('aiObservability.traceModel')}>{run?.model || '—'}</Descriptions.Item>
            <Descriptions.Item label={t('aiObservability.traceDuration')}>{duration}</Descriptions.Item>
            <Descriptions.Item label={t('aiObservability.traceStartedAt')}>
              {formatTraceTime(run?.startedAt)}
            </Descriptions.Item>
            <Descriptions.Item label={t('aiObservability.traceFinishedAt')}>
              {formatTraceTime(run?.finishedAt)}
            </Descriptions.Item>
            <Descriptions.Item label={t('aiObservability.traceErrorCode')}>
              {run?.errorCode || '—'}
            </Descriptions.Item>
          </Descriptions>
        </Card>

        {/* 步骤与事件 */}
        <Card
          size="small"
          title={t('aiObservability.traceStepsEvents')}
          data-testid="run-trace-steps"
        >
          {trace.steps.length === 0 && trace.events.length === 0 ? (
            sectionEmpty(t('aiObservability.traceEmptySection'))
          ) : (
            <Space direction="vertical" size="small" style={{ width: '100%' }}>
              {trace.steps.map(step => (
                <div key={`step-${step.id}`} data-testid={`run-trace-step-${step.stepIndex}`}>
                  <Tag color="blue">{step.type}</Tag>
                  <Text>#{step.stepIndex}</Text>
                  <Text type="secondary" style={{ marginInlineStart: 8 }}>
                    {formatDuration(step.durationMs)}
                  </Text>
                  {step.payloadRef ? (
                    <Text code style={{ marginInlineStart: 8 }}>
                      {step.payloadRef}
                    </Text>
                  ) : null}
                </div>
              ))}
              {trace.events.map(event => (
                <div key={`event-${event.id}`} data-testid={`run-trace-event-${event.seq}`}>
                  <Tag>{event.type}</Tag>
                  <Text type="secondary">
                    seq {event.seq} · {summarizePayload(event.payload)}
                  </Text>
                </div>
              ))}
            </Space>
          )}
        </Card>

        {/* 工具调用与确认 */}
        <Card size="small" title={t('aiObservability.traceTools')} data-testid="run-trace-tools">
          {trace.toolInvocations.length === 0 ? (
            sectionEmpty(t('aiObservability.traceEmptySection'))
          ) : (
            <List
              size="small"
              dataSource={trace.toolInvocations}
              renderItem={tool => (
                <List.Item key={tool.id} data-testid={`run-trace-tool-${tool.id}`}>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space wrap>
                      <Text strong>{tool.toolName}</Text>
                      <Tag data-testid={`run-trace-tool-${tool.id}-confirmation`} color={confirmationColor(tool.confirmationState)}>
                        {confirmationLabel(tool.confirmationState)}
                      </Tag>
                      {tool.risk ? <Tag>{tool.risk}</Tag> : null}
                      {tool.verifyState ? <Tag color="purple">verify:{tool.verifyState}</Tag> : null}
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
        <Card size="small" title={t('aiObservability.traceArtifacts')} data-testid="run-trace-artifacts">
          {trace.artifacts.length === 0 ? (
            sectionEmpty(t('aiObservability.traceEmptySection'))
          ) : (
            <List
              size="small"
              dataSource={trace.artifacts}
              renderItem={artifact => (
                <List.Item key={artifact.id} data-testid={`run-trace-artifact-${artifact.id}`}>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space wrap>
                      <Text code>artifact:{artifact.id}</Text>
                      <Tag color="cyan">{artifact.kind}</Tag>
                      {artifact.title ? <Text strong>{artifact.title}</Text> : null}
                      {artifact.toolName ? <Text type="secondary">{artifact.toolName}</Text> : null}
                    </Space>
                    {artifact.contentJson ? (
                      <Text type="secondary" style={{ fontSize: 12, wordBreak: 'break-all' }}>
                        {artifact.contentJson.length > 200
                          ? `${artifact.contentJson.slice(0, 200)}…`
                          : artifact.contentJson}
                      </Text>
                    ) : null}
                  </Space>
                </List.Item>
              )}
            />
          )}
        </Card>

        {/* 业务对象（Outcome Contract 引用） */}
        <Card size="small" title={t('aiObservability.traceTargets')} data-testid="run-trace-targets">
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
          {t('aiObservability.traceDescription')}
        </Text>
      </Space>
    </div>
  );
};

export default RunTraceView;