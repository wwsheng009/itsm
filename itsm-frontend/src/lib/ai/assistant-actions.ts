/**
 * D1-02 动作桥：`actionId → handler`；未注册 handler 的动作按「声明 route → 路由跳转」，
 * 否则回退为把提示词交给对话链路（默认行为，与 D0-05 一致）。
 *
 * 设计边界：
 * - handler 由页面显式注册（`useRegisterAssistantAction`），卸载自动注销；
 * - 桥本身**不执行任何写动作**：写动作仍走对话内确认闭环 / Gate3 审批（§D4 不变量）；
 * - 返回 dispatch 结果（handler | navigated | prompt），供埋点 `extra.dispatch` 区分落地方式。
 */
import type { AssistantPageData } from '@/lib/store/assistant-context-store';

export type AssistantActionDispatch = 'handler' | 'navigated' | 'prompt';

export interface AssistantActionContext {
  actionId: string;
  prompt: string;
  pageData: AssistantPageData | null;
  navigate: (to: string) => void;
  sendPrompt: (prompt: string) => void;
}

export type AssistantActionHandler = (ctx: AssistantActionContext) => void;

const handlers = new Map<string, AssistantActionHandler>();

/** 注册动作 handler；返回注销函数（仅当仍指向自己时才移除，后注册者优先）。 */
export function registerAssistantAction(
  actionId: string,
  handler: AssistantActionHandler
): () => void {
  if (!actionId || typeof handler !== 'function') {
    return () => undefined;
  }
  handlers.set(actionId, handler);
  return () => {
    if (handlers.get(actionId) === handler) handlers.delete(actionId);
  };
}

/** 清空全部 handler（测试/登出场景）。 */
export function clearAssistantActions(): void {
  handlers.clear();
}

export interface AssistantActionInput {
  actionId: string;
  prompt: string;
  /** 动作声明的跳转目标（可选；仅 handler 未注册时使用）。 */
  route?: string;
}

/**
 * 执行动作桥：
 *  1) 已注册 handler → 执行并返回 'handler'（异常静默，不阻塞对话链路）；
 *  2) 未注册但声明 route → 路由跳转，返回 'navigated'；
 *  3) 其余 → 发送提示词（现状），返回 'prompt'。
 */
export function executeAssistantAction(
  action: AssistantActionInput,
  ctx: Omit<AssistantActionContext, 'actionId' | 'prompt'>
): AssistantActionDispatch {
  const handler = handlers.get(action.actionId);
  const fullCtx: AssistantActionContext = {
    actionId: action.actionId,
    prompt: action.prompt,
    ...ctx,
  };

  if (handler) {
    try {
      handler(fullCtx);
    } catch {
      // 动作异常不得阻塞对话链路；错误经业务组件自行上报（error_shown）。
    }
    return 'handler';
  }
  if (action.route) {
    ctx.navigate(action.route);
    return 'navigated';
  }
  ctx.sendPrompt(action.prompt);
  return 'prompt';
}