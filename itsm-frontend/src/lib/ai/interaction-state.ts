/**
 * I0-01：AI 助手交互状态机 + 错误字典（§3.2）。
 *
 * 设计约束：
 *   - 纯函数、零副作用、零 UI 依赖 —— 工作区（`/ai/chat`）与 Dock 共用同一判定，
 *     避免两个入口各自维护一套"什么时候该显示什么"的隐式规则；
 *   - 判定顺序即优先级：错误 > 审批/确认 > 工具 > 流式 > 交接 > 完成 > 就绪；
 *     错误优先是因为所有失败都会终止流式，若先判 `streaming` 会出现"已失败但仍显示生成中"；
 *   - 错误字典只做「原因码 → 文案 → 恢复动作」映射，恢复动作本身由调用方执行
 *     （本模块不导航、不发请求），便于两侧按自己的路由能力接线。
 */

/** §3.2 的 10 个面板状态（closed 即 FAB 态）。 */
export type AssistantInteractionState =
  | 'closed'
  | 'opening'
  | 'ready'
  | 'streaming'
  | 'tool_running'
  | 'awaiting_confirmation'
  | 'awaiting_approval'
  | 'degraded'
  | 'done'
  | 'handoff';

/** 推导状态所需的信号（全部由调用方从现有 store/会话状态映射，不新增数据请求）。 */
export interface AssistantInteractionSignals {
  /** 面板是否展开；false → closed(FAB)。 */
  panelOpen: boolean;
  /** 页面上下文与建议是否就绪；false 且已展开 → opening（骨架屏）。 */
  contextReady: boolean;
  /** 是否已有消息（未发送过任何内容时保持 ready，不进入 done）。 */
  hasMessages: boolean;
  /** 是否正在流式生成。 */
  streaming: boolean;
  /** 正在执行的工具名（时间线 running 项）；空 → 无工具在跑。 */
  runningTool?: string | null;
  /** 是否有待确认（写动作 Gate2 确认单）。 */
  pendingConfirmation?: boolean;
  /** 待人工审批数量（Gate3）。 */
  pendingApprovalCount?: number;
  /** 结构化错误码（如 AI_PROVIDER_DISABLED / budget_exceeded / rate_limited）。 */
  errorCode?: string | null;
  /** 兜底：已有可读错误文案但无错误码。 */
  hasError?: boolean;
  /** 最近一次运行终态；failed 视为 degraded，completed 且无错误视为 done。 */
  runStatus?: 'running' | 'completed' | 'failed' | null;
  /** 已产出结果落点（回执）数量；>0 视为 done。 */
  outcomeCount?: number;
  /** 是否处于"转人工"交接态。 */
  handoff?: boolean;
}

/**
 * 按 §3.2 优先级推导当前状态。
 *
 * 注意：`hasMessages` 仅在"有产物/完成"时参与 done 判定，避免刚发出一条用户消息
 * 就被误判为 done（此时应仍是 ready 或 streaming）。
 */
export function deriveInteractionState(signals: AssistantInteractionSignals): AssistantInteractionState {
  const {
    panelOpen,
    contextReady,
    hasMessages,
    streaming,
    runningTool,
    pendingConfirmation,
    pendingApprovalCount,
    errorCode,
    hasError,
    runStatus,
    outcomeCount,
    handoff,
  } = signals;

  if (!panelOpen) return 'closed';
  // 错误优先：失败可能伴随 streaming=false 的中间态，不能被后续分支覆盖。
  if (errorCode || hasError || runStatus === 'failed') return 'degraded';
  if (!contextReady) return 'opening';
  if (pendingConfirmation) return 'awaiting_confirmation';
  if ((pendingApprovalCount ?? 0) > 0) return 'awaiting_approval';
  if (runningTool) return 'tool_running';
  if (streaming) return 'streaming';
  if (handoff) return 'handoff';
  if (hasMessages && (runStatus === 'completed' || (outcomeCount ?? 0) > 0)) return 'done';
  return 'ready';
}

/** 恢复动作标识（由调用方映射为导航/重试/配置等具体行为）。 */
export type AssistantErrorAction =
  | 'retry'
  | 'open_settings'
  | 'open_approval'
  | 'switch_provider'
  | 'dismiss';

/** 错误字典条目：原因码 → 可读文案 → 恢复动作。 */
export interface AssistantErrorCopy {
  /** 稳定原因码（用于埋点与测试断言）。 */
  code: string;
  /** 一句话标题。 */
  title: string;
  /** 可读说明（不暴露堆栈/内部细节）。 */
  description: string;
  /** 恢复动作。 */
  action: AssistantErrorAction;
  /** 动作按钮文案。 */
  actionLabel: string;
}

const ENTRY = (
  code: string,
  title: string,
  description: string,
  action: AssistantErrorAction,
  actionLabel: string
): AssistantErrorCopy => ({ code, title, description, action, actionLabel });

/**
 * 错误字典（§3.2 / §6「原因码 → 文案 → 动作」）。
 *
 * 键包含：
 *   - 后端 Provider 错误码（`lib/api/llm-provider-api.ts` 已定义的真实码）；
 *   - Bot 运行时错误码（`budget_exceeded` 等，见 `service/bot/run_manager.go`）；
 *   - 传输层归类（`rate_limited` / `offline` / `stream_failed`）。
 */
