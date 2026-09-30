import React, { useCallback } from 'react';
import { useNavigate } from 'react-router';
import { Button, Tooltip } from 'antd';
import { MessageOutlined } from '@ant-design/icons';

import {
  ASK_AI_ROUTE,
  buildAskAIState,
  type AskAIEntrypoint,
  type AskAITargetType,
} from '@/lib/ai/ask-ai-scope';

/**
 * B3-02 页面「问 AI」入口（渐进增强）。
 *
 * 行为：携带入口上下文（entrypoint/target_type/target_id/summary）打开工作区 `/ai/chat`；
 * 上下文经路由 state 传递并由工作区回传后端（后端会再次解析 + 目标对象预检）。
 *
 * 权限：`denied` 时按 `denyMode` 渲染——`disabled`（默认，置灰 + Tooltip 说明）或 `hidden`
 * （不渲染）。页面调用方负责判定（如 `hasPermission('ai','read')`），组件不做权限推断。
 */
export interface AskAILauncherProps {
  /** 入口枚举（页面语义；与后端 B3-01 对齐）。 */
  entrypoint: AskAIEntrypoint;
  /** 目标对象类型；与 targetId 成对提供（缺省 = 无目标上下文，例如列表页）。 */
  targetType?: AskAITargetType;
  /** 目标对象 ID（当前页面已加载成功的对象 ID）。 */
  targetId?: number;
  /** 上下文摘要（提示性；后端截断 300 rune）。 */
  summary?: string;
  /** 无权限/不可用时置灰（或按 denyMode 隐藏）。 */
  denied?: boolean;
  /** 拒绝态渲染方式：disabled（默认）/ hidden。 */
  denyMode?: 'disabled' | 'hidden';
  /** 拒绝原因（置灰时的 Tooltip 文案）。 */
  denyReason?: string;
  /** 按钮文案（默认「问 AI」）。 */
  label?: string;
  /** 尺寸（透传 antd）。 */
  size?: 'small' | 'middle' | 'large';
  /** 额外样式（页面布局用）。 */
  style?: React.CSSProperties;
  /** 测试注入：自定义跳转（缺省使用 react-router 的 navigate）。 */
  onNavigate?: (path: string, state: ReturnType<typeof buildAskAIState>) => void;
}

export const AskAILauncher: React.FC<AskAILauncherProps> = ({
  entrypoint,
  targetType,
  targetId,
  summary,
  denied = false,
  denyMode = 'disabled',
  denyReason = '无 AI 使用权限',
  label = '问 AI',
  size = 'small',
  style,
  onNavigate,
}) => {
  const navigate = useNavigate();

  const open = useCallback(() => {
    const state = buildAskAIState({ entrypoint, targetType, targetId, summary });
    if (onNavigate) {
      onNavigate(ASK_AI_ROUTE, state);
      return;
    }
    navigate(ASK_AI_ROUTE, { state });
  }, [entrypoint, targetType, targetId, summary, navigate, onNavigate]);

  if (denied && denyMode === 'hidden') {
    return null;
  }

  const button = (
    <Button
      type="text"
      size={size}
      icon={<MessageOutlined />}
      disabled={denied}
      style={style}
      onClick={denied ? undefined : open}
      data-testid="ask-ai-launcher"
      data-entrypoint={entrypoint}
    >
      {label}
    </Button>
  );

  return denied ? <Tooltip title={denyReason}>{button}</Tooltip> : button;
};

export default AskAILauncher;
