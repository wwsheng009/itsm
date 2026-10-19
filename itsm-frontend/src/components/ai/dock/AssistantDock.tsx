/**
 * D0-02 AssistantDock：常驻 AI 助手容器（FAB ↔ 右侧面板 / 移动端底部抽屉）。
 *
 * 验收（方案 §7 D0-02；I1-03 主动协助）：
 * - 168 路由可开合：挂载于 MainLayout，任意页面可用；`Ctrl/Cmd+I` 唤起/收起，ESC 收起；
 * - 无 `ai:read` 不渲染（入口级门禁，与 Header 的 AI 入口同码）；
 * - 移动端（<768px）为底部全宽面板；桌面端为右侧面板（宽度取自 store，可持久化）；
 * - 懒挂载：面板未打开时不挂载聊天视图（零会话/候选请求）；
 * - I1-03 主动协助（§3.6，非打断三形态）：FAB 徽标（待审批 + 高价值通知计数）、
 *   页面内联建议条（静默模式默认开启 → 不出现；可关闭/「不再提示」记忆）、
 *   通知订阅（复用 `notificationWS` 单例只读订阅 + 未读高价值通知聚合）。
 */
import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { Button, Popover, Switch, Tag, Tooltip, theme } from 'antd';
import { Bot, SlidersHorizontal, X } from 'lucide-react';
import { useHotkeys } from 'react-hotkeys-hook';

import { usePermissions } from '@/lib/hooks/use-permissions';
import { askAITargetTypeLabel } from '@/lib/ai/ask-ai-scope';
import type { ProactiveSignal } from '@/lib/ai/proactive/signals';
import { useProactiveSignals } from '@/lib/ai/proactive/use-proactive-signals';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { useAssistantProactiveStore } from '@/lib/store/assistant-proactive-store';
import { useAssistantPageData } from '@/lib/store/assistant-context-store';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import AssistantChatPanel from './AssistantChatPanel';
import ProactiveSuggestionBar from './ProactiveSuggestionBar';
import { countRunningTasks, countUnreadTasks, useAssistantAsyncStore } from '@/lib/ai/async/assistant-async-store';

export const ASSISTANT_DOCK_TESTID = 'assistant-dock';
export const ASSISTANT_FAB_TESTID = 'assistant-fab';
export const ASSISTANT_PANEL_TESTID = 'assistant-panel';
export const ASSISTANT_FAB_BADGE_TESTID = 'assistant-fab-badge';
/** V2-02：FAB 运行中状态点（后台精算进行中；与未读回执徽标共同构成「运行中/未读回执」状态点）。 */
export const ASSISTANT_FAB_RUNNING_TESTID = 'assistant-fab-running';

const MOBILE_BREAKPOINT = 768;

