/**
 * D0-01 chat-core：会话消息模型（由 AIChat.tsx 逐字搬运）。
 */
import type { AIRunStepEvent, AIToolStreamEvent, AIUsageEstimate, RagAnswer } from '@/lib/api/ai-api';
import type { RunSnapshot } from '../run-status-bar';

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  createdAt: string;
  streaming?: boolean;
  sources?: RagAnswer[];
  /**
   * 工具调用过程事件（M1-04）：仅来自 SSE（M1-03），不额外轮询、不读审计表。
   * 事件缺失（旧后端/事件丢失）时组件不渲染时间线，内容仍由 `content` 承载。
   */
  toolEvents?: AIToolStreamEvent[];
  /**
   * 写工具的待审批 invocation（M1-05）：由 approval_pending 事件派生，渲染待审批卡片。
   * 状态刷新由卡片自身的「刷新状态」按钮触发（不轮询）。
   */
  pendingApprovals?: number[];
  /**
   * B1-08：v2 运行快照（`run_started`/`step`/`done`/`error` 增量维护）。
   * 旧后端不产生 v2 事件 → 保持 undefined，状态条与证据面板均不渲染。
   */
  run?: RunSnapshot;
  /** B1-08：v2 步骤（按 stepIndex 去重、上限 100 条兜底内存）。 */
  steps?: AIRunStepEvent[];
  /** 生效实例（done 事件回带；开关关闭时缺省）。 */
  providerInfo?: { provider?: string; providerSource?: string };
  /** T5-06a：会话 id（回答级反馈关联；历史消息可能缺省）。 */
  conversationId?: number;
  /** T5-06a：对应问题的 FNV-1a 哈希（仅分组关联，不存原文）。 */
  queryHash?: string;
  /**
   * O2-02/§13.4：本回合 token 用量**估算**（v2 `usage` 事件，随 done 回带）。
   * 口径：字符/4，非账单；旧后端缺省 → 诊断抽屉显示「无计量数据」。
   */
  usage?: AIUsageEstimate;
  error?: string;
}
