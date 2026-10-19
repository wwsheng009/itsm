/**
 * D0-03 页面助手上下文（L1 页面声明）类型。
 *
 * 契约（方案 §4.1 L1 / §3.3 任务卡 / I0-02 分组模型）：
 * - `pageKey`：稳定页面键（埋点 `pageKey` 与 Page Manifest 共用）；
 * - `pattern`：react-router 路由模式（`end: true` 匹配）；
 * - `entrypoint`：最近语义入口（对齐后端 `service/bot/scope.go` 枚举；缺省按 `chat`）；
 * - `scene`：场景 Bot slug（对齐 `service/bot/scenarios.go`，如 s1/s2/s3/s4）；
 * - **I0-02 分组**：`tasks[]`（任务卡，零输入可执行；每页 ≥1，必须只读/plan——§D4 不变量）
 *   与 `questions[]`（问题卡，了解型）；
 * - 消费统一走 `flattenPageActions()`（任务卡在前、问题卡在后），后端 D1-01 注册表亦按该顺序扁平化。
 */
import type { AskAIEntrypoint } from '@/lib/ai/ask-ai-scope';

export type PageAssistantActionKind = 'task' | 'question';

/** 动作公共字段（工具依赖 / 回退路由声明见 D1-01/D1-02）。 */
export interface PageAssistantActionBase {
  /** 稳定动作 id（页内唯一；动作桥 `assistant-actions` 以此为键）。 */
  id: string;
  /** 卡片文案（静态注册）。 */
  label: string;
  /** 点击后发送的提示词（零输入发起）。 */
  prompt: string;
  /** 零输入可执行必须为只读/plan；问题卡恒 true。 */
  readOnly: boolean;
  /**
   * D1-01：该建议依赖的工具名（可选；如 ['list_tickets']）。
   * 服务端 `/ai/assistant/context` 据此做「场景 Bot 授权 ∩ 调用方 RBAC ∩ 风险上限」过滤；
   * 未声明的建议只做场景可见性过滤（未声明 ≠ 无依赖，而是暂不纳入工具门禁）。
   */
  tools?: string[];
  /**
   * D1-02：动作未注册 handler 时的回退路由（动作桥 `executeAssistantAction`）。
   * 缺省 = 点击后把 prompt 交给对话链路（与 D0-05 行为一致）。
   */
  route?: string;
}

/** 任务卡：零输入可执行（只读/plan；契约测试锁定）。 */
export interface PageAssistantTask extends PageAssistantActionBase {
  kind: 'task';
}

/** 问题卡：了解型（点击即问）。 */
export interface PageAssistantQuestion extends PageAssistantActionBase {
  kind: 'question';
}

/** 兼容扁平视图（D0-05/D1-01 消费口径）。 */
export interface PageAssistantAction extends PageAssistantActionBase {
  kind: PageAssistantActionKind;
}

export interface PageAssistantDeclaration {
  pageKey: string;
  pattern: string;
  /** 页面标题（用于上下文条展示；与面包屑口径一致）。 */
  title: string;
  scene?: string;
  entrypoint?: AskAIEntrypoint;
  /** I0-02：任务卡（每页至少 1 个）。 */
  tasks: PageAssistantTask[];
  /** I0-02：问题卡（可选）。 */
  questions: PageAssistantQuestion[];
}

/** 扁平化页面动作：任务卡在前、问题卡在后（服务端 rank 与建议区顺序一致）。 */
export function flattenPageActions(declaration: PageAssistantDeclaration): PageAssistantAction[] {
  return [
    ...(declaration.tasks ?? []).map(task => ({ ...task, kind: 'task' as const })),
    ...(declaration.questions ?? []).map(question => ({ ...question, kind: 'question' as const })),
  ];
}