const AssistantDock: React.FC = () => {
  const navigate = useNavigate();
  const { hasPermission } = usePermissions();
  const { token } = theme.useToken();
  const open = useAssistantStore(state => state.open);
  const width = useAssistantStore(state => state.width);
  const togglePanel = useAssistantStore(state => state.togglePanel);
  const closePanel = useAssistantStore(state => state.closePanel);
  const openPanel = useAssistantStore(state => state.openPanel);
  const setPendingPrompt = useAssistantStore(state => state.setPendingPrompt);
  // D0-03/D0-05：页面感知（解析声明 + 记录 lastRoute + 面板上下文条）。
  const { pageKey, declaration } = usePageAssistantContext();
  // D1-02：L2 运行时数据（详情页显式注册）→ 上下文条展示具体对象（用户可见）。
  const pageData = useAssistantPageData();
  const canReadAssistant = hasPermission('ai', 'read');
  // I1-03：主动协助偏好（持久化 UI 偏好；默认静默）。
  const proactiveEnabled = useAssistantProactiveStore(state => state.enabled);
  const proactiveSilent = useAssistantProactiveStore(state => state.silent);
  const dismissedScopes = useAssistantProactiveStore(state => state.dismissedScopes);
  const setProactiveEnabled = useAssistantProactiveStore(state => state.setEnabled);
  const setProactiveSilent = useAssistantProactiveStore(state => state.setSilent);
  const dismissScope = useAssistantProactiveStore(state => state.dismissScope);
  const restoreDismissed = useAssistantProactiveStore(state => state.restoreDismissed);
  // I1-03：只读信号聚合（无权限/总开关关闭 → 零请求）。
  const { badgeCount, suggestion } = useProactiveSignals(canReadAssistant);
  // V2-02：异步精算任务状态（运行中点 + 未读回执并入 FAB 徽标；面板打开即已读）。
  const asyncTasks = useAssistantAsyncStore(state => state.tasks);
  const asyncRunningCount = countRunningTasks(asyncTasks);
  const asyncUnreadCount = countUnreadTasks(asyncTasks);
  const fabBadgeCount = badgeCount + asyncUnreadCount;
  // 本次会话内点关闭的建议（不写记忆；刷新页面即失效）。
  const [sessionDismissedScopes, setSessionDismissedScopes] = useState<string[]>([]);
  const reportedSuggestionIds = useRef<Set<string>>(new Set());
  const [isMobile, setIsMobile] = useState(
    () => typeof window !== 'undefined' && window.innerWidth < MOBILE_BREAKPOINT
  );

  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < MOBILE_BREAKPOINT);
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // Ctrl/Cmd+I：唤起/收起（输入框内同样生效）；ESC：收起（面板打开时）。
  useHotkeys('ctrl+i, cmd+i', togglePanel, { preventDefault: true, enableOnFormTags: true });
  useHotkeys(
    'esc',
    () => {
      if (open) closePanel();
    },
    { enabled: open, enableOnFormTags: true }
  );

  // 内联建议条可见性：静默模式/总开关/面板展开/已忽略 → 隐藏（suggestion 已按忽略过滤，二次防御）。
  const suggestionVisible =
    Boolean(suggestion) &&
    !open &&
    !dismissedScopes.includes(suggestion?.scopeKey ?? '') &&
    !sessionDismissedScopes.includes(suggestion?.scopeKey ?? '');
  const suggestionId = suggestion?.id;
  useEffect(() => {
    if (!suggestionVisible || !suggestionId) return;
    if (reportedSuggestionIds.current.has(suggestionId)) return;
    reportedSuggestionIds.current.add(suggestionId);
    trackAssistantEvent('proactive_shown', {
      pageKey,
      source: suggestion?.kind ?? 'page_suggestion',
      extra: { signal: suggestionId },
    });
    // 依赖仅取稳定标识：同一建议重开面板不重复上报。
  }, [suggestionVisible, suggestionId, pageKey, suggestion?.kind]);

  if (!canReadAssistant) return null;

  const chipLabel = declaration
    ? pageData?.targetId
      ? `${declaration.title} · ${askAITargetTypeLabel(pageData.targetType)} #${pageData.targetId}`
      : declaration.title
    : null;

  const handleProactiveAction = (signal: ProactiveSignal) => {
    trackAssistantEvent('proactive_clicked', {
      pageKey,
      source: signal.kind,
      extra: { signal: signal.id },
    });
    if (signal.action.type === 'navigate') {
      navigate(signal.action.route);
      return;
    }
    // 非打断：只预填 + 打开面板，不自动发送。
    setPendingPrompt(signal.action.prompt);
    openPanel();
  };

  const handleProactiveDismiss = (signal: ProactiveSignal) => {
    trackAssistantEvent('proactive_dismissed', {
      pageKey,
      source: 'session',
      extra: { signal: signal.id },
    });
    setSessionDismissedScopes(prev =>
      prev.includes(signal.scopeKey) ? prev : [...prev, signal.scopeKey]
    );
  };

  const handleProactiveDismissForever = (signal: ProactiveSignal) => {
    trackAssistantEvent('proactive_dismissed', {
      pageKey,
      source: 'forever',
      extra: { signal: signal.id },
    });
    dismissScope(signal.scopeKey);
  };

  return (
    <div data-testid={ASSISTANT_DOCK_TESTID} data-page-key={pageKey}>
      {!open ? (
        <>
          <span
            data-testid="assistant-fab-wrap"
            style={{
              position: 'fixed',
              // V1-02 移动端入口覆盖：FAB 在小屏贴近拇指并避让底部安全区（刘海屏/手势条）。
              right: isMobile ? 16 : 24,
              bottom: isMobile ? 'calc(16px + env(safe-area-inset-bottom, 0px))' : 24,
              zIndex: 980,
              display: 'inline-flex',
            }}
          >
            <Tooltip
              title={
                fabBadgeCount > 0 ? `AI 助手（Ctrl+I）· ${fabBadgeCount} 项待处理` : 'AI 助手（Ctrl+I）'
              }
            >
              <Button
                data-testid={ASSISTANT_FAB_TESTID}
                type="primary"
                shape="circle"
                aria-label="AI 助手"
                onClick={togglePanel}
                style={{
                  width: 48,
                  height: 48,
                  boxShadow: token.boxShadowSecondary,
                }}
              >
                <Bot size={20} />
              </Button>
            </Tooltip>
            {/* I1-03 徽标：待办类信号计数（非打断；点击 FAB 进入面板查看）。 */}
            {fabBadgeCount > 0 ? (
              <span
                data-testid={ASSISTANT_FAB_BADGE_TESTID}
                aria-label={`${fabBadgeCount} 项 AI 待处理`}
                style={{
                  position: 'absolute',
                  top: -4,
                  right: -4,
                  minWidth: 18,
                  height: 18,
                  padding: '0 4px',
                  borderRadius: 9,
                  background: token.colorError,
                  color: '#fff',
                  fontSize: 11,
                  lineHeight: '18px',
                  textAlign: 'center',
                  boxSizing: 'border-box',
                }}
              >
                {fabBadgeCount > 9 ? '9+' : fabBadgeCount}
              </span>
            ) : null}
            {/* V2-02 状态点：后台精算运行中（呼吸点；收口后转为上面的未读回执徽标）。 */}
            {asyncRunningCount > 0 ? (
              <span
                data-testid={ASSISTANT_FAB_RUNNING_TESTID}
                aria-label={`${asyncRunningCount} 项后台精算进行中`}
                style={{
                  position: 'absolute',
                  top: -2,
                  right: -2,
                  width: 10,
                  height: 10,
                  borderRadius: '50%',
                  background: token.colorPrimary,
                  border: `2px solid ${token.colorBgContainer}`,
                  boxSizing: 'border-box',
                }}
              />
            ) : null}
          </span>
          {suggestionVisible && suggestion ? (
            <ProactiveSuggestionBar
              signal={suggestion}
              onAction={handleProactiveAction}
              onDismiss={handleProactiveDismiss}
              onDismissForever={handleProactiveDismissForever}
            />
          ) : null}
        </>
      ) : (
        <section
          data-testid={ASSISTANT_PANEL_TESTID}
          // V1-02：移动端形态标记（<768px 为底部全宽抽屉），供样式与测试断言。
          data-mobile={isMobile ? 'true' : 'false'}
          role="dialog"
          aria-label="AI 助手面板"
          style={{
            position: 'fixed',
            top: 64,
            right: 0,
            bottom: 0,
            width: isMobile ? '100%' : width,
            zIndex: 981,
            display: 'flex',
            flexDirection: 'column',
            background: token.colorBgContainer,
            borderLeft: isMobile ? 'none' : `1px solid ${token.colorBorderSecondary}`,
            // V1-02 移动端 Bottom Sheet：圆角顶 + 安全区 padding，与通知抽屉/全局搜索的层级关系保持 981。
            borderTopLeftRadius: isMobile ? 16 : 0,
            borderTopRightRadius: isMobile ? 16 : 0,
            paddingBottom: isMobile ? 'env(safe-area-inset-bottom, 0px)' : undefined,
            boxShadow: token.boxShadowSecondary,
          }}
        >
          <header
            style={{
              height: 44,
              flexShrink: 0,
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '0 10px 0 12px',
              borderBottom: `1px solid ${token.colorBorderSecondary}`,
            }}
          >
            <Bot size={16} color={token.colorPrimary} />
            <span style={{ fontWeight: 600, fontSize: 13 }}>AI 助手</span>
            {/* D0-05 上下文条：当前页面（来自注册表声明）。 */}
            {chipLabel ? (
              <Tag
                data-testid="assistant-context-chip"
                color="processing"
                title={pageData?.summary ?? chipLabel}
                style={{
                  marginInlineEnd: 0,
                  maxWidth: 160,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {chipLabel}
              </Tag>
            ) : null}
            <span style={{ flex: 1 }} />
            {/* I1-03：主动协助设置（默认静默；可恢复已忽略提示）。 */}
            <Popover
              trigger="click"
              placement="bottomRight"
              title="主动协助"
              content={
                <div style={{ width: 248, display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      gap: 8,
                    }}
                  >
                    <span style={{ fontSize: 12 }}>启用主动协助</span>
                    <span data-testid="assistant-proactive-enabled">
                      <Switch
                        size="small"
                        checked={proactiveEnabled}
                        onChange={setProactiveEnabled}
                        aria-label="启用主动协助"
                      />
                    </span>
                  </div>
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      gap: 8,
                    }}
                  >
                    <span style={{ fontSize: 12 }}>静默模式</span>
                    <span data-testid="assistant-proactive-silent">
                      <Switch
                        size="small"
                        checked={proactiveSilent}
                        disabled={!proactiveEnabled}
                        onChange={setProactiveSilent}
                        aria-label="静默模式"
                      />
                    </span>
                  </div>
                  <div style={{ fontSize: 11, color: token.colorTextSecondary }}>
                    静默模式：只显示徽标与通知中心投递，不出现页面内建议条（默认开启）。
                  </div>
                  <Button
                    data-testid="assistant-proactive-restore"
                    size="small"
                    type="link"
                    disabled={dismissedScopes.length === 0}
                    onClick={restoreDismissed}
                    style={{ padding: 0, alignSelf: 'flex-start' }}
                  >
                    恢复已忽略提示（{dismissedScopes.length}）
                  </Button>
                  {/* 通知订阅/退订复用现有通知偏好（/profile），助手侧不重复实现。 */}
                  <Button
                    data-testid="assistant-proactive-notification-settings"
                    size="small"
                    type="link"
                    onClick={() => navigate('/profile')}
                    style={{ padding: 0, alignSelf: 'flex-start' }}
                  >
                    通知订阅设置（通知中心投递）
                  </Button>
                </div>
              }
            >
              <Button
                data-testid="assistant-proactive-settings"
                type="text"
                size="small"
                icon={<SlidersHorizontal size={14} />}
                aria-label="主动协助设置"
              />
            </Popover>
            <Button
              data-testid="assistant-close"
              type="text"
              size="small"
              icon={<X size={14} />}
              onClick={closePanel}
              aria-label="收起 AI 助手"
            />
          </header>
          <AssistantChatPanel />
        </section>
      )}
    </div>
  );
};

export default AssistantDock;