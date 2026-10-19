/**
 * D0-02 AssistantDock 聊天视图（紧凑面板）。
 *
 * 与工作区（AIChat.tsx）共用 `useAIChatSession` + `ChatMessageItem`：
 * - 不展示会话历史侧栏与 Provider/Bot 工具条（紧凑形态；能力切换留在工作区）；
 * - 懒挂载：仅面板打开时挂载本组件，因此未展开时零会话/候选请求（见 AssistantDock 注释）。
 */
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Input, Popover, Segmented, Spin, Tooltip, theme } from 'antd';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import { ExternalLink, History, Plus, Send, Sparkles, StopCircle } from 'lucide-react';
import { useNavigate } from 'react-router';

import { useAIChatSession } from '@/components/ai/chat-core/use-ai-chat-session';
import { ChatMessageItem } from '@/components/ai/chat-core/ChatMessageItem';
import SuggestionCards from '@/components/ai/chat-core/SuggestionCards';
import { ASSISTANT_DOCK_PUSH_THRESHOLD, useAssistantStore } from '@/lib/store/assistant-store';
import { buildAskAIState } from '@/lib/ai/ask-ai-scope';
import { startAssistantAsyncAnswer } from '@/lib/ai/async/assistant-async-store';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import AssistantHelpPanel from './AssistantHelpPanel';
import AssistantDiagnosticsEntry from '@/components/ai/diagnostics/AssistantDiagnosticsEntry';
import AssistantMemoryChips from './AssistantMemoryChips';
import AssistantAsyncAnswerCard from './AssistantAsyncAnswerCard';
import AssistantInteractionBanner from './AssistantInteractionBanner';
import { deriveInteractionState } from '@/lib/ai/interaction-state';

const AssistantChatPanel: React.FC = () => {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const navigate = useNavigate();
  const { pageKey: assistantPageKey, route: assistantRoute } = usePageAssistantContext();
  const {
    messages,
    convId,
    streaming,
    conversations,
    loadingConvs,
    currentConvTitle,
    formatTime,
    switchToConversation,
    startNewConversation,
    query,
    setQuery,
    scrollRef,
    isEmpty,
    chatCss,
    providerLabel,
    providerNotice,
    setProviderNotice,
    handleSend,
    handleStop,
    handleKeyDown,
    handlePromoteToArticle,
    handleOpenApproval,
    handleRequestConfirm,
    openOutcome,
    effectiveScope,
  } = useAIChatSession();

  // P1：宽档位（≥560px）放宽输入行数；与 AssistantDock 的推挤阈值同源，避免两处漂移。
  const panelWidth = useAssistantStore(state => state.width);
  const widePanel = panelWidth >= ASSISTANT_DOCK_PUSH_THRESHOLD;
  const closePanel = useAssistantStore(state => state.closePanel);
  const setHandoffConversationId = useAssistantStore(state => state.setHandoffConversationId);

  /**
   * P3：把当前 Dock 会话交给 `/ai/chat` 工作区。
   *
   * 会话 id 是内核的组件内 state，直接导航会得到空会话；因此先写入一次性交接 id，
   * 由工作区挂载时消费。流式生成中禁用（切页会中断 SSE 并丢失半截回答）。
   */
  const handleOpenWorkspace = useCallback(() => {
    if (!convId || streaming) return;
    setHandoffConversationId(convId);
    closePanel();
    // P3：把 Dock 当前的入口上下文（页面 launcher / L2 目标）一并交给工作区，
    // 否则跳过去后 target/summary 丢失，后续提问会脱离当前对象。
    navigate('/ai/chat', {
      state: effectiveScope && effectiveScope.entrypoint !== 'chat'
        ? buildAskAIState(effectiveScope)
        : undefined,
    });
  }, [closePanel, convId, effectiveScope, navigate, setHandoffConversationId, streaming]);

  const workspaceHint = streaming
    ? '生成中，完成后可在工作区继续'
    : convId
      ? '在工作区打开（继续当前会话）'
      : '发送第一条消息后可在工作区继续';

  // 会话历史下拉：与工作区共用内核的列表/切换动作（同一套数据与竞态保护）。
  const [historyOpen, setHistoryOpen] = useState(false);
  const handleSwitchConversation = useCallback(
    (conv: (typeof conversations)[number]) => {
      if (streaming) return;
      setHistoryOpen(false);
      void switchToConversation(conv);
    },
    [streaming, switchToConversation]
  );
  const handleNewConversation = useCallback(() => {
    if (streaming) return;
    setHistoryOpen(false);
    startNewConversation();
  }, [startNewConversation, streaming]);

  // I0-01：从现有会话信号推导交互状态（零额外请求）。
  // 只取"已知有错误"与"正在跑工具"两个可判定信号；确认/审批态由消息卡片自身呈现。
  const lastAssistant = [...messages].reverse().find(m => m.role === 'assistant');
  const runningTool =
    lastAssistant?.toolEvents?.filter(e => e.status === 'running').slice(-1)[0]?.tool ?? null;
  const interactionState = deriveInteractionState({
    panelOpen: true,
    contextReady: true,
    hasMessages: messages.length > 0,
    streaming,
    runningTool,
    hasError: Boolean(lastAssistant?.error),
    runStatus: lastAssistant?.run?.status === 'failed' ? 'failed' : null,
  });

  // H0-04：面板页签（对话 / 帮助）；"生成初稿"切回对话并发送结构化提示。
  const [tab, setTab] = useState<'chat' | 'help'>('chat');

  // P1：面板打开即聚焦输入框（少一次点击）。桌面端延迟到下一帧，避免与打开动画抢布局；
  // 移动端不自动聚焦——会立即弹起软键盘、遮挡刚打开的面板。
  const inputRef = useRef<TextAreaRef>(null);
  useEffect(() => {
    if (tab !== 'chat') return;
    if (typeof window === 'undefined' || window.innerWidth < 768) return;
    const raf = window.requestAnimationFrame(() => inputRef.current?.focus());
    return () => window.cancelAnimationFrame(raf);
  }, [tab]);
  const handleDraftFromHelp = useCallback(
    (prompt: string) => {
      setTab('chat');
      handleSend(prompt);
    },
    [handleSend]
  );

  // I0-03：消费全局搜索「问 AI」/划词等入口的预填（只填不发送；聚焦输入框）。
  const pendingPrompt = useAssistantStore(state => state.pendingPrompt);
  const consumePendingPrompt = useAssistantStore(state => state.consumePendingPrompt);
  useEffect(() => {
    if (!pendingPrompt) return;
    const prompt = consumePendingPrompt();
    if (!prompt) return;
    setQuery(prompt);
    window.setTimeout(() => {
      document
        .querySelector<HTMLTextAreaElement>('textarea[data-testid="assistant-panel-input"]')
        ?.focus();
    }, 0);
  }, [pendingPrompt, consumePendingPrompt, setQuery]);

  // V2-01：记忆 chip 点击只预填输入框（绝不自动发送）；与 pendingPrompt 相同的聚焦路径。
  const handleInsertMemory = useCallback(
    (text: string) => {
      setQuery(text);
      window.setTimeout(() => {
        document
          .querySelector<HTMLTextAreaElement>('textarea[data-testid="assistant-panel-input"]')
          ?.focus();
      }, 0);
    },
    [setQuery]
  );

  // V2-02：后台精算（先给可用答案 + 完成后回执/通知）；发起失败如实提示，不静默假装。
  const handleStartAsync = useCallback(async () => {
    const prompt = query.trim();
    if (!prompt) return;
    const task = await startAssistantAsyncAnswer({
      prompt,
      pageKey: assistantPageKey && assistantPageKey !== 'unknown' ? assistantPageKey : undefined,
      route: assistantRoute,
    });
    if (!task) message.error('发起后台精算失败，请稍后重试');
  }, [query, assistantPageKey, assistantRoute, message]);

  return (
    <div
      data-testid="assistant-chat-panel"
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        background: token.colorBgContainer,
      }}
    >
      <style>{chatCss}</style>

      <div
        data-testid="assistant-panel-tabs"
        style={{
          flexShrink: 0,
          padding: '6px 12px',
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <Segmented
            size="small"
            value={tab}
            onChange={value => setTab(value as 'chat' | 'help')}
            options={[
              { label: '对话', value: 'chat' },
              { label: '帮助', value: 'help' },
            ]}
          />
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            {/* P4：会话历史（Dock 内切换历史会话 / 新建），与工作区共用内核动作。 */}
            <Popover
              open={historyOpen}
              onOpenChange={setHistoryOpen}
              trigger="click"
              placement="bottomRight"
              title="会话历史"
              content={
                <div
                  data-testid="assistant-history-popover"
                  style={{ width: 280, maxHeight: 320, overflowY: 'auto' }}
                >
                  <Button
                    data-testid="assistant-history-new"
                    type="text"
                    size="small"
                    icon={<Plus size={13} />}
                    onClick={handleNewConversation}
                    disabled={streaming}
                    style={{ width: '100%', justifyContent: 'flex-start', marginBottom: 4 }}
                  >
                    新建会话
                  </Button>
                  {loadingConvs ? (
                    <div style={{ textAlign: 'center', padding: 12 }}>
                      <Spin size="small" />
                    </div>
                  ) : conversations.length === 0 ? (
                    <div style={{ fontSize: 12, color: token.colorTextSecondary, padding: '8px 4px' }}>
                      暂无会话记录
                    </div>
                  ) : (
                    conversations.map(conv => (
                      <div
                        key={conv.id}
                        data-testid={`assistant-history-item-${conv.id}`}
                        role="button"
                        tabIndex={0}
                        aria-current={conv.id === convId ? 'true' : undefined}
                        onClick={() => handleSwitchConversation(conv)}
                        onKeyDown={e => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            handleSwitchConversation(conv);
                          }
                        }}
                        style={{
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 2,
                          padding: '6px 8px',
                          borderRadius: 6,
                          cursor: streaming ? 'not-allowed' : 'pointer',
                          background: conv.id === convId ? token.colorFillSecondary : undefined,
                          opacity: streaming ? 0.6 : 1,
                        }}
                      >
                        <span style={{ fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {conv.title || `会话 #${conv.id}`}
                        </span>
                        <span style={{ fontSize: 11, color: token.colorTextSecondary }}>
                          {formatTime(conv.createdAt)}
                        </span>
                      </div>
                    ))
                  )}
                </div>
              }
            >
              <Tooltip title={currentConvTitle ? `会话历史 · ${currentConvTitle}` : '会话历史'}>
                <Button
                  data-testid="assistant-history-open"
                  type="text"
                  size="small"
                  icon={<History size={14} />}
                  aria-label="会话历史"
                  aria-haspopup="dialog"
                  aria-expanded={historyOpen}
                  onClick={() => setHistoryOpen(v => !v)}
                />
              </Tooltip>
            </Popover>
            {/* P3：Dock → 工作区会话交接（无会话/生成中禁用）。 */}
            <Tooltip title={workspaceHint}>
              <Button
                data-testid="assistant-open-workspace"
                type="text"
                size="small"
                icon={<ExternalLink size={14} />}
                onClick={handleOpenWorkspace}
                disabled={!convId || streaming}
                aria-label="在工作区打开"
              />
            </Tooltip>
            {/* O2-02：诊断入口（与 /ai/chat 工作区共用同一实现；关闭时不请求轨迹）。 */}
            <AssistantDiagnosticsEntry
              messages={messages}
              conversationId={convId}
              streaming={streaming}
            />
          </div>
        </div>
      </div>

      {tab === 'chat' ? (
        <>
      {/* I0-01：状态机可见反馈（streaming / tool_running / degraded）。 */}
      <AssistantInteractionBanner
        state={interactionState}
        runningTool={runningTool}
        errorDetail={lastAssistant?.error}
        onAction={action => {
          if (action === 'retry') {
            const lastUser = [...messages].reverse().find(m => m.role === 'user');
            if (lastUser) handleSend(lastUser.content);
          }
        }}
      />
      <div ref={scrollRef} style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '14px 12px 6px' }}>
        {isEmpty ? (
          <div data-testid="assistant-panel-empty" style={{ padding: '16px 2px' }}>
            <div
              style={{
                fontSize: 12,
                color: token.colorTextSecondary,
                marginBottom: 12,
                textAlign: 'center',
              }}
            >
              结合知识库回答当前页面的问题
            </div>
            <div style={{ display: 'grid', gap: 8 }}>
              {/* D0-05：页面级建议卡（注册表驱动，未注册路由回退通用建议）。 */}
              <SuggestionCards onSend={handleSend} disabled={streaming} compact />
            </div>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
            {messages.map(item => (
              <ChatMessageItem
                key={item.id}
                message={item}
                providerLabel={providerLabel}
                onCreateArticle={handlePromoteToArticle}
                onOpenApproval={handleOpenApproval}
                onOpenOutcome={openOutcome}
                onRequestConfirm={handleRequestConfirm}
              />
            ))}
          </div>
        )}
      </div>

      {/* V2-02：异步精算回执（先给可用答案；后台精算完成后为未读回执/FAB 状态点）。 */}
      <div style={{ flexShrink: 0, paddingTop: 6 }}>
        <AssistantAsyncAnswerCard />
      </div>

      {/* V2-01：个性化记忆条（常用语/模板/记忆管理；点击只预填，不发送）。 */}
      <div style={{ flexShrink: 0, padding: '8px 12px 0' }}>
        <AssistantMemoryChips query={query} onInsert={handleInsertMemory} />
      </div>

      <div
        style={{
          flexShrink: 0,
          padding: '8px 12px 8px',
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
            style={{ marginBottom: 8 }}
          />
        ) : null}

        <div
          style={{
            display: 'flex',
            alignItems: 'flex-end',
            gap: 8,
            padding: '4px 4px 4px 12px',
            borderRadius: 20,
            border: `1px solid ${token.colorBorderSecondary}`,
            background: token.colorBgContainer,
          }}
        >
          <Input.TextArea
            ref={inputRef}
            data-testid="assistant-panel-input"
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="请输入你的问题…"
            // P1：宽档位（≥560px）给输入更多行数，长提示词不必在 6 行里滚动。
            autoSize={{ minRows: 1, maxRows: widePanel ? 10 : 6 }}
            variant="borderless"
            style={{ padding: '6px 0', resize: 'none', fontSize: 13, lineHeight: 1.6 }}
          />
          <Tooltip title="后台精算：先看快速参考，完整回答完成后通知">
            <Button
              data-testid="assistant-async-start"
              shape="circle"
              icon={<Sparkles size={15} />}
              onClick={() => void handleStartAsync()}
              disabled={!query.trim()}
              aria-label="后台精算"
            />
          </Tooltip>
          {streaming ? (
            <Button
              shape="circle"
              danger
              icon={<StopCircle size={15} />}
              onClick={handleStop}
              aria-label="停止生成"
            />
          ) : (
            <Button
              data-testid="assistant-panel-send"
              shape="circle"
              type="primary"
              icon={<Send size={15} />}
              onClick={() => handleSend()}
              disabled={!query.trim()}
              aria-label="发送"
            />
          )}
        </div>
        <div
          style={{
            fontSize: 11,
            color: token.colorTextSecondary,
            textAlign: 'center',
            marginTop: 6,
          }}
        >
          Enter 发送 · 回答由 AI 生成，请自行甄别
        </div>
      </div>
        </>
      ) : (
        <AssistantHelpPanel onDraft={handleDraftFromHelp} />
      )}

    </div>
  );
};

export default AssistantChatPanel;