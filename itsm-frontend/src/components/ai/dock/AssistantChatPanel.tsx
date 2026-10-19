/**
 * D0-02 AssistantDock 聊天视图（紧凑面板）。
 *
 * 与工作区（AIChat.tsx）共用 `useAIChatSession` + `ChatMessageItem`：
 * - 不展示会话历史侧栏与 Provider/Bot 工具条（紧凑形态；能力切换留在工作区）；
 * - 懒挂载：仅面板打开时挂载本组件，因此未展开时零会话/候选请求（见 AssistantDock 注释）。
 */
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, App, Button, Input, Segmented, Tooltip, theme } from 'antd';
import { Send, Sparkles, StopCircle } from 'lucide-react';

import { useAIChatSession } from '@/components/ai/chat-core/use-ai-chat-session';
import { ChatMessageItem } from '@/components/ai/chat-core/ChatMessageItem';
import SuggestionCards from '@/components/ai/chat-core/SuggestionCards';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { startAssistantAsyncAnswer } from '@/lib/ai/async/assistant-async-store';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import AssistantHelpPanel from './AssistantHelpPanel';
import AssistantDiagnosticsEntry from '@/components/ai/diagnostics/AssistantDiagnosticsEntry';
import AssistantMemoryChips from './AssistantMemoryChips';
import AssistantAsyncAnswerCard from './AssistantAsyncAnswerCard';

const AssistantChatPanel: React.FC = () => {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const { pageKey: assistantPageKey, route: assistantRoute } = usePageAssistantContext();
  const {
    messages,
    convId,
    streaming,
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
  } = useAIChatSession();

  // H0-04：面板页签（对话 / 帮助）；"生成初稿"切回对话并发送结构化提示。
  const [tab, setTab] = useState<'chat' | 'help'>('chat');
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
          {/* O2-02：诊断入口（与 /ai/chat 工作区共用同一实现；关闭时不请求轨迹）。 */}
          <AssistantDiagnosticsEntry
            messages={messages}
            conversationId={convId}
            streaming={streaming}
          />
        </div>
      </div>

      {tab === 'chat' ? (
        <>
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
            data-testid="assistant-panel-input"
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="请输入你的问题…"
            autoSize={{ minRows: 1, maxRows: 6 }}
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