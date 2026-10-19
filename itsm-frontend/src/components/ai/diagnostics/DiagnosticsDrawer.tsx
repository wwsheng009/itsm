/**
 * O2-02 面板内诊断抽屉（方案 §13.4 用户侧"诊断抽屉"）。
 *
 * 内容：
 * - 会话信息（会话 ID / 消息数 / 生成状态）与最近一次运行的本地快照（v2 事件，零请求可见）；
 * - 「复制轨迹 ID」/「复制会话 ID」：报障可导出的稳定引用（埋点 handoff(source=diagnostics_copy)）；
 * - 深度轨迹：按 `runId` 拉取 O1-01 的 `GET /ai/runs/:id/trace` 并复用 `RunTraceView` 渲染。
 *
 * 诚实边界：token 两种口径显式区分——`estimated=true` 为字符/4 估算（与后端限流同源）；
 * `estimated=false` 为 provider 回包实测（B4-02，流式末帧 usage）。两者均非账单口径。
 */
import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Descriptions, Drawer, Space, Tag, Typography, theme } from 'antd';
import { Check, Copy, Timer } from 'lucide-react';

import {
  aiGetConversationTrace,
  aiGetRunTrace,
  type AIConversationTrace,
  type AIRunTrace,
} from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import ConversationTraceView from '@/components/ai/run-trace/ConversationTraceView';
import RunTraceView from '@/components/ai/run-trace/RunTraceView';
import type { ChatMessage } from '@/components/ai/chat-core/types';
import {
  buildDiagnosticsSummary,
  diagnosticsReference,
  formatStepDuration,
  formatTokenUsage,
} from './diagnostics-model';

const { Text } = Typography;

export const DIAGNOSTICS_DRAWER_TESTID = 'assistant-diagnostics-drawer';

interface DiagnosticsDrawerProps {
  open: boolean;
  onClose: () => void;
  messages?: ChatMessage[];
  conversationId?: number;
  streaming?: boolean;
  /** 埋点用页面键（缺省不落字段）。 */
  pageKey?: string;
}

const RUN_STATUS_META: Record<string, { color: string; label: string }> = {
  running: { color: 'processing', label: '运行中' },
  done: { color: 'success', label: '已完成' },
  failed: { color: 'error', label: '已中断' },
};

