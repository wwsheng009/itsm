/**
 * O2-02 面板内诊断抽屉纯逻辑：从会话消息中提取「最近一次运行」摘要。
 *
 * 数据来源（全部本地已到事件，零额外请求即可见）：
 * - `ChatMessage.run`：v2 运行快照（runId/状态/步骤数/最近耗时/预算）；
 * - `ChatMessage.steps`：v2 步骤（stepIndex/type/durationMs/payloadRef）。
 *
 * 深度轨迹（steps/events/tools/artifacts）经 `GET /ai/runs/:id/trace` 按需加载（O1-01）。
 */
import type { AIRunStepEvent, AIUsageEstimate } from '@/lib/api/ai-api';
import type { ChatMessage } from '@/components/ai/chat-core/types';
import type { RunSnapshot } from '@/components/ai/run-status-bar';

export interface DiagnosticsSummary {
  messageCount: number;
  assistantMessageCount: number;
  /** 最近一次运行的快照（最后一条带 run 的助手消息；无 → undefined）。 */
  latestRun?: RunSnapshot;
  /** 该消息的 v2 步骤（按 stepIndex 升序）。 */
  steps: AIRunStepEvent[];
  /** 最近一次运行所属消息 id（用于定位来源）。 */
  runMessageId?: string;
  /**
   * O2-02/§13.4：最近一次带用量估算的助手消息（v2 `usage` 事件，独立于运行档案——
   * 纯问答没有 bot_run 但有回合用量）。
   */
  latestUsage?: AIUsageEstimate;
  /** 该用量所属消息 id。 */
  usageMessageId?: string;
}

/** 提取诊断摘要（纯函数，便于单测）。 */
export function buildDiagnosticsSummary(messages: readonly ChatMessage[] | undefined): DiagnosticsSummary {
  const list = messages ?? [];
  const summary: DiagnosticsSummary = {
    messageCount: list.length,
    assistantMessageCount: list.filter(item => item.role === 'assistant').length,
    steps: [],
  };

  for (let index = list.length - 1; index >= 0; index -= 1) {
    const message = list[index];
    if (message.role !== 'assistant' || !message.run) continue;
    summary.latestRun = message.run;
    summary.runMessageId = message.id;
    summary.steps = [...(message.steps ?? [])].sort((a, b) => a.stepIndex - b.stepIndex);
    break;
  }

  // 用量估算独立扫描：纯问答（无 run）同样应有回合用量。
  for (let index = list.length - 1; index >= 0; index -= 1) {
    const message = list[index];
    if (message.role !== 'assistant' || !message.usage) continue;
    summary.latestUsage = message.usage;
    summary.usageMessageId = message.id;
    break;
  }
  return summary;
}

/**
 * 用量文案：估算口径恒带「≈」；provider 实测（estimated=false，B4-02）用精确口径并显式标注。
 * 展示契约：诊断抽屉与测试共用同一文案，避免两处漂移。
 */
export function formatTokenUsage(usage?: AIUsageEstimate): string {
  if (!usage) return '—';
  const total = usage.tokenIn + usage.tokenOut;
  if (usage.estimated === false) {
    return `输入 ${usage.tokenIn} · 输出 ${usage.tokenOut} · 合计 ${total}（provider 实测）`;
  }
  return `输入 ≈ ${usage.tokenIn} · 输出 ≈ ${usage.tokenOut} · 合计 ≈ ${total}`;
}

/** 报障文案：优先轨迹 ID，其次会话 ID（两者皆无 → 空串，调用方禁用按钮）。 */
export function diagnosticsReference(summary: DiagnosticsSummary, conversationId?: number): string {
  if (summary.latestRun?.runId) return `run#${summary.latestRun.runId}`;
  if (conversationId) return `conversation#${conversationId}`;
  return '';
}

/** 步骤耗时格式化（毫秒 → ms/s；缺省 → '—'）。 */
export function formatStepDuration(durationMs?: number): string {
  if (typeof durationMs !== 'number' || !Number.isFinite(durationMs) || durationMs < 0) return '—';
  if (durationMs < 1000) return `${Math.round(durationMs)} ms`;
  return `${(durationMs / 1000).toFixed(1)} s`;
}