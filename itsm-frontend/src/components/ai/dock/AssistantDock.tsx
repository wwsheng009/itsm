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
import { useLocation, useNavigate } from 'react-router';
import { Button, Popover, Switch, Tag, Tooltip, theme } from 'antd';
import { Bot, Maximize2, SlidersHorizontal, X } from 'lucide-react';
import { useHotkeys } from 'react-hotkeys-hook';

import { usePermissions } from '@/lib/hooks/use-permissions';
import { askAITargetTypeLabel } from '@/lib/ai/ask-ai-scope';
import type { ProactiveSignal } from '@/lib/ai/proactive/signals';
import { useProactiveSignals } from '@/lib/ai/proactive/use-proactive-signals';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import {
  ASSISTANT_DOCK_PUSH_THRESHOLD,
  ASSISTANT_WIDTH_TIERS,
  normalizeAssistantWidth,
  useAssistantStore,
} from '@/lib/store/assistant-store';
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
  const location = useLocation();
  const { hasPermission } = usePermissions();
  const { token } = theme.useToken();
  const open = useAssistantStore(state => state.open);
  const width = useAssistantStore(state => state.width);
  const setWidth = useAssistantStore(state => state.setWidth);
  const cycleWidth = useAssistantStore(state => state.cycleWidth);
  const togglePanel = useAssistantStore(state => state.togglePanel);
  const closePanel = useAssistantStore(state => state.closePanel);
  const openPanel = useAssistantStore(state => state.openPanel);
  const setPendingPrompt = useAssistantStore(state => state.setPendingPrompt);
  // D0-03/D0-05：页面感知（解析声明 + 记录 lastRoute + 面板上下文条）。
  const { pageKey, declaration } = usePageAssistantContext();
  // D1-02：L2 运行时数据（详情页显式注册）→ 上下文条展示具体对象（用户可见）。
  const pageData = useAssistantPageData();
  const canReadAssistant = hasPermission('ai', 'read');
  // P0：AI 工作区（/ai/chat）本身就是全宽会话界面，再挂 Dock 会出现两套聊天并存。
  // 路由级直接不渲染（含 FAB），避免重复入口与重复会话请求。
  // 必须在所有依赖它的 hook（信号聚合/快捷键）之前定义。
  const isWorkspaceRoute = location.pathname === '/ai/chat';
  // I1-03：主动协助偏好（持久化 UI 偏好；默认静默）。
  const proactiveEnabled = useAssistantProactiveStore(state => state.enabled);
  const proactiveSilent = useAssistantProactiveStore(state => state.silent);
  const dismissedScopes = useAssistantProactiveStore(state => state.dismissedScopes);
  const setProactiveEnabled = useAssistantProactiveStore(state => state.setEnabled);
  const setProactiveSilent = useAssistantProactiveStore(state => state.setSilent);
  const dismissScope = useAssistantProactiveStore(state => state.dismissScope);
  const restoreDismissed = useAssistantProactiveStore(state => state.restoreDismissed);
  // I1-03：只读信号聚合（无权限/总开关关闭/工作区路由 → 零请求）。
  // P0 补充：/ai/chat 不渲染 Dock，但仍挂载本组件；若不关掉信号聚合，
  // 工作区会平白轮询审批/通知，属于重复请求。
  const { badgeCount, suggestion } = useProactiveSignals(canReadAssistant && !isWorkspaceRoute);
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
  // a11y：面板关闭后把焦点还给 FAB（否则键盘用户会掉回 body，丢失当前位置）。
  const fabRef = useRef<HTMLButtonElement | HTMLAnchorElement>(null);
  const wasOpenRef = useRef(open);

  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < MOBILE_BREAKPOINT);
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // P1：视口变化后把持久化宽度收敛到当前可用档位（例如从 1440 缩到 1024 时 720 不可用）。
  // 只在值真的变化时写入，避免每次 resize 都触发一次 persist 写盘。
  useEffect(() => {
    if (typeof window === 'undefined' || isMobile) return;
    const normalized = normalizeAssistantWidth(width, window.innerWidth);
    if (normalized !== width) setWidth(normalized);
  }, [width, isMobile, setWidth]);

  useEffect(() => {
    const wasOpen = wasOpenRef.current;
    wasOpenRef.current = open;
    if (!wasOpen || open) return;
    if (typeof document === 'undefined') return;
    const active = document.activeElement as HTMLElement | null;
    // 只在"焦点因面板卸载而丢失"时归还：用户主动点了其它控件（active 仍在文档中）则不抢焦点。
    const lostFocus = !active || active === document.body || !active.isConnected;
    if (!lostFocus) return;
    const raf = window.requestAnimationFrame(() => fabRef.current?.focus());
    return () => window.cancelAnimationFrame(raf);
  }, [open]);

  // Ctrl/Cmd+I：唤起/收起（输入框内同样生效）；ESC：收起（面板打开时）。
  // 工作区路由下 Dock 不渲染，快捷键同样禁用，避免"按了没反应但状态被改"。
  useHotkeys('ctrl+i, cmd+i', togglePanel, {
    preventDefault: true,
    enableOnFormTags: true,
    enabled: !isWorkspaceRoute,
  });
  useHotkeys(
    'esc',
    () => {
      if (open) closePanel();
    },
    { enabled: open && !isWorkspaceRoute, enableOnFormTags: true }
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

  // P2：宽档位起推挤主内容（避免 fixed 面板遮挡表格右侧操作列）。
  // 用 CSS 变量而不是逐层传 props：MainLayout 只需读取一次，页面组件零改动。
  // 注意：该 effect 必须在任何 early return 之前——否则 hooks 顺序不稳定（rules-of-hooks）。
  useEffect(() => {
    if (typeof document === 'undefined') return;
    const root = document.documentElement;
    // 工作区路由必须为 0：Dock 不渲染，若沿用持久化的 open=true 会给工作区白加右内边距。
    const shouldPush =
      open && !isMobile && !isWorkspaceRoute && width >= ASSISTANT_DOCK_PUSH_THRESHOLD;
    root.style.setProperty('--assistant-dock-push', shouldPush ? `${width}px` : '0px');
    return () => {
      root.style.setProperty('--assistant-dock-push', '0px');
    };
  }, [open, isMobile, isWorkspaceRoute, width]);

  if (!canReadAssistant || isWorkspaceRoute) return null;

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

  // P1：宽度档位循环（窄 → 宽 → 超宽 → 窄），按当前视口自动跳过放不下的档位。
  const tierIndex = ASSISTANT_WIDTH_TIERS.indexOf(width as (typeof ASSISTANT_WIDTH_TIERS)[number]);
  const widthLabel = tierIndex >= 0 ? ['标准宽度', '加宽', '超宽'][tierIndex] : '调整宽度';
  const handleCycleWidth = () => {
    if (typeof window === 'undefined') return;
    const next = cycleWidth(window.innerWidth);
    trackAssistantEvent('assistant_opened', {
      pageKey,
      source: 'width_toggle',
      extra: { width: next },
    });
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
                ref={fabRef}
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
          data-width={width}
          role="dialog"
          aria-label="AI 助手面板"
          style={{
            position: 'fixed',
            top: 64,
            right: 0,
            bottom: 0,
            width: isMobile ? '100%' : width,
            // P1：极端窄视口下兜底，保证面板永不把整页遮死（与 store 的档位收敛同口径）。
            maxWidth: isMobile ? '100%' : 'calc(100vw - 320px)',
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
            {/* P1：宽度三档循环（400 → 560 → 720），键盘可达；移动端不展示（已是全宽）。 */}
            {!isMobile ? (
              <Tooltip title={`${widthLabel}（${width}px）`}>
                <Button
                  data-testid="assistant-width-toggle"
                  type="text"
                  size="small"
                  icon={<Maximize2 size={14} />}
                  onClick={handleCycleWidth}
                  aria-label={`调整 AI 助手宽度（当前 ${width}px）`}
                  aria-pressed={width > ASSISTANT_WIDTH_TIERS[0]}
                />
              </Tooltip>
            ) : null}
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