/**
 * I0-01：面板交互状态条（§3.2）。
 *
 * 只负责"把状态翻译成可见反馈 + 恢复动作"，不持有会话状态、不发请求：
 * 状态由 `deriveInteractionState` 从现有 store/会话信号推导，调用方按自身路由能力执行 action。
 *
 * 无障碍：状态变化使用 `role="status"` + `aria-live="polite"`（§3.7 流式内容可感知）。
 */
import React from 'react';
import { Alert, Button, Skeleton, Space, Tag, Typography, theme } from 'antd';
import { CheckCircle2, ShieldAlert, Wrench } from 'lucide-react';

import {
  resolveAssistantErrorCopy,
  type AssistantErrorAction,
  type AssistantInteractionState,
} from '@/lib/ai/interaction-state';

const { Text } = Typography;

export interface AssistantInteractionBannerProps {
  state: AssistantInteractionState;
  /** tool_running 时的工具名。 */
  runningTool?: string | null;
  /** awaiting_approval 时的待审批数量。 */
  pendingApprovalCount?: number;
  /** degraded 时的稳定原因码。 */
  errorCode?: string | null;
  /** degraded 时的后端可读文案（字典未命中时兜底）。 */
  errorDetail?: string | null;
  /** 恢复动作回调；未注入时只展示文案（不假装可操作）。 */
  onAction?: (action: AssistantErrorAction) => void;
  /** awaiting_approval 的跳转回调（与 onAction 分离，便于调用方复用路由）。 */
  onOpenApproval?: () => void;
  className?: string;
}

export const AssistantInteractionBanner: React.FC<AssistantInteractionBannerProps> = ({
  state,
  runningTool,
  pendingApprovalCount = 0,
  errorCode,
  errorDetail,
  onAction,
  onOpenApproval,
  className,
}) => {
  const { token } = theme.useToken();

  // ready / closed 不占位：closed 由 FAB 呈现，ready 保持界面干净。
  if (state === 'closed' || state === 'ready') return null;

  if (state === 'opening') {
    return (
      <div
        data-testid="assistant-interaction-banner"
        data-state="opening"
        className={className}
        style={{ padding: '10px 12px' }}
      >
        <Skeleton active title={false} paragraph={{ rows: 2, width: ['80%', '55%'] }} />
        <Text type="secondary" style={{ fontSize: 12 }}>
          正在准备页面上下文与建议…
        </Text>
      </div>
    );
  }

  if (state === 'degraded') {
    const copy = resolveAssistantErrorCopy(errorCode, errorDetail);
    return (
      <div data-testid="assistant-interaction-banner" data-state="degraded" className={className}>
        <Alert
          type="error"
          showIcon
          message={copy.title}
          description={copy.description}
          role="status"
          aria-live="polite"
          action={
            onAction ? (
              <Button size="small" danger onClick={() => onAction(copy.action)}>
                {copy.actionLabel}
              </Button>
            ) : undefined
          }
        />
      </div>
    );
  }

  if (state === 'streaming') {
    return (
      <div
        data-testid="assistant-interaction-banner"
        data-state="streaming"
        className={className}
        role="status"
        aria-live="polite"
        style={{ padding: '6px 12px' }}
      >
        <Space size={6}>
          <Tag color="processing" style={{ marginInlineEnd: 0 }}>
            生成中
          </Tag>
          <Text type="secondary" style={{ fontSize: 12 }}>
            回答正在流式生成，可随时停止。
          </Text>
        </Space>
      </div>
    );
  }

  if (state === 'tool_running') {
    return (
      <div
        data-testid="assistant-interaction-banner"
        data-state="tool_running"
        className={className}
        role="status"
        aria-live="polite"
        style={{ padding: '6px 12px' }}
      >
        <Space size={6}>
          <Wrench size={13} color={token.colorPrimary} />
          <Text style={{ fontSize: 12 }}>正在执行工具：</Text>
          <Tag style={{ marginInlineEnd: 0 }}>{runningTool || '未知工具'}</Tag>
        </Space>
      </div>
    );
  }

  if (state === 'awaiting_confirmation') {
    return (
      <div
        data-testid="assistant-interaction-banner"
        data-state="awaiting_confirmation"
        className={className}
        role="status"
        aria-live="polite"
        style={{ padding: '6px 12px' }}
      >
        <Space size={6}>
          <ShieldAlert size={13} color={token.colorWarning} />
          <Text style={{ fontSize: 12 }}>有写操作等待确认，确认后才会执行。</Text>
        </Space>
      </div>
    );
  }

  if (state === 'awaiting_approval') {
    return (
      <div
        data-testid="assistant-interaction-banner"
        data-state="awaiting_approval"
        className={className}
        role="status"
        aria-live="polite"
        style={{ padding: '6px 12px' }}
      >
        <Space size={6} wrap>
          <ShieldAlert size={13} color={token.colorWarning} />
          <Text style={{ fontSize: 12 }}>
            {pendingApprovalCount > 0 ? `${pendingApprovalCount} 个写操作等待人工审批` : '写操作等待人工审批'}
          </Text>
          {onOpenApproval ? (
            <Button type="link" size="small" style={{ padding: 0 }} onClick={onOpenApproval}>
              去审批
            </Button>
          ) : null}
        </Space>
      </div>
    );
  }

  if (state === 'done') {
    return (
      <div
        data-testid="assistant-interaction-banner"
        data-state="done"
        className={className}
        role="status"
        aria-live="polite"
        style={{ padding: '6px 12px' }}
      >
        <Space size={6}>
          <CheckCircle2 size={13} color={token.colorSuccess} />
          <Text type="secondary" style={{ fontSize: 12 }}>
            本轮已完成，结果落点见回执。
          </Text>
        </Space>
      </div>
    );
  }

  // handoff
  return (
    <div
      data-testid="assistant-interaction-banner"
      data-state="handoff"
      className={className}
      role="status"
      aria-live="polite"
      style={{ padding: '6px 12px' }}
    >
      <Space size={6}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          已转人工处理，可在对应业务页面继续。
        </Text>
      </Space>
    </div>
  );
};

export default AssistantInteractionBanner;
