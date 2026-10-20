/**
 * AI 智能助手工作区（页面壳）。
 *
 * D0-01：会话内核（状态 / SSE / 工具事件 / 确认闭环 / Provider 与 Bot 选择）已抽到
 * `chat-core/use-ai-chat-session`，消息项抽到 `chat-core/ChatMessageItem`；
 * 本文件只保留整页布局锁 + 工作区外壳 JSX（会话历史 / 工具条 / 消息区 / 输入坞）。
 */
import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import {
  Alert,
  Button,
  Dropdown,
  Popconfirm,
  Select,
  Spin,
  Tag,
  Tooltip,
  Typography,
  theme,
} from 'antd';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import {
  Bot,
  ChevronLeft,
  Clock,
  Download,
  Eraser,
  FileText,
  LoaderCircle,
  MessageSquare,
  MoreHorizontal,
  Plus,
  Star,
  StopCircle,
  Trash2,
} from 'lucide-react';

import { useI18n } from '@/lib/i18n/useI18n';
import { askAIEntrypointLabel } from '@/lib/ai/ask-ai-scope';
import BotSelector from './BotSelector';
import { ChatMessageItem } from './chat-core/ChatMessageItem';
import SuggestionCards from './chat-core/SuggestionCards';
import { CONTENT_MAX_WIDTH } from './chat-core/constants';
import { useAIChatSession } from './chat-core/use-ai-chat-session';
import { useInputDockAssist } from './chat-core/use-input-dock-assist';
import { useAssistantStore } from '@/lib/store/assistant-store';
import type { ChatMessage } from './chat-core/types';
import HandoffActionCard from './handoff/HandoffActionCard';
import type { AIAttachmentRef } from '@/lib/api/ai-api';
import { ACCEPT_ATTACHMENT_STRING } from '@/components/common/attachment/AttachmentField';
import {
  CHAT_ATTACHMENT_INPUT_TESTID,
  useChatAttachments,
} from './chat-core/use-chat-attachments';
import AssistantComposer from './dock/AssistantComposer';
import AssistantDiagnosticsEntry, {
  AI_CHAT_DIAGNOSTICS_OPEN_TESTID,
} from './diagnostics/AssistantDiagnosticsEntry';

const { Text } = Typography;

/**
 * 聊天页整页布局锁：把 Header / Content 外壳临时改造成 100vh 纵向 flex 链，
 * 使聊天卡片精确占满剩余高度、输入坞贴底，消息溢出只滚动消息区，页面本身不再出现滚动条。
 *
 * 同时在 <html> 上打 data-ai-chat 标记，启用 buildChatCss 中「隐藏全局页脚 + 去掉内容区
 * 底部内边距」的作用域规则，让聊天卡片直接铺到视口下边缘。
 *
 * 祖先链刻意用内联样式逐层覆盖，而不是写作用域 CSS（如 html.xxx #main-content .page-transition）：
 * 跨层后代选择器在挂载瞬间命中不稳定，会让外壳先按旧高度渲染一帧再跳变；内联样式配合
 * transition:none 可保证首帧即为最终布局（实测首帧卡片高度已等于视口减去 Header 与顶部内边距）。
 * 返回的还原函数在组件卸载时把祖先链恢复原样，因此不会影响其它页面。
 */
const lockChatPageLayout = (): (() => void) => {
  const restores: Array<() => void> = [];
  const patch = (el: Element | null, styles: Record<string, string>) => {
    if (!(el instanceof HTMLElement)) return;
    const saved = Object.keys(styles).map(
      prop => [prop, el.style.getPropertyValue(prop)] as const
    );
    for (const [prop, value] of Object.entries(styles)) el.style.setProperty(prop, value);
    restores.push(() => {
      for (const [prop, prev] of saved) {
        if (prev) el.style.setProperty(prop, prev);
        else el.style.removeProperty(prop);
      }
    });
  };

  const sider = document.querySelector('#root .ant-layout-has-sider');
  const main = document.getElementById('main-content');
  const fillHeight = { height: '100%', 'min-height': '0' };
  const column = { display: 'flex', 'flex-direction': 'column' };
  // 外壳自身带 transition（ant-layout-content 200ms / 降级动画 0.01ms），会让尺寸变化延迟一帧才生效。
  const instant = { transition: 'none' };

  // 外壳锁成 100vh：文档本身不滚动，滚动条只出现在消息区。
  patch(document.documentElement, { height: '100vh', overflow: 'hidden' });
  patch(document.body, { height: '100%', overflow: 'hidden' });
  for (const el of document.querySelectorAll('#root, #root .ant-app, #root .app-root')) {
    patch(el, { height: '100%' });
  }
  patch(sider, fillHeight);
  patch(sider?.querySelector(':scope > .ant-layout') ?? null, fillHeight);

  // Content 与路由过渡层改成纵向 flex 容器，卡片用 flex:1 吃掉 Header 之外的全部高度。
  patch(main, { ...column, ...instant, 'min-height': '0', overflow: 'hidden' });
  patch(main?.querySelector(':scope > .main-content') ?? null, {
    ...column,
    ...instant,
    ...fillHeight,
    flex: '1 1 auto',
  });
  for (const el of main?.querySelectorAll('.page-transition') ?? []) {
    patch(el, { ...column, ...instant, ...fillHeight, flex: '1 1 auto' });
  }

  // 底部留白（16px 内边距 + 48px 页脚）交给作用域 CSS：内联写法会被 MainLayout 在断点
  // 切换时的重渲染清掉，而这里的两处目标都受 React 管理（.main-content 的 padding 简写）。
  document.documentElement.setAttribute('data-ai-chat', '');
  restores.push(() => document.documentElement.removeAttribute('data-ai-chat'));

  return () => {
    for (const restore of restores.reverse()) restore();
  };
};

