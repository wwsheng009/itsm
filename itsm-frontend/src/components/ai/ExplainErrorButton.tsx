/**
 * I1-02 错误解释入口：错误旁「解释这个错误」→ 助手面板预填（只填不发送）。
 *
 * 口径（方案 §3.5 报错解释）：错误信息（≤500 字符）+ 可选上下文（页面/模块）拼成提示词；
 * 埋点 `handoff(from=error_explain)`；预填通道复用 assistant-store.pendingPrompt（I0-03）。
 */
import React, { useCallback } from 'react';
import { Button } from 'antd';
import { HelpCircle } from 'lucide-react';

import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

const MAX_ERROR_CHARS = 500;

/** 从任意错误值提取可读消息（Error/字符串/对象兜底）。 */
export const errorMessageOf = (error: unknown): string => {
  if (error instanceof Error) return error.message || error.name;
  // react-router 的 errorElement 可能收到 Response（404/500 等）。
  // 用鸭子类型判定（instanceof 在跨 realm/测试环境不可靠）。
  const responseLike = error as { status?: unknown; statusText?: unknown } | null;
  if (
    responseLike &&
    typeof responseLike === 'object' &&
    typeof responseLike.status === 'number' &&
    typeof responseLike.statusText === 'string'
  ) {
    return `HTTP ${responseLike.status}${responseLike.statusText ? ` ${responseLike.statusText}` : ''}`;
  }
  if (typeof error === 'string') return error;
  if (error === null || error === undefined) return '未知错误';
  try {
    return JSON.stringify(error);
  } catch {
    return '未知错误';
  }
};

/** 错误解释提示词（截断 + 可选上下文；不自动发送）。 */
export const buildExplainErrorPrompt = (message: string, context?: string): string => {
  const trimmed = message.trim().slice(0, MAX_ERROR_CHARS) || '未知错误';
  const contextLine = context?.trim() ? `\n出错位置：${context.trim().slice(0, 200)}` : '';
  return (
    '请解释下面这个错误的含义与可能原因，并给出排查步骤（不要假设我已修改任何配置）：' +
    `\n"""${trimmed}"""${contextLine}`
  );
};

export interface ExplainErrorButtonProps {
  error?: unknown;
  /** 附加上下文（页面 URL / 模块名等）。 */
  context?: string;
  label?: string;
  size?: 'small' | 'middle';
  type?: 'default' | 'primary' | 'link' | 'text';
}

export const ExplainErrorButton: React.FC<ExplainErrorButtonProps> = ({
  error,
  context,
  label = '解释这个错误',
  size = 'small',
  type = 'default',
}) => {
  const setPendingPrompt = useAssistantStore(store => store.setPendingPrompt);
  const openPanel = useAssistantStore(store => store.openPanel);

  const handleExplain = useCallback(() => {
    setPendingPrompt(buildExplainErrorPrompt(errorMessageOf(error), context));
    openPanel();
    trackAssistantEvent('handoff', { extra: { from: 'error_explain' } });
  }, [context, error, openPanel, setPendingPrompt]);

  if (error === null || error === undefined) return null;

  return (
    <Button
      data-testid="explain-error-button"
      size={size}
      type={type}
      icon={<HelpCircle size={14} />}
      onClick={handleExplain}
    >
      {label}
    </Button>
  );
};

export default ExplainErrorButton;