/**
 * T5-04a handoff 预填契约。
 *
 * 只承载“打开页面 / 预填表单”所需的最小数据：
 * - source 固定，避免误读其他页面 state；
 * - targetType 必须匹配目标创建页；
 * - 预填只含标题/描述，不携带任意表单字段。
 */

export const HANDOFF_PREFILL_SOURCE = 'assistant_handoff';

export type HandoffPrefillTarget = 'ticket' | 'incident';

export interface HandoffPrefillPayload {
  title?: string;
  description?: string;
}

export interface HandoffPrefillState {
  source: typeof HANDOFF_PREFILL_SOURCE;
  targetType: HandoffPrefillTarget;
  prefill: HandoffPrefillPayload;
}

const TITLE_MAX = 200;
const DESCRIPTION_MAX = 4000;

const trimText = (value: unknown, max: number): string | undefined => {
  if (typeof value !== 'string') return undefined;
  const trimmed = value.trim();
  return trimmed ? trimmed.slice(0, max) : undefined;
};

/** 生成创建页一次性预填 state。 */
export function buildHandoffPrefillState(
  targetType: HandoffPrefillTarget,
  payload: HandoffPrefillPayload
): HandoffPrefillState {
  const prefill: HandoffPrefillPayload = {};
  const title = trimText(payload.title, TITLE_MAX);
  const description = trimText(payload.description, DESCRIPTION_MAX);
  if (title) prefill.title = title;
  if (description) prefill.description = description;
  return { source: HANDOFF_PREFILL_SOURCE, targetType, prefill };
}

/**
 * 读取并校验创建页 state；来源不符、目标类型不符、无有效字段一律返回 null。
 * 调用方消费后应清除路由 state（一次性）。
 */
export function readHandoffPrefill(
  state: unknown,
  targetType: HandoffPrefillTarget
): HandoffPrefillPayload | null {
  if (!state || typeof state !== 'object') return null;
  const raw = state as Record<string, unknown>;
  if (raw.source !== HANDOFF_PREFILL_SOURCE || raw.targetType !== targetType) return null;
  const prefillRaw = raw.prefill;
  if (!prefillRaw || typeof prefillRaw !== 'object') return null;
  const prefill = prefillRaw as Record<string, unknown>;
  const title = trimText(prefill.title, TITLE_MAX);
  const description = trimText(prefill.description, DESCRIPTION_MAX);
  if (!title && !description) return null;
  return { ...(title ? { title } : {}), ...(description ? { description } : {}) };
}

/**
 * 只填充空白字段：已有值（含用户已输入内容）一律不覆盖。
 * 返回可直接传给 Form.setFieldsValue 的 patch。
 */
export function pickBlankHandoffPrefill(
  prefill: HandoffPrefillPayload,
  current: { title?: unknown; description?: unknown }
): HandoffPrefillPayload {
  const patch: HandoffPrefillPayload = {};
  if (prefill.title && !current.title) patch.title = prefill.title;
  if (prefill.description && !current.description) patch.description = prefill.description;
  return patch;
}
