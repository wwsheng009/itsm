/**
 * O2-02：会话诊断入口（Dock 面板与 `/ai/chat` 工作区共用的单一实现）。
 *
 * - 一个「会话诊断」按钮 + 按需挂载的 `DiagnosticsDrawer`（关闭时不请求轨迹，零额外请求）；
 * - 两处放置（面板页签行 / 工作区工具条）共用同一组件与页面键语义，避免两套入口漂移；
 * - `testId` 可覆写以区分放置位：默认沿用面板既有 `assistant-diagnostics-open`（既有测试不变），
 *   工作区用 `ai-chat-diagnostics-open`——避免同页两个入口在同一 `getByTestId` 查询下产生歧义。
 * - 诚实降级：抽屉自身对轨迹缺省/失败如实说明，本入口不改变其语义。
 */
import React, { useState } from 'react';
import { Button } from 'antd';
import { Stethoscope } from 'lucide-react';

import DiagnosticsDrawer from './DiagnosticsDrawer';
import type { ChatMessage } from '@/components/ai/chat-core/types';

export const ASSISTANT_DIAGNOSTICS_OPEN_TESTID = 'assistant-diagnostics-open';
export const AI_CHAT_DIAGNOSTICS_OPEN_TESTID = 'ai-chat-diagnostics-open';

export interface AssistantDiagnosticsEntryProps {
  /** 当前会话消息（用于本地快照：会话/消息数/生成状态/最近运行/步骤耗时/token 估算）。 */
  messages?: ChatMessage[];
  /** 当前会话 ID（用于「复制会话 ID」报障；纯问答会话为空时抽屉回退会话 ID）。 */
  conversationId?: number;
  /** 生成中标记（抽屉展示「生成中/空闲」）。 */
  streaming?: boolean;
  /** 埋点用页面键（缺省不落字段）。 */
  pageKey?: string;
  /** 入口按钮 testid 覆写（默认 `assistant-diagnostics-open`）。 */
  testId?: string;
}

const AssistantDiagnosticsEntry: React.FC<AssistantDiagnosticsEntryProps> = ({
  messages,
  conversationId,
  streaming,
  pageKey,
  testId = ASSISTANT_DIAGNOSTICS_OPEN_TESTID,
}) => {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        data-testid={testId}
        type="text"
        size="small"
        icon={<Stethoscope size={14} />}
        aria-label="会话诊断"
        title="会话诊断（轨迹/耗时/报障 ID）"
        onClick={() => setOpen(true)}
      />
      <DiagnosticsDrawer
        open={open}
        onClose={() => setOpen(false)}
        messages={messages}
        conversationId={conversationId}
        streaming={streaming}
        pageKey={pageKey}
      />
    </>
  );
};

export default AssistantDiagnosticsEntry;