/**
 * T5-02 输入坞智能辅助（解析层，纯函数）：
 * - `/` 快捷指令：整串 trimmed 值匹配 `^\/\S*$` 时开启菜单，`/` 后的字符作为过滤词；
 * - `@` 当前页对象引用：值末尾匹配 `(?:^|\s)@([^\s@]*)$` 时开启菜单；
 * - 选中一律「只填不发」：把触发 token 替换为脚手架文案 / 引用标签，由用户继续编辑或发送。
 *
 * 命令集来源：UX 评审 B4 示例（`/工单` `/SLA` `/话术`）；注册表可扩展，
 * 后续与 I0-03 命令面板统一时替换本注册表即可（调用方不感知）。
 */
import { askAITargetTypeLabel } from '@/lib/ai/ask-ai-scope';
import type { AssistantPageData } from '@/lib/store/assistant-context-store';

export interface SlashCommand {
  /** 命令名（含 `/`），如 `/工单`。 */
  name: string;
  /** 菜单副标题（一句话说明）。 */
  description: string;
  /** 选中后写入输入框的脚手架文案（只填不发，用户可继续编辑）。 */
  insertText: string;
}

/** `/` 快捷指令注册表（T5-02 v1）。 */
export const SLASH_COMMANDS: readonly SlashCommand[] = [
  {
    name: '/工单',
    description: '总结或查询我最近处理过的工单',
    insertText: '总结我最近处理过的工单',
  },
  {
    name: '/SLA',
    description: '查看 SLA 达成与违约情况',
    insertText: '当前 SLA 达成情况如何？',
  },
  {
    name: '/话术',
    description: '起草一段客户沟通话术',
    insertText: '帮我起草一段客户沟通话术',
  },
];

/** 斜杠触发：整串输入恰为 `/xxx`（`xxx` 不含空白与 `/`）。 */
const SLASH_TRIGGER = /^\/([^\s/]*)$/;

/** 返回 `/` 后的过滤词（可为空串）；非触发态返回 null。 */
export function parseSlashTrigger(value: string): string | null {
  const match = SLASH_TRIGGER.exec(value.trim());
  return match ? match[1] : null;
}

/** 按过滤词筛选命令（大小写不敏感；空过滤词返回全部）。 */
export function filterSlashCommands(keyword: string): SlashCommand[] {
  const kw = keyword.trim().toLowerCase();
  if (!kw) return [...SLASH_COMMANDS];
  return SLASH_COMMANDS.filter(
    command =>
      command.name.toLowerCase().includes(kw) ||
      command.name.slice(1).toLowerCase().includes(kw)
  );
}

/** 选中命令：整串触发替换为插入文案；不在触发态时原样返回（防御）。 */
export function applySlashCommand(value: string, command: SlashCommand): string {
  return parseSlashTrigger(value) === null ? value : command.insertText;
}

/** `@` 触发：位于值开头或空白之后、直到末尾的 `@…` 片段（内容不含空白/@）。 */
const REFERENCE_TRIGGER = /(?:^|\s)@([^\s@]*)$/;

export interface ReferenceTrigger {
  /** `@` 在输入串中的起始下标（替换起点）。 */
  start: number;
  /** `@` 之后的过滤词（可能为空串）。 */
  keyword: string;
}

/** 解析 `@` 触发段；非触发态返回 null。 */
export function parseReferenceTrigger(value: string): ReferenceTrigger | null {
  const match = REFERENCE_TRIGGER.exec(value);
  if (!match) return null;
  return { start: match.index + match[0].indexOf('@'), keyword: match[1] };
}

/** 引用标签（用户可见）：`@工单#42`。 */
export function referenceDisplayText(page: AssistantPageData): string {
  return `@${askAITargetTypeLabel(page.targetType)}#${page.targetId}`;
}

/** 过滤词是否命中该页对象（标签/编号/摘要，大小写不敏感；空过滤词恒命中）。 */
export function matchesReferenceKeyword(page: AssistantPageData, keyword: string): boolean {
  const kw = keyword.trim().toLowerCase();
  if (!kw) return true;
  const haystack =
    `${referenceDisplayText(page)} ${askAITargetTypeLabel(page.targetType)}${page.targetId} ${
      page.summary ?? ''
    }`.toLowerCase();
  return haystack.includes(kw);
}

/**
 * 选中引用：把 `@…` 触发段替换为引用标签 + 尾随空格。
 * 尾随空格让值立即脱离触发态（`@` 后不再续接过滤词），便于用户继续输入。
 */
export function applyReference(value: string, page: AssistantPageData): string {
  const trigger = parseReferenceTrigger(value);
  if (!trigger) return value;
  return `${value.slice(0, trigger.start)}${referenceDisplayText(page)} `;
}