const DiagnosticsDrawer: React.FC<DiagnosticsDrawerProps> = ({
  open,
  onClose,
  messages,
  conversationId,
  streaming,
  pageKey,
}) => {
  const { token } = theme.useToken();
  const summary = useMemo(() => buildDiagnosticsSummary(messages), [messages]);
  const runId = summary.latestRun?.runId;
  const reference = diagnosticsReference(summary, conversationId);
  const [copied, setCopied] = useState<string | null>(null);
  const [trace, setTrace] = useState<AIRunTrace | null>(null);
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceError, setTraceError] = useState(false);
  const [conversationTrace, setConversationTrace] = useState<AIConversationTrace | null>(null);
  const [conversationTraceLoading, setConversationTraceLoading] = useState(false);
  const [conversationTraceError, setConversationTraceError] = useState(false);

  // 深度轨迹：run 维度优先（runId）；关闭即释放。
  useEffect(() => {
    if (!open) {
      setTrace(null);
      setTraceError(false);
      setConversationTrace(null);
      setConversationTraceError(false);
      return undefined;
    }
    if (!runId) {
      setTrace(null);
      setTraceError(false);
      return undefined;
    }
    let alive = true;
    setTraceLoading(true);
    setTraceError(false);
    aiGetRunTrace(runId)
      .then(result => {
        if (alive) setTrace(result);
      })
      .catch(() => {
        if (alive) {
          setTrace(null);
          setTraceError(true);
        }
      })
      .finally(() => {
        if (alive) setTraceLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [open, runId]);

  // O1-01 条件项：纯问答（无 run）回退会话维度轨迹，按需请求（缺会话 ID 不发请求）。
  useEffect(() => {
    if (!open || runId || !conversationId) return undefined;
    let alive = true;
    setConversationTraceLoading(true);
    setConversationTraceError(false);
    aiGetConversationTrace(conversationId)
      .then(result => {
        if (alive) setConversationTrace(result);
      })
      .catch(() => {
        if (alive) {
          setConversationTrace(null);
          setConversationTraceError(true);
        }
      })
      .finally(() => {
        if (alive) setConversationTraceLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [open, runId, conversationId]);

  const handleCopy = (value: string, kind: 'run' | 'conversation') => {
    // 报障链路埋点在先；剪贴板不可用（隐私模式/非安全上下文）静默降级。
    trackAssistantEvent('handoff', {
      pageKey,
      source: 'diagnostics_copy',
      runId: runId ? String(runId) : undefined,
      extra: { reference: kind },
    });
    void (async () => {
      try {
        await navigator.clipboard?.writeText(value);
      } catch {
        /* 静默降级 */
      }
    })();
    setCopied(value);
    window.setTimeout(() => setCopied(current => (current === value ? null : current)), 2000);
  };

  const runMeta = summary.latestRun ? RUN_STATUS_META[summary.latestRun.status] : undefined;

  return (
    <Drawer title="会话诊断" placement="right" width={520} open={open} onClose={onClose} destroyOnClose>
      <div
        data-testid={DIAGNOSTICS_DRAWER_TESTID}
        style={{ display: 'flex', flexDirection: 'column', gap: 12 }}
      >
        <Descriptions size="small" column={1} data-testid="diagnostics-summary">
          <Descriptions.Item label="会话">{conversationId ? `#${conversationId}` : '—'}</Descriptions.Item>
          <Descriptions.Item label="消息数">
            {summary.messageCount}（助手 {summary.assistantMessageCount}）
          </Descriptions.Item>
          <Descriptions.Item label="状态">{streaming ? '生成中' : '空闲'}</Descriptions.Item>
          <Descriptions.Item label="最近轨迹">
            {runId ? `run#${runId}` : '无运行记录（纯问答）'}
          </Descriptions.Item>
        </Descriptions>

        <Space wrap>
          <Button
            size="small"
            data-testid="diagnostics-copy-run"
            disabled={!runId}
            icon={copied && runId !== undefined && copied === String(runId) ? <Check size={14} /> : <Copy size={14} />}
            onClick={() => runId && handleCopy(String(runId), 'run')}
          >
            复制轨迹 ID
          </Button>
          <Button
            size="small"
            data-testid="diagnostics-copy-conversation"
            disabled={!conversationId}
            onClick={() => conversationId && handleCopy(String(conversationId), 'conversation')}
          >
            复制会话 ID
          </Button>
          {reference ? (
            <Text type="secondary" code style={{ fontSize: 12 }}>
              {reference}
            </Text>
          ) : null}
        </Space>

        {/* 本地运行快照（v2 事件，无需请求）。 */}
        {summary.latestRun ? (
          <div
            data-testid="diagnostics-run-summary"
            style={{
              padding: '8px 10px',
              borderRadius: 8,
              border: `1px solid ${token.colorBorderSecondary}`,
              background: token.colorFillQuaternary,
            }}
          >
            <Space wrap size={8} style={{ fontSize: 12 }}>
              <Tag color={runMeta?.color}>{runMeta?.label ?? summary.latestRun.status}</Tag>
              <Text type="secondary">步骤 {summary.latestRun.stepCount ?? summary.steps.length}</Text>
              {typeof summary.latestRun.lastDurationMs === 'number' ? (
                <Text type="secondary">
                  <Timer size={12} style={{ verticalAlign: -2, marginRight: 2 }} />
                  最近 {formatStepDuration(summary.latestRun.lastDurationMs)}
                </Text>
              ) : null}
              {summary.latestRun.providerLabel ? (
                <Text type="secondary">{summary.latestRun.providerLabel}</Text>
              ) : null}
              {summary.latestRun.budget &&
              (summary.latestRun.budget.stepsLimit || summary.latestRun.budget.toolCallsLimit) ? (
                <Text type="secondary">
                  预算：步数 {summary.latestRun.budget.stepsUsed ?? 0}/
                  {summary.latestRun.budget.stepsLimit ?? '—'} · 工具{' '}
                  {summary.latestRun.budget.toolCallsUsed ?? 0}/
                  {summary.latestRun.budget.toolCallsLimit ?? '—'}
                </Text>
              ) : null}
            </Space>
          </div>
        ) : null}

        {/* O2-02/§13.4：回合 token 估算（v2 `usage` 事件随 done 回带；纯问答同样可见）。 */}
        {summary.latestUsage ? (
          <div
            data-testid="diagnostics-usage"
            style={{
              padding: '8px 10px',
              borderRadius: 8,
              border: `1px solid ${token.colorBorderSecondary}`,
              background: token.colorFillQuaternary,
            }}
          >
            <Space wrap size={8} style={{ fontSize: 12 }}>
              <Tag color="default">
                {summary.latestUsage.estimated === false ? '用量（provider 实测）' : '用量（估算）'}
              </Tag>
              <Text type="secondary">{formatTokenUsage(summary.latestUsage)}</Text>
            </Space>
            <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 4 }}>
              {summary.latestUsage.estimated === false
                ? '口径：provider 回包实测（prompt/completion 原值）；非账单口径（缓存/推理细分为未映射）。'
                : '口径：字符数 ÷ 4（含系统提示与历史上下文），与限流同源；provider 未回包 usage 时保持估算。'}
            </Text>
          </div>
        ) : null}

        {summary.steps.length > 0 ? (
          <div data-testid="diagnostics-steps" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            {summary.steps.map(step => (
              <div key={`${step.runId}-${step.stepIndex}`}>
                <Tag style={{ marginInlineEnd: 6 }}>#{step.stepIndex}</Tag>
                <Text style={{ fontSize: 12 }}>{step.type}</Text>
                <Text type="secondary" style={{ fontSize: 12, marginInlineStart: 8 }}>
                  {formatStepDuration(step.durationMs)}
                </Text>
                {step.payloadRef ? (
                  <Text code style={{ fontSize: 12, marginInlineStart: 8 }}>
                    {step.payloadRef}
                  </Text>
                ) : null}
              </div>
            ))}
          </div>
        ) : null}

        {/* 深度轨迹（O1-01）：run 维度优先；纯问答回退会话维度（条件项闭环）。 */}
        {runId ? (
          traceLoading ? (
            <Text type="secondary" data-testid="diagnostics-trace-loading">
              轨迹加载中…
            </Text>
          ) : traceError ? (
            <Alert
              type="warning"
              showIcon
              data-testid="diagnostics-trace-error"
              message="轨迹加载失败（可复制轨迹 ID 报障）"
            />
          ) : trace ? (
            <RunTraceView trace={trace} pageKey={pageKey} />
          ) : null
        ) : conversationId ? (
          conversationTraceLoading ? (
            <Text type="secondary" data-testid="diagnostics-conversation-trace-loading">
              会话轨迹加载中…
            </Text>
          ) : conversationTraceError ? (
            <Alert
              type="warning"
              showIcon
              data-testid="diagnostics-conversation-trace-error"
              message="会话轨迹加载失败（可复制会话 ID 报障）"
            />
          ) : conversationTrace ? (
            <ConversationTraceView trace={conversationTrace} pageKey={pageKey} />
          ) : null
        ) : (
          <Alert
            type="info"
            showIcon
            data-testid="diagnostics-no-run"
            message="本次会话没有可回放的轨迹"
            description="没有会话 ID 或运行记录；可复制可用 ID 报障或稍后重试。"
          />
        )}

        <Text type="secondary" style={{ fontSize: 12, color: token.colorTextTertiary }}>
          {summary.latestUsage
            ? summary.latestUsage.estimated === false
              ? 'token 为 provider 回包实测（B4-02）；可查轨迹、步骤与耗时。'
              : 'token 为估算口径（provider 未回包 usage 时）：可查轨迹、步骤与耗时。'
            : '本会话没有可计量的回合用量（provider 未回包且无估算）：可查轨迹、步骤与耗时。'}
        </Text>
      </div>
    </Drawer>
  );
};

export default DiagnosticsDrawer;