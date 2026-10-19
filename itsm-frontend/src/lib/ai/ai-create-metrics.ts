/**
 * V0-02 一句话建单度量（员工侧）：建单时长 / 误分类率代理 / 任务漏斗。
 *
 * 口径（诚实标注，方案 §11 / V0-02 验收）：
 * - **建单时长** = `generateTicketForm` 发起 → 提交成功（`durationMs` 同时写入 `latencyMs`，
 *   使 O0-03 看板的时延桶包含建单样本；样本量小，口径在证据中记录）；
 * - **误分类率代理** = AI 生成表单后用户修改过 `type`/`priority` 的比例
 *   （无人工标注真值，代理口径：纠正次数 / 任务完成次数）；
 * - 事件：`task_started` / `task_completed` / `task_failed` / `ai_create_field_corrected`。
 */
import { trackAssistantEvent } from '@/lib/ai/telemetry';

export interface AICreateSuggestedFields {
  type?: string;
  priority?: string;
}

/** 对比 AI 建议字段与用户最终字段：返回被纠正的字段名（type/priority）。 */
export function diffTicketFields(
  suggested: AICreateSuggestedFields,
  current: AICreateSuggestedFields
): string[] {
  const changed: string[] = [];
  if (suggested.type !== undefined && current.type !== undefined && suggested.type !== current.type) {
    changed.push('type');
  }
  if (
    suggested.priority !== undefined &&
    current.priority !== undefined &&
    suggested.priority !== current.priority
  ) {
    changed.push('priority');
  }
  return changed;
}

const AI_CREATE_TASK_ID = 'ai_create_ticket';

/** 一句话建单流程跟踪器（组件持有一个实例；纯计时 + 埋点，无其它副作用）。 */
export class AICreateFlowTracker {
  private startedAt?: number;

  private suggested: AICreateSuggestedFields = {};

  constructor(private readonly now: () => number = () => Date.now()) {}

  /** 流程开始（生成表单请求发起）。 */
  start(pageKey?: string): void {
    this.startedAt = this.now();
    this.suggested = {};
    trackAssistantEvent('task_started', {
      pageKey,
      taskCardId: AI_CREATE_TASK_ID,
      source: 'ai_create',
    });
  }

  /** 记录 AI 建议字段（首次生成表单的数据模型）。 */
  captureSuggested(fields: AICreateSuggestedFields): void {
    this.suggested = { ...fields };
  }

  /** 提交成功收口：先报字段纠正，再报任务完成（时长/误分类率均由此派生）。 */
  finishSuccess(current: AICreateSuggestedFields, pageKey?: string): void {
    if (this.startedAt === undefined) return;
    const durationMs = Math.max(0, this.now() - this.startedAt);
    for (const field of diffTicketFields(this.suggested, current)) {
      trackAssistantEvent('ai_create_field_corrected', {
        pageKey,
        taskCardId: AI_CREATE_TASK_ID,
        field,
        source: 'ai_create',
      });
    }
    trackAssistantEvent('task_completed', {
      pageKey,
      taskCardId: AI_CREATE_TASK_ID,
      durationMs,
      latencyMs: durationMs,
      source: 'ai_create',
    });
    this.startedAt = undefined;
  }

  /** 提交/生成失败收口。 */
  finishFailure(errorCode: string, pageKey?: string): void {
    if (this.startedAt === undefined) return;
    const durationMs = Math.max(0, this.now() - this.startedAt);
    trackAssistantEvent('task_failed', {
      pageKey,
      taskCardId: AI_CREATE_TASK_ID,
      durationMs,
      errorCode,
      source: 'ai_create',
    });
    this.startedAt = undefined;
  }

  /** 流程是否进行中（测试/诊断用）。 */
  isRunning(): boolean {
    return this.startedAt !== undefined;
  }
}