export const ASSISTANT_ERROR_DICTIONARY: Record<string, AssistantErrorCopy> = {
  AI_PROVIDER_DISABLED: ENTRY(
    'AI_PROVIDER_DISABLED',
    '所选模型实例已停用',
    '该 Provider 已被管理员停用，本次回答没有生成。可切换到其他可用实例后重试。',
    'switch_provider',
    '切换模型'
  ),
  AI_PROVIDER_KEY_MISSING: ENTRY(
    'AI_PROVIDER_KEY_MISSING',
    '模型实例未配置密钥',
    '该 Provider 缺少 API Key，需要管理员在 AI 配置中补齐后才能回答。',
    'open_settings',
    '去配置'
  ),
  AI_PROVIDER_NOT_FOUND: ENTRY(
    'AI_PROVIDER_NOT_FOUND',
    '模型实例不存在',
    '所选 Provider 已被删除。请切换到可用实例或使用租户默认。',
    'switch_provider',
    '切换模型'
  ),
  AI_PROVIDER_UNAVAILABLE: ENTRY(
    'AI_PROVIDER_UNAVAILABLE',
    '模型管理服务暂不可用',
    'Provider 管理服务没有响应，稍后重试即可；本次问题不会丢失。',
    'retry',
    '重试'
  ),
  AI_PROTOCOL_NOT_IMPLEMENTED: ENTRY(
    'AI_PROTOCOL_NOT_IMPLEMENTED',
    '该协议尚未接入',
    '当前 Provider 使用的协议尚未实现，请换用已支持的协议或实例。',
    'switch_provider',
    '切换模型'
  ),
  budget_exceeded: ENTRY(
    'budget_exceeded',
    '本次任务超出预算',
    '已达到单次运行的步数/Token/工具调用上限，系统已安全中止。可拆分为更小的任务再试。',
    'dismiss',
    '知道了'
  ),
  rate_limited: ENTRY(
    'rate_limited',
    '请求过于频繁',
    '服务端触发了限流，稍等片刻再试即可。',
    'retry',
    '重试'
  ),
  offline: ENTRY(
    'offline',
    '网络已断开',
    '当前设备离线，恢复网络后可继续本次对话。',
    'retry',
    '重试'
  ),
  stream_failed: ENTRY(
    'stream_failed',
    '回答中断',
    '流式响应意外中断，已保留已生成的内容。可重试获取完整回答。',
    'retry',
    '重试'
  ),
  tool_failed: ENTRY(
    'tool_failed',
    '工具执行失败',
    '调用的工具没有成功完成，详细原因见过程证据。可重试或转人工处理。',
    'retry',
    '重试'
  ),
  approval_expired: ENTRY(
    'approval_expired',
    '审批单已失效',
    '该写操作的确认/审批单已过期或不可用，请前往审批页确认后再操作。',
    'open_approval',
    '前往审批'
  ),
  aborted: ENTRY(
    'aborted',
    '已停止生成',
    '你已主动停止本次回答，已生成的内容保留。可重新发送以获取完整回答。',
    'retry',
    '重新生成'
  ),
};

/** 未登记原因码的兜底文案（不隐藏错误，也不暴露内部细节）。 */
export const ASSISTANT_ERROR_FALLBACK: AssistantErrorCopy = ENTRY(
  'unknown',
  '回答失败',
  '本次请求没有完成。可以重试；若持续失败，请打开诊断抽屉导出轨迹 ID 反馈。',
  'retry',
  '重试'
);

/**
 * 从 HTTP/流式错误中抽取稳定原因码。
 *
 * 只做归类，不改写原始文案：调用方仍可用后端 message 作为 `detail`。
 */
export function classifyAssistantError(err: unknown): string {
  const candidate = err as
    | { errorCode?: string | null; httpStatus?: number | null; name?: string; message?: string }
    | undefined;
  if (!candidate) return 'unknown';
  if (candidate.name === 'AbortError') return 'aborted';
  if (typeof candidate.message === 'string' && candidate.message.includes('已停止生成')) return 'aborted';

  const code = candidate.errorCode;
  if (code && ASSISTANT_ERROR_DICTIONARY[code]) return code;

  const status = candidate.httpStatus;
  if (status === 429) return 'rate_limited';
  if (status === 0 || (typeof navigator !== 'undefined' && navigator.onLine === false)) return 'offline';
  if (status && status >= 500) return 'stream_failed';
  return 'unknown';
}

/**
 * 解析错误文案。
 *
 * @param code 稳定原因码（优先）
 * @param detail 后端可读 message（作为补充；仅在字典未命中时作为描述兜底）
 */
export function resolveAssistantErrorCopy(code?: string | null, detail?: string | null): AssistantErrorCopy {
  if (code && ASSISTANT_ERROR_DICTIONARY[code]) return ASSISTANT_ERROR_DICTIONARY[code];
  if (detail && detail.trim()) {
    return {
      ...ASSISTANT_ERROR_FALLBACK,
      description: detail.trim(),
    };
  }
  return ASSISTANT_ERROR_FALLBACK;
}
