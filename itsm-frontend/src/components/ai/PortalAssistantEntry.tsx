/**
 * V1-02 门户入口：员工自助门户（服务目录等）的助手形态。
 *
 * 场景：员工在门户里「不知道选哪个服务 / 不知道从哪提」——入口把问题交给常驻 Dock，
 * 并预填提示词（只读建议；不自动提交任何申请/表单）。
 * 门禁：`ai:read`（与 Dock/Header 同码），无权限不渲染。
 * 埋点：复用既有事件 `assistant_opened`（source=launcher），事件字典零变更。
 */
import React, { useCallback } from 'react';
import { Button, Card, Typography, theme } from 'antd';
import { Bot, Sparkles } from 'lucide-react';

import { usePermissions } from '@/lib/hooks/use-permissions';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

export const PORTAL_ASSISTANT_ENTRY_TESTID = 'portal-assistant-entry';
export const PORTAL_ASSISTANT_ENTRY_OPEN_TESTID = 'portal-assistant-entry-open';

/** 缺省提示词：与 service.catalog 注册表的「该选哪个服务？」建议同义（只读、不提交）。 */
const DEFAULT_PORTAL_PROMPT =
  '我遇到一个问题，不确定该走哪个服务目录条目或工单类型，请帮我判断并说明理由（只读，不提交申请）。';

const { Text } = Typography;

interface PortalAssistantEntryProps {
  /** 埋点页面键（如 `service.catalog`）。 */
  pageKey: string;
  /** 打开 Dock 时预填的提示词；缺省为「帮我判断该选哪个服务」。 */
  prompt?: string;
  title?: string;
  description?: string;
}

const PortalAssistantEntry: React.FC<PortalAssistantEntryProps> = ({
  pageKey,
  prompt,
  title = '不知道该选哪个服务？',
  description = '描述你遇到的问题，AI 帮你判断该走哪个服务/工单类型；只给建议，不自动提交申请。',
}) => {
  const { token } = theme.useToken();
  const { hasPermission } = usePermissions();
  const openPanel = useAssistantStore(state => state.openPanel);
  const setPendingPrompt = useAssistantStore(state => state.setPendingPrompt);
  const canReadAssistant = hasPermission('ai', 'read');

  const handleOpen = useCallback(() => {
    trackAssistantEvent('assistant_opened', {
      pageKey,
      source: 'launcher',
      extra: { entry: 'portal_entry' },
    });
    setPendingPrompt(prompt ?? DEFAULT_PORTAL_PROMPT);
    openPanel();
  }, [pageKey, prompt, setPendingPrompt, openPanel]);

  if (!canReadAssistant) return null;

  return (
    <Card
      data-testid={PORTAL_ASSISTANT_ENTRY_TESTID}
      size="small"
      style={{ marginBottom: 16, borderColor: token.colorPrimaryBorder }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
        <Bot size={18} color={token.colorPrimary} aria-hidden />
        <div style={{ flex: 1, minWidth: 220 }}>
          <Text strong style={{ fontSize: 13 }}>
            {title}
          </Text>
          <div>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {description}
            </Text>
          </div>
        </div>
        <Button
          type="primary"
          ghost
          size="small"
          icon={<Sparkles size={14} />}
          onClick={handleOpen}
          data-testid={PORTAL_ASSISTANT_ENTRY_OPEN_TESTID}
        >
          让 AI 帮我选
        </Button>
      </div>
    </Card>
  );
};

export default PortalAssistantEntry;