const AIChat: React.FC = () => {
  const { t } = useI18n();
  const { token } = theme.useToken();
  const [pendingAttachments, setPendingAttachments] = useState<AIAttachmentRef[]>([]);
  const attachmentsRef = useRef<AIAttachmentRef[]>([]);
  const setAttachments = useCallback((next: AIAttachmentRef[]) => {
    attachmentsRef.current = next;
    setPendingAttachments(next);
  }, []);
  const getAttachments = useCallback(() => attachmentsRef.current, []);
  const clearAttachments = useCallback(() => setAttachments([]), [setAttachments]);

  // 整页锁定：挂载时把外壳改造成 100vh 纵向 flex 链，卸载时原样还原（布局提交前同步执行，无跳变）。
  useLayoutEffect(() => {
    return lockChatPageLayout();
  }, []);

  const {
    messages,
    convId,
    streaming,
    query,
    setQuery,
    providerNotice,
    setProviderNotice,
    conversations,
    loadingConvs,
    sidebarOpen,
    setSidebarOpen,
    activeScope,
    setScopeDismissed,
    bots,
    botCapabilities,
    selectedBotId,
    setSelectedBotId,
    feature,
    selectedProvider,
    setSelectedProvider,
    canSwitchProvider,
    canManageProviderPreference,
    providerLabel,
    handleSetPersonalDefault,
    handleClearPersonalDefault,
    canPromoteKnowledge,
    canDeleteConversation,
    scrollRef,
    isEmpty,
    currentConvTitle,
    chatCss,
    formatTime,
    handleSend,
    handleStop,
    handleClear,
    handleKeyDown,
    handleScroll,
    switchToConversation,
    startNewConversation,
    handleDeleteConversation,
    handleExportConversation,
    handlePromoteToArticle,
    handleSaveConversationAsArticle,
    handleOpenApproval,
    handleRequestConfirm,
    openOutcome,
  } = useAIChatSession({
    consumeWorkspaceHandoff: true,
    getAttachments,
    onAttachmentsSent: clearAttachments,
  });

  // composer 复用：附件上传链路与 Dock 共用同一 hook（按钮进工具条、芯片在文本域上方）。
  const attachmentState = useChatAttachments({
    attachments: pendingAttachments,
    onChange: setAttachments,
    disabled: streaming,
  });

  // T5-02：输入坞 `/` 快捷指令 + `@` 当前页对象引用（只填不发）。
  const inputRef = useRef<TextAreaRef>(null);
  const [handoffMessage, setHandoffMessage] = useState<ChatMessage | null>(null);
  // T5-05：Dock 关闭时，预填入口降级到工作区；工作区在此消费一次提示词（只填不发）。
  const pendingPrompt = useAssistantStore(state => state.pendingPrompt);
  const consumePendingPrompt = useAssistantStore(state => state.consumePendingPrompt);
  useEffect(() => {
    if (!pendingPrompt) return;
    const prompt = consumePendingPrompt();
    if (!prompt) return;
    setQuery(prompt);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [consumePendingPrompt, pendingPrompt, setQuery]);

  const inputAssist = useInputDockAssist({
    query,
    setQuery,
    onSessionKeyDown: handleKeyDown,
    inputRef,
  });

  return (
    <div data-ai-chat-page style={{ display: 'flex', gap: 12, flex: '1 1 auto', minHeight: 0 }}>
      {/* 局部 hover 反馈（不新增全局样式文件） */}
      <style>{chatCss}</style>

      {/* 左侧会话历史栏 */}
      <aside
        style={{
          width: sidebarOpen ? 268 : 0,
          flexShrink: 0,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          visibility: sidebarOpen ? 'visible' : 'hidden',
          transition: 'width 0.2s ease',
          background: token.colorFillQuaternary,
          borderRadius: 12,
          border: sidebarOpen ? `1px solid ${token.colorBorderSecondary}` : 'none',
        }}
      >
        <div style={{ width: 268, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '10px 10px 6px', display: 'flex', alignItems: 'center', gap: 6 }}>
            <MessageSquare size={15} color={token.colorTextSecondary} />
            <Text strong style={{ fontSize: 13, flex: 1 }}>
              会话历史
            </Text>
            <Tooltip title="新建会话">
              <Button
                type="text"
                size="small"
                icon={<Plus size={14} />}
                onClick={startNewConversation}
                disabled={streaming}
              />
            </Tooltip>
            <Tooltip title="收起侧栏">
              <Button
                type="text"
                size="small"
                icon={<ChevronLeft size={14} />}
                onClick={() => setSidebarOpen(false)}
              />
            </Tooltip>
          </div>

          <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', paddingBottom: 8 }}>
            {loadingConvs ? (
              <div style={{ textAlign: 'center', padding: 24 }}>
                <Spin size="small" />
              </div>
            ) : conversations.length === 0 ? (
              <div style={{ padding: 16, textAlign: 'center' }}>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  暂无会话记录
                </Text>
              </div>
            ) : (
              conversations.map(conv => (
                <div
                  key={conv.id}
                  className="ai-chat-conv"
                  data-selected={conv.id === convId}
                  onClick={() => switchToConversation(conv)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '8px 10px',
                    margin: '0 6px 2px',
                    cursor: 'pointer',
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <Text style={{ fontSize: 13, display: 'block' }} ellipsis>
                      {conv.title || `会话 #${conv.id}`}
                    </Text>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                      <Clock size={10} color={token.colorTextTertiary} />
                      <Text type="secondary" style={{ fontSize: 11 }}>
                        {formatTime(conv.createdAt)}
                      </Text>
                    </div>
                  </div>
                  {canDeleteConversation && (
                    <Popconfirm
                      title="删除此会话？"
                      onConfirm={e => handleDeleteConversation(e as React.MouseEvent, conv.id)}
                      okText="删除"
                      cancelText="取消"
                      okButtonProps={{ danger: true, size: 'small' }}
                    >
                      <Button
                        type="text"
                        size="small"
                        danger
                        icon={<Trash2 size={12} />}
                        onClick={e => e.stopPropagation()}
                        style={{ opacity: 0.6 }}
                      />
                    </Popconfirm>
                  )}
                  {/* I2-02 合规：导出会话（Markdown 快照；只读，不受 ai:write 门控）。 */}
                  <Tooltip title="导出会话（Markdown）">
                    <Button
                      type="text"
                      size="small"
                      aria-label="导出会话"
                      data-testid={`export-conversation-${conv.id}`}
                      icon={<Download size={12} />}
                      onClick={e => void handleExportConversation(e, conv.id)}
                      style={{ opacity: 0.6 }}
                    />
                  </Tooltip>
                </div>
              ))
            )}
          </div>
        </div>
      </aside>

      {/* 主聊天区：工具条 / 消息滚动区 / 输入坞 */}
      <section
        style={{
          flex: 1,
          minWidth: 0,
          display: 'flex',
          flexDirection: 'column',
          background: token.colorBgContainer,
          borderRadius: 12,
          border: `1px solid ${token.colorBorderSecondary}`,
          overflow: 'hidden',
        }}
      >
        {/* a. 顶部工具条（约 48px，底部 1px 分隔线） */}
        <header
          style={{
            height: 48,
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '0 12px',
            borderBottom: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          {!sidebarOpen ? (
            <Tooltip title="展开会话历史">
              <Button
                type="text"
                size="small"
                icon={<MessageSquare size={15} />}
                onClick={() => setSidebarOpen(true)}
              />
            </Tooltip>
          ) : null}
          <Bot size={18} color={token.colorPrimary} />
          <Text strong style={{ fontSize: 14, whiteSpace: 'nowrap' }}>
            AI 助手
          </Text>
          {currentConvTitle ? (
            <Tag
              style={{
                maxWidth: 200,
                marginInlineEnd: 0,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {currentConvTitle}
            </Tag>
          ) : null}

          <div
            style={{
              marginLeft: 'auto',
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              flexShrink: 0,
            }}
          >
            {/* B2-04 Bot 选择器：已有会话时锁定（切换仅影响新会话，不回溯改写历史归属） */}
            <BotSelector
              bots={bots}
              value={selectedBotId}
              onChange={setSelectedBotId}
              locked={Boolean(convId)}
              disabled={botCapabilities?.botEnabled === false}
              labels={{ disabledHint: t('bots.capabilities.selectorDisabled') }}
            />
            {/* B3-02：入口上下文条（来自页面 launcher；可清除，清除后按普通对话处理） */}
            {activeScope ? (
              <Tag
                color="processing"
                closable
                onClose={e => {
                  e.preventDefault();
                  setScopeDismissed(true);
                }}
                data-testid="ask-ai-scope-chip"
                style={{ marginInlineEnd: 0 }}
              >
                {askAIEntrypointLabel(activeScope.entrypoint)}
                {activeScope.targetType && activeScope.targetId
                  ? ` · ${activeScope.targetType}#${activeScope.targetId}`
                  : ''}
              </Tag>
            ) : null}
            {/* Provider 选择器：feature.enabled && providers.length > 1（P1 起普通用户同样可见） */}
            {canSwitchProvider ? (
              <>
                <Select
                  size="small"
                  style={{ minWidth: 160, maxWidth: 240 }}
                  value={selectedProvider}
                  allowClear
                  placeholder={
                    feature.preference?.effectiveProviderKey
                      ? `跟随默认 · ${providerLabel(feature.preference.effectiveProviderKey)}`
                      : '跟随默认实例'
                  }
                  options={feature.providers.map(p => ({
                    value: p.key,
                    label: p.implemented ? p.displayName : `${p.displayName}（未接入）`,
                    disabled: !p.implemented,
                  }))}
                  onChange={value => {
                    setSelectedProvider(value);
                    setProviderNotice(null);
                  }}
                />
                {/* 写端点（PUT /ai/user-preference）仍需 system:write */}
                {canManageProviderPreference &&
                selectedProvider &&
                feature.preference?.providerKey !== selectedProvider ? (
                  <Tooltip title="设为我的默认（仅影响你自己的新会话）">
                    <Button
                      size="small"
                      type="text"
                      icon={<Star size={13} />}
                      onClick={handleSetPersonalDefault}
                    />
                  </Tooltip>
                ) : null}
                {canManageProviderPreference && feature.preference?.providerKey ? (
                  <Tooltip
                    title={`清除我的默认（当前：${providerLabel(feature.preference.providerKey)}）`}
                  >
                    <Button size="small" type="text" onClick={handleClearPersonalDefault}>
                      清除默认
                    </Button>
                  </Tooltip>
                ) : null}
              </>
            ) : null}

            {streaming ? (
              <Tag color="processing" icon={<LoaderCircle size={12} className="animate-spin" />}>
                生成中
              </Tag>
            ) : null}
            {streaming ? (
              <Button size="small" danger icon={<StopCircle size={14} />} onClick={handleStop}>
                停止生成
              </Button>
            ) : null}
            {/* O2-02：会话诊断入口（与 Dock 面板共用同一实现；工作区入口独立 testid 免同页歧义）。 */}
            <AssistantDiagnosticsEntry
              testId={AI_CHAT_DIAGNOSTICS_OPEN_TESTID}
              messages={messages}
              conversationId={convId ?? undefined}
              streaming={streaming}
              pageKey="ai.chat"
            />
            {/*
              低频动作收进「更多」：保存成文章 / 清空对话都是整段会话级操作，
              不需要常驻在 48px 工具条里（改前 1024px 视口实测溢出 17px）。
            */}
            <Dropdown
              trigger={['click']}
              placement="bottomRight"
              menu={{
                items: [
                  ...(canPromoteKnowledge
                    ? [
                        {
                          key: 'save-article',
                          icon: <FileText size={14} />,
                          label: '保存成文章',
                          disabled: !convId || streaming || isEmpty,
                          onClick: handleSaveConversationAsArticle,
                        },
                      ]
                    : []),
                  {
                    key: 'clear',
                    icon: <Eraser size={14} />,
                    label: '清空对话',
                    danger: true,
                    disabled: streaming,
                    onClick: handleClear,
                  },
                ],
              }}
            >
              <Tooltip title="更多操作">
                <Button
                  data-testid="ai-chat-more-actions"
                  size="small"
                  type="text"
                  icon={<MoreHorizontal size={16} />}
                  aria-label="更多操作"
                  aria-haspopup="menu"
                />
              </Tooltip>
            </Dropdown>
          </div>
        </header>

        {/* b. 消息滚动区（ref 绑在真正的滚动容器上）。
            T5-08：消息区即 live region（role=log + polite）——流式期间 aria-busy 抑制逐字播报，
            生成结束置 false 后由读屏一次性读出完整回答。 */}
        <div
          ref={scrollRef}
          onScroll={handleScroll}
          role="log"
          aria-label="AI 助手会话消息"
          aria-live="polite"
          aria-busy={streaming}
          style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '20px 20px 8px' }}
        >
          {isEmpty ? (
            <div
              style={{
                maxWidth: CONTENT_MAX_WIDTH,
                margin: '0 auto',
                paddingTop: 32,
                textAlign: 'center',
              }}
            >
              <div
                style={{
                  width: 56,
                  height: 56,
                  margin: '0 auto',
                  borderRadius: '50%',
                  background: token.colorPrimaryBg,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <Bot size={28} color={token.colorPrimary} />
              </div>
              <div style={{ fontSize: 18, fontWeight: 600, color: token.colorTextHeading, marginTop: 12 }}>
                AI 助手
              </div>
              <div style={{ fontSize: 13, color: token.colorTextSecondary, marginTop: 4 }}>
                结合知识库为你回答，并给出引用来源。选择左侧会话可继续历史对话。
              </div>
              <div
                style={{ marginTop: 28 }}
              >
                {/* D0-05：页面级建议卡（工作区页面注册为 ai.chat，未注册回退通用建议）。 */}
                <SuggestionCards onSend={handleSend} disabled={streaming} />
              </div>
            </div>
          ) : (
            <div
              style={{
                maxWidth: CONTENT_MAX_WIDTH,
                margin: '0 auto',
                display: 'flex',
                flexDirection: 'column',
                gap: 22,
              }}
            >
              {messages.map(item => (
                <ChatMessageItem
                  key={item.id}
                  message={item}
                  providerLabel={providerLabel}
                  onCreateArticle={handlePromoteToArticle}
                  onOpenApproval={handleOpenApproval}
                  onOpenOutcome={openOutcome}
                  // B1-07：对话内确认（确认抽屉）。入口只在确认单仍 pending 且未过期时出现。
                  onRequestConfirm={handleRequestConfirm}
                  onHandoff={setHandoffMessage}
                />
              ))}
            </div>
          )}
        </div>

        {/* c. 输入坞（与消息列同宽居中） */}
        <div
          style={{
            flexShrink: 0,
            padding: '10px 16px 8px',
            borderTop: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          {providerNotice ? (
            <Alert
              type="warning"
              showIcon
              closable
              message={providerNotice}
              onClose={() => setProviderNotice(null)}
              style={{ maxWidth: CONTENT_MAX_WIDTH, margin: '0 auto 8px' }}
            />
          ) : null}

          <div style={{ maxWidth: CONTENT_MAX_WIDTH, margin: '0 auto' }}>
            {/*
              composer 复用（2026-10-20）：与 Dock 共用同一输入坞实现。
              改前工作区有两处不一致：附件选择器独占输入框上方一行（左边缘还比输入框偏 15px），
              且输入条与消息列宽度不同源。改后：附件进工具条、芯片在文本域上方、单一容器。
            */}
            <AssistantComposer
              testId="assistant-workspace-composer"
              inputTestId="assistant-chat-input"
              inputFontSize={14}
              maxRows={8}
              widePanel
              query={query}
              setQuery={setQuery}
              onSend={() => handleSend()}
              onStop={handleStop}
              streaming={streaming}
              inputRef={inputRef}
              inputAssist={inputAssist}
              attachments={pendingAttachments}
              attachmentInput={
                <input
                  ref={attachmentState.inputRef}
                  type="file"
                  multiple
                  accept={ACCEPT_ATTACHMENT_STRING}
                  style={{ display: 'none' }}
                  data-testid={CHAT_ATTACHMENT_INPUT_TESTID}
                  onChange={attachmentState.onInputChange}
                />
              }
              onOpenAttachment={attachmentState.openPicker}
              attachmentUploading={attachmentState.uploading}
              attachmentError={attachmentState.error}
              attachmentDisabled={streaming}
              onRemoveAttachment={attachmentState.remove}
            />
            <div
              style={{
                fontSize: 12,
                color: token.colorTextSecondary,
                textAlign: 'center',
                marginTop: 6,
              }}
            >
              Enter 发送，Shift+Enter 换行 · 回答由 AI 生成，请自行甄别
            </div>
          </div>
        </div>
      </section>
      <HandoffActionCard
        open={handoffMessage !== null}
        message={handoffMessage}
        onClose={() => setHandoffMessage(null)}
      />
    </div>
  );
};

export default AIChat;
