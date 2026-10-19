/**
 * D0-01 chat-core：会话内核常量（由 AIChat.tsx 逐字搬运）。
 */

/**
 * 时间线中不再重复展示的状态（由待审批卡片单独承载）。
 * 常量置于模块级，避免每次渲染新建数组导致 ToolCallTimeline 的重算。
 */
export const PENDING_HIDDEN_STATUSES: readonly string[] = ['pending'];

/** done 事件 providerSource → 可读来源（BE-7）。 */
export const PROVIDER_SOURCE_LABELS: Record<string, string> = {
  request: '会话指定',
  user: '个人默认',
  tenant: '租户默认',
  static: '静态配置',
};

/** 空状态建议卡片（点击即发送）。 */
export const SUGGESTIONS = [
  '总结我最近处理过的工单',
  '这个问题的标准处置流程是什么？',
  '知识库里有没有相关方案？',
  '帮我起草一份变更方案',
];

/** 消息列宽度（与输入坞同宽居中）。 */
export const CONTENT_MAX_WIDTH = 820;

/** 距底部小于该阈值时恢复"自动滚到底"。 */
export const AUTO_SCROLL_THRESHOLD = 120;

export const nextId = () => `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
