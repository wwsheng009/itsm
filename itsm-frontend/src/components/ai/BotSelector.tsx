import React from 'react';
import { Select, Tooltip } from 'antd';

import type { BotOption } from '@/lib/api/ai-api';

/** 默认助手哨兵值：选择它等价于「不指定 Bot」（后端 bot_id=0 → 内置默认助手）。 */
export const DEFAULT_BOT_VALUE = 0;

export interface BotSelectorLabels {
  /** 无障碍标签 / Tooltip 标题。 */
  title?: string;
  /** 默认助手选项文案。 */
  defaultBot?: string;
  /** 已有会话时（锁定态）的提示文案。 */
  lockedHint?: string;
}

export interface BotSelectorProps {
  /** 可见 Bot 列表（已由后端按角色 audience 过滤）。 */
  bots: BotOption[];
  /** 当前选择：null = 默认助手（未选择，兼容默认）。 */
  value: number | null;
  /** 选择变更（仅影响新会话）。 */
  onChange: (botId: number | null) => void;
  /** 已有会话：锁定切换（切换不回溯改写历史会话归属）。 */
  locked?: boolean;
  /** 加载中：不渲染（避免布局抖动）。 */
  loading?: boolean;
  labels?: BotSelectorLabels;
}

const DEFAULT_LABELS: Required<BotSelectorLabels> = {
  title: '选择 Bot（仅影响新会话）',
  defaultBot: '默认助手',
  lockedHint: '当前会话归属已固定；切换 Bot 仅影响新会话',
};

/**
 * 工作区 Bot 选择器（B2-04）。
 *
 * 设计要点：
 * - **兼容默认**：`bots` 为空（`bot.enabled=false` 或全部不可见）或加载中 → 渲染 null，
 *   聊天链路与引入该能力前完全一致；
 * - **切换仅影响新会话**：`locked`（已有会话）时禁用选择，历史会话归属由后端
 *   `conversation.bot_id` 决定，不随此处切换改写；
 * - **默认助手**：未选择（value=null）时发送请求不落 botId 字段（请求体与现状逐字节一致）。
 */
const BotSelector: React.FC<BotSelectorProps> = ({
  bots,
  value,
  onChange,
  locked = false,
  loading = false,
  labels,
}) => {
  const text = { ...DEFAULT_LABELS, ...(labels ?? {}) };
  if (loading || bots.length === 0) {
    return null;
  }

  const control = (
    <Select
      size="small"
      data-testid="bot-selector"
      aria-label={text.title}
      style={{ minWidth: 140, maxWidth: 220 }}
      value={value ?? DEFAULT_BOT_VALUE}
      disabled={locked}
      options={[
        { value: DEFAULT_BOT_VALUE, label: text.defaultBot },
        ...bots.map(bot => ({ value: bot.id, label: bot.name })),
      ]}
      onChange={next => onChange(next === DEFAULT_BOT_VALUE ? null : Number(next))}
    />
  );

  return locked ? <Tooltip title={text.lockedHint}>{control}</Tooltip> : control;
};

export default BotSelector;
