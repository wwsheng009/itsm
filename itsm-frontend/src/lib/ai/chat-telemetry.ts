/**
 * D0-05（埋点子集）：把 AIChat 流式回调映射为观测事件（契约见方案 §13.2）。
 *
 * 设计动机：
 * - AIChat 是 1500+ 行的关键组件，埋点接线必须**极薄且可单测**——本模块承载全部
 *   状态与映射逻辑（一次性/顺序/去重），组件侧只做一行式调用；
 * - 隐私：不采集问题原文，只上报长度（queryLength）与 FNV-1a 哈希（queryHash）；错误详情走 extra.detail，
 *   由 SDK 按敏感 key 哈希（§13.1 元数据优先）；
 * - 失败静默：track 抛错不得影响对话（SDK 本身已静默，这里再兜一层）。
 */
import {
  hashTelemetryText,
  trackAssistantEvent,
  type AssistantEventInput,
  type AssistantEventName,
} from './telemetry';

export interface ChatTelemetryContext {
  pageKey?: string;
  route?: string;
  entrypoint?: string;
  botId?: number;
  conversationId?: number;
}

export interface ChatTelemetryToolEvent {
  tool: string;
  status?: string;
  durationMs?: number;
  errorCode?: string;
}

export interface ChatTelemetryDoneInfo {
  conversationId?: number;
  provider?: string;
  providerSource?: string;
  /** T5-06a：本回合生效模型（done 事件回带；静态回退缺省）。 */
  model?: string;
  /** T5-06a：本回合引用条数（缺省时用 onSources 累计值；从未收到 sources 视为 0）。 */
  sourcesCount?: number;
  /**
   * O2-02/§13.4：回合 token 用量（后端 `usage` v2 事件；estimated=false 时为 provider 实测，B4-02）。
   * 旧后端缺省 → stream_end 不落这两个字段（§13.2 字典：字段可选）。
   */
  tokenIn?: number;
  tokenOut?: number;
  /** 口径标记：true=字符/4 估算（缺省）；false=provider 回包实测（B4-02）。 */
  tokenEstimated?: boolean;
}

export type ChatTelemetryTrack = (name: AssistantEventName, input?: AssistantEventInput) => void;

export interface ChatTelemetrySink {
  /** 用户发送消息（一次会话回合开始）。 */
  onSend(query: string, conversationId?: number): void;
  /** 首个 delta 到达（重复调用只计一次）。 */
  onFirstToken(): void;
  /** RAG 来源事件（T5-06a：记录引用条数，随 stream_end 上报）。 */
  onSources(sourcesCount: number): void;
  /** 工具调用过程事件（started/done/failed/pending）。 */
  onToolEvent(event: ChatTelemetryToolEvent): void;
  /** 流正常结束（幂等；未发送时忽略）。 */
  onDone(info?: ChatTelemetryDoneInfo): void;
  /** 流失败/降级失败（幂等；详情经 SDK 脱敏）。 */
  onError(detail?: string): void;
  /** 对话内确认单（shown/accepted/rejected）。 */
  onConfirmation(kind: 'shown' | 'accepted' | 'rejected'): void;
}

/**
 * 创建一次聊天会话的埋点接收器。
 *
 * @param context 页面/入口上下文（pageKey、route、entrypoint、botId、conversationId）
 * @param track   事件上报函数（默认 SDK 单例；测试注入假实现）
 * @param now     时钟（默认 Date.now；测试注入假时钟）
 */
export function createChatTelemetry(
  context: ChatTelemetryContext = {},
  track: ChatTelemetryTrack = trackAssistantEvent,
  now: () => number = () => Date.now()
): ChatTelemetrySink {
  let startedAt: number | null = null;
  let firstTokenSeen = false;
  let queryHash: string | undefined;
  let sourcesCount: number | null = null;

  const emit = (name: AssistantEventName, input: AssistantEventInput): void => {
    try {
      track(name, input);
    } catch {
      // 埋点失败静默：绝不冒泡到对话链路。
    }
  };

  const base = (): AssistantEventInput => ({
    pageKey: context.pageKey,
    route: context.route,
    entrypoint: context.entrypoint,
  });

  const active = (): boolean => startedAt !== null;

  return {
    onSend(query, conversationId) {
      startedAt = now();
      firstTokenSeen = false;
      queryHash = hashTelemetryText(query);
      sourcesCount = null;
      emit('message_sent', {
        ...base(),
        conversationId: conversationId ?? context.conversationId,
        queryHash,
        extra: { queryLength: query.length, botId: context.botId },
      });
    },

    onFirstToken() {
      if (!active() || firstTokenSeen) return;
      firstTokenSeen = true;
      emit('first_token', { ...base(), latencyMs: now() - (startedAt as number) });
    },

    onSources(count) {
      if (!active()) return;
      sourcesCount = Number.isFinite(count) && count >= 0 ? count : null;
    },

    onToolEvent(event) {
      if (!active()) return;
      emit('tool_call', {
        ...base(),
        toolName: event.tool,
        reasonCode: event.status,
        latencyMs: event.durationMs,
        errorCode: event.errorCode,
      });
    },

    onDone(info) {
      if (!active()) return;
      const latency = now() - (startedAt as number);
      // T5-06a：正常结束的回合必须给出引用数（0 = 无引用回答），否则质量层分母失真。
      const finalSourcesCount = info?.sourcesCount ?? sourcesCount ?? 0;
      startedAt = null;
      emit('stream_end', {
        ...base(),
        conversationId: info?.conversationId ?? context.conversationId,
        model: info?.model,
        queryHash,
        sourcesCount: finalSourcesCount,
        hasSources: finalSourcesCount > 0,
        latencyMs: latency,
        providerSource: info?.providerSource,
        tokenIn: info?.tokenIn,
        tokenOut: info?.tokenOut,
        extra: {
          ...(info?.provider ? { provider: info.provider } : {}),
          // 口径标记：缺省按估算（true）；provider 实测（B4-02）显式落 false。
          ...(typeof info?.tokenIn === 'number' || typeof info?.tokenOut === 'number'
            ? { tokenEstimated: info?.tokenEstimated !== false }
            : {}),
        },
      });
      queryHash = undefined;
      sourcesCount = null;
    },

    onError(detail) {
      if (!active()) return;
      const latency = now() - (startedAt as number);
      startedAt = null;
      emit('error_shown', {
        ...base(),
        queryHash,
        ...(typeof sourcesCount === 'number'
          ? { sourcesCount, hasSources: sourcesCount > 0 }
          : {}),
        latencyMs: latency,
        extra: detail ? { detail } : undefined,
      });
      queryHash = undefined;
      sourcesCount = null;
    },

    onConfirmation(kind) {
      const name: AssistantEventName =
        kind === 'shown'
          ? 'confirmation_shown'
          : kind === 'accepted'
            ? 'confirmation_accepted'
            : 'confirmation_rejected';
      emit(name, base());
    },
  };
}