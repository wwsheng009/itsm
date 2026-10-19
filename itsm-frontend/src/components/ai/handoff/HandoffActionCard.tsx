/**
 * T5-04a：消息级「转人工」动作卡。
 *
 * 只提供两个闭环动作：
 * - 打开对应页面（当前对象或列表）；
 * - 预填新建工单/事件（仅写创建页草稿，不自动提交）。
 *
 * 权限在动作生成时裁剪；页面消费预填时仍按空白字段优先，不覆盖人工输入。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, App, Button, Modal, Select, Space, Typography } from 'antd';
import { ExternalLink, FilePlus2, UserPlus } from 'lucide-react';
import { useNavigate } from 'react-router';

import type { ChatMessage } from '@/components/ai/chat-core/types';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import { useAssistantPageData } from '@/lib/store/assistant-context-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useI18n } from '@/lib/i18n/useI18n';
import {
  buildHandoffPrefillState,
  type HandoffPrefillTarget,
} from '@/lib/ai/handoff/prefill';
import { TicketApi } from '@/lib/api/ticket-api';
import { IncidentAPI } from '@/lib/api/incident-api';
import { UserApi } from '@/lib/api/user-api';

const { Text } = Typography;

export const HANDOFF_ACTION_CARD_TESTID = 'handoff-action-card';

interface HandoffActionCardProps {
  open: boolean;
  onClose: () => void;
  /** 当前点击「转人工」的助手消息；用于生成预填描述。 */
  message?: ChatMessage | null;
}

type HandoffAction =
  | {
      id: 'open';
      label: string;
      route: string;
      targetType?: string;
      targetId?: number;
    }
  | {
      id: 'prefill-ticket' | 'prefill-incident';
      label: string;
      targetType: HandoffPrefillTarget;
    }
  | {
      id: 'assign';
      label: string;
      targetType: 'ticket' | 'incident';
      targetId: number;
    };

const targetRoute = (targetType: string, targetId: number): string | null => {
  switch (targetType) {
    case 'ticket':
      return `/tickets/${targetId}`;
    case 'incident':
      return `/incidents/${targetId}`;
    case 'change':
      return `/changes/${targetId}`;
    case 'ci':
      return `/cmdb/cis/${targetId}`;
    default:
      return null;
  }
};

const targetResource = (targetType: string): string | null => {
  switch (targetType) {
    case 'ticket':
      return 'ticket';
    case 'incident':
      return 'incident';
    case 'change':
      return 'change';
    case 'ci':
      return 'cmdb';
    default:
      return null;
  }
};

const HandoffActionCard: React.FC<HandoffActionCardProps> = ({ open, onClose, message }) => {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { message: antMessage } = App.useApp();
  const { hasPermission } = usePermissions();
  const { pageKey } = usePageAssistantContext();
  const pageData = useAssistantPageData();
  const [assignMode, setAssignMode] = useState(false);
  const [assignTarget, setAssignTarget] = useState<{ targetType: 'ticket' | 'incident'; targetId: number } | null>(null);
  const [assigneeId, setAssigneeId] = useState<number | undefined>();
  const [assigneeOptions, setAssigneeOptions] = useState<Array<{ value: number; label: string }>>([]);
  const [assigneesLoading, setAssigneesLoading] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const [assignError, setAssignError] = useState<string | null>(null);

  const resetAssign = useCallback(() => {
    setAssignMode(false);
    setAssignTarget(null);
    setAssigneeId(undefined);
    setAssigneesLoading(false);
    setAssigning(false);
    setAssignError(null);
  }, []);

  useEffect(() => {
    if (!open) resetAssign();
  }, [open, resetAssign]);

  const prefillTitle = useMemo(() => {
    const summary = pageData?.summary?.trim();
    if (summary) return summary.slice(0, 200);
    return t('assistant.handoff.defaultTitle');
  }, [pageData?.summary, t]);

  const prefillDescription = useMemo(() => {
    const content = message?.content?.trim();
    return content ? content.slice(0, 4000) : t('assistant.handoff.defaultDescription');
  }, [message?.content, t]);

  const actions = useMemo<HandoffAction[]>(() => {
    const next: HandoffAction[] = [];
    if (pageData) {
      const route = targetRoute(pageData.targetType, pageData.targetId);
      const resource = targetResource(pageData.targetType);
      if (route && resource && hasPermission(resource, 'read')) {
        next.push({
          id: 'open',
          label: t('assistant.handoff.openCurrent'),
          route,
          targetType: pageData.targetType,
          targetId: pageData.targetId,
        });
      }
    }
    if (!next.some(item => item.id === 'open')) {
      if (pageData?.targetType === 'incident' || hasPermission('incident', 'read')) {
        next.push({
          id: 'open',
          label: t('assistant.handoff.openIncidentList'),
          route: '/incidents',
        });
      } else if (hasPermission('ticket', 'read')) {
        next.push({
          id: 'open',
          label: t('assistant.handoff.openTicketList'),
          route: '/tickets',
        });
      }
    }
    if (hasPermission('ticket', 'create')) {
      next.push({ id: 'prefill-ticket', label: t('assistant.handoff.prefillTicket'), targetType: 'ticket' });
    }
    if (hasPermission('incident', 'write')) {
      next.push({
        id: 'prefill-incident',
        label: t('assistant.handoff.prefillIncident'),
        targetType: 'incident',
      });
    }
    if (pageData?.targetType === 'ticket' && hasPermission('ticket', 'assign')) {
      next.push({
        id: 'assign',
        label: t('assistant.handoff.assign'),
        targetType: 'ticket',
        targetId: pageData.targetId,
      });
    }
    if (pageData?.targetType === 'incident' && hasPermission('incident', 'write')) {
      next.push({
        id: 'assign',
        label: t('assistant.handoff.assign'),
        targetType: 'incident',
        targetId: pageData.targetId,
      });
    }
    return next;
  }, [hasPermission, pageData, t]);

  const loadAssignees = useCallback(async () => {
    setAssigneesLoading(true);
    setAssignError(null);
    try {
      const result = await UserApi.getUsers({ page: 1, pageSize: 200, status: 'active' });
      setAssigneeOptions(
        (result.users ?? []).map(user => ({
          value: user.id,
          label: `${user.name || user.username}${user.department ? ` · ${user.department}` : ''}`,
        }))
      );
    } catch {
      setAssignError(t('assistant.handoff.assigneesLoadFailed'));
    } finally {
      setAssigneesLoading(false);
    }
  }, [t]);

  const handleAction = useCallback(
    (action: HandoffAction) => {
      if (action.id === 'assign') {
        setAssignTarget({ targetType: action.targetType, targetId: action.targetId });
        setAssignMode(true);
        setAssignError(null);
        void loadAssignees();
        return;
      }
      trackAssistantEvent('handoff', {
        pageKey,
        conversationId: message?.conversationId,
        queryHash: message?.queryHash,
        extra: {
          action: action.id,
          targetType: action.targetType,
          targetId: action.id === 'open' ? action.targetId : undefined,
        },
      });
      onClose();
      if (action.id === 'open') {
        navigate(action.route);
        return;
      }
      navigate(action.targetType === 'ticket' ? '/tickets/create' : '/incidents/create', {
        state: buildHandoffPrefillState(action.targetType, {
          title: prefillTitle,
          description: prefillDescription,
        }),
      });
    },
    [
      loadAssignees,
      message?.conversationId,
      message?.queryHash,
      navigate,
      onClose,
      pageKey,
      prefillDescription,
      prefillTitle,
    ]
  );

  const handleAssign = useCallback(async () => {
    if (!assignTarget || !assigneeId) return;
    setAssigning(true);
    setAssignError(null);
    try {
      if (assignTarget.targetType === 'ticket') {
        await TicketApi.assignTicket(assignTarget.targetId, { assigneeId });
      } else {
        await IncidentAPI.assignIncident(assignTarget.targetId, assigneeId);
      }
      trackAssistantEvent('handoff', {
        pageKey,
        conversationId: message?.conversationId,
        queryHash: message?.queryHash,
        extra: {
          action: 'assign',
          targetType: assignTarget.targetType,
          targetId: assignTarget.targetId,
          assigneeId,
          success: true,
        },
      });
      antMessage.success(t('assistant.handoff.assignSuccess'));
      onClose();
      resetAssign();
    } catch (err) {
      const reason =
        (err as { message?: string } | undefined)?.message || t('assistant.handoff.assignFailed');
      setAssignError(reason);
      trackAssistantEvent('handoff', {
        pageKey,
        conversationId: message?.conversationId,
        queryHash: message?.queryHash,
        extra: {
          action: 'assign',
          targetType: assignTarget.targetType,
          targetId: assignTarget.targetId,
          assigneeId,
          success: false,
        },
      });
    } finally {
      setAssigning(false);
    }
  }, [antMessage, assignTarget, assigneeId, message?.conversationId, message?.queryHash, onClose, pageKey, resetAssign, t]);

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      title={assignMode ? t('assistant.handoff.assignTitle') : t('assistant.handoff.title')}
      data-testid={HANDOFF_ACTION_CARD_TESTID}
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {assignMode && assignTarget ? (
          <>
            <Text type="secondary">
              {t('assistant.handoff.assignHint', {
                type: assignTarget.targetType === 'ticket' ? '工单' : '事件',
                id: assignTarget.targetId,
              })}
            </Text>
            <Select
              showSearch
              value={assigneeId}
              onChange={setAssigneeId}
              loading={assigneesLoading}
              options={assigneeOptions}
              placeholder={t('assistant.handoff.assignPlaceholder')}
              optionFilterProp="label"
              data-testid="handoff-assignee-select"
              style={{ width: '100%' }}
            />
            {assignError ? (
              <Alert
                type="error"
                showIcon
                data-testid="handoff-assign-error"
                message={assignError}
              />
            ) : null}
            <Space>
              <Button onClick={resetAssign}>{t('assistant.handoff.assignCancel')}</Button>
              <Button
                type="primary"
                loading={assigning}
                disabled={!assigneeId}
                data-testid="handoff-assign-confirm"
                onClick={() => void handleAssign()}
              >
                {t('assistant.handoff.assignConfirm')}
              </Button>
            </Space>
          </>
        ) : (
          <>
            <Text type="secondary">{t('assistant.handoff.description')}</Text>
            {actions.length > 0 ? (
              actions.map(action => (
                <Button
                  key={action.id}
                  block
                  size="large"
                  icon={
                    action.id === 'open' ? (
                      <ExternalLink size={16} />
                    ) : action.id === 'assign' ? (
                      <UserPlus size={16} />
                    ) : (
                      <FilePlus2 size={16} />
                    )
                  }
                  data-testid={`handoff-action-${action.id}`}
                  onClick={() => handleAction(action)}
                  style={{ justifyContent: 'flex-start' }}
                >
                  {action.label}
                </Button>
              ))
            ) : (
              <Alert
                type="info"
                showIcon
                data-testid="handoff-empty"
                message={t('assistant.handoff.empty')}
              />
            )}
          </>
        )}
      </Space>
    </Modal>
  );
};

export default HandoffActionCard;
