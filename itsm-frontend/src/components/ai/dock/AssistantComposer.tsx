/**
 * Dock 聊天输入坞（composer）。
 *
 * 设计目标（2026-10-19 UI/UX 优化）：
 * - **单容器**：输入框与全部动作收进同一个圆角容器，不再让附件入口独占一整行；
 * - **两级布局**：上方是可增长的文本域，下方是一条 24px 高的工具条——左侧「附件 / 记忆」，
 *   右侧「后台精算 / 发送（或停止）」；视觉重心落在文本域，而不是一排按钮；
 * - **图标化 + 可访问名**：工具条只保留图标按钮，全部带 `aria-label` 与 Tooltip，
 *   既降噪又不牺牲可发现性与读屏语义（WCAG 2.5.3 / 4.1.2）；
 * - **聚焦反馈**：仅在聚焦时显示主色描边与阴影，静止态保持极轻的次色描边；
 * - 附件芯片在文本域上方以只读 chip 呈现，失败原因就地提示（不上移、不弹窗）。
 */
import React, { useState } from 'react';
import { Button, Input, Tooltip, theme } from 'antd';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import { Paperclip, Send, Sparkles, StopCircle, X } from 'lucide-react';

import type { AIAttachmentRef } from '@/lib/api/ai-api';
import { InputDockAssistMenu } from '@/components/ai/chat-core/InputDockAssistMenu';
import type { useInputDockAssist } from '@/components/ai/chat-core/use-input-dock-assist';
import { CHAT_ATTACHMENT_BUTTON_TESTID } from '@/components/ai/chat-core/use-chat-attachments';
import { useI18n } from '@/lib/i18n/useI18n';

export const ASSISTANT_COMPOSER_TESTID = 'assistant-composer';

interface AssistantComposerProps {
  query: string;
  setQuery: (value: string) => void;
  onSend: () => void;
  onStop: () => void;
  streaming: boolean;
  inputRef: React.RefObject<TextAreaRef | null>;
  inputAssist: ReturnType<typeof useInputDockAssist>;
  /** 宽档位：给长提示词更多行数。 */
  widePanel: boolean;
  /** 后台精算入口（Dock 专属；不传则不渲染该按钮）。 */
  onStartAsync?: () => void;
  attachments: AIAttachmentRef[];
  /** 附件选择（由 useChatAttachments 提供的隐藏 input 与打开动作）。 */
  attachmentInput: React.ReactNode;
  onOpenAttachment: () => void;
  attachmentUploading: boolean;
  attachmentError: string | null;
  attachmentDisabled: boolean;
  onRemoveAttachment: (id: number) => void;
  /** 左侧工具条附加入口（记忆等；可为空）。 */
  tools?: React.ReactNode;
  /* ---------- 工作区（/ai/chat）复用所需的形态参数 ---------- */
  /** 容器 testid（默认 Dock 的 `assistant-composer`）。 */
  testId?: string;
  /** 输入框 testid（Dock: assistant-panel-input；工作区: assistant-chat-input）。 */
  inputTestId?: string;
  /** 输入字号（工作区 14，Dock 13）。 */
  inputFontSize?: number;
  /** 文本域最大行数（不传则由 widePanel 推导）。 */
  maxRows?: number;
  /** 工具条右侧、发送按钮之前的附加动作。 */
  extraActions?: React.ReactNode;
}

const ICON_BUTTON_STYLE: React.CSSProperties = {
  width: 28,
  height: 28,
  minWidth: 28,
  borderRadius: 8,
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
};

const AssistantComposer: React.FC<AssistantComposerProps> = ({
  query,
  setQuery,
  onSend,
  onStop,
  streaming,
  inputRef,
  inputAssist,
  widePanel,
  onStartAsync,
  attachments,
  attachmentInput,
  onOpenAttachment,
  attachmentUploading,
  attachmentError,
  attachmentDisabled,
  onRemoveAttachment,
  tools,
  testId = ASSISTANT_COMPOSER_TESTID,
  inputTestId = 'assistant-panel-input',
  inputFontSize = 13,
  maxRows,
  extraActions,
}) => {
  const { token } = theme.useToken();
  const { t } = useI18n();
  const [focused, setFocused] = useState(false);
  // 流式生成中禁止再选附件：否则新附件会滞留到下一轮（与"本轮已发出"的语义不符）。
  const attachmentsLocked = attachmentDisabled || streaming;
  const asyncTooltip =
    attachments.length > 0
      ? t('assistant.attachment.asyncUnsupported')
      : !query.trim()
        ? t('assistant.panel.asyncRequiresPrompt')
        : t('assistant.panel.asyncStartTooltip');

  return (
    <div data-testid={testId}>
      {attachmentInput}

      {/* 附件芯片：单行横向滚动，不挤占工具条。 */}
      {attachments.length > 0 ? (
        <div
          data-testid="chat-attachment-list"
          style={{
            display: 'flex',
            gap: 6,
            flexWrap: 'wrap',
            marginBottom: 6,
          }}
        >
          {attachments.map(item => (
            <span
              key={item.id}
              data-testid={`chat-attachment-chip-${item.id}`}
              title={`${item.fileName} · ${item.mimeType} · ${item.fileSize} bytes`}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                maxWidth: '100%',
                padding: '2px 4px 2px 8px',
                borderRadius: 6,
                fontSize: 11,
                lineHeight: '18px',
                color: token.colorTextSecondary,
                background: token.colorFillTertiary,
                border: `1px solid ${token.colorBorderSecondary}`,
              }}
            >
              <span
                style={{
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  maxWidth: 180,
                }}
              >
                {item.fileName}
              </span>
              {!attachmentDisabled ? (
                <button
                  type="button"
                  aria-label={t('assistant.attachment.remove', { name: item.fileName })}
                  onClick={() => onRemoveAttachment(item.id)}
                  style={{
                    border: 'none',
                    background: 'transparent',
                    padding: 0,
                    width: 16,
                    height: 16,
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    cursor: 'pointer',
                    color: token.colorTextTertiary,
                    borderRadius: 4,
                  }}
                >
                  <X size={11} />
                </button>
              ) : null}
            </span>
          ))}
        </div>
      ) : null}

      {attachmentError ? (
        <div
          data-testid="chat-attachment-error"
          style={{ fontSize: 11, color: token.colorError, marginBottom: 6 }}
        >
          {attachmentError}
        </div>
      ) : null}

      <div
        style={{
          position: 'relative',
          borderRadius: 16,
          border: `1px solid ${focused ? token.colorPrimaryBorder : token.colorBorderSecondary}`,
          background: token.colorBgContainer,
          boxShadow: focused ? token.boxShadowTertiary : 'none',
          transition: 'border-color .15s ease, box-shadow .15s ease',
        }}
      >
        <Input.TextArea
          ref={inputRef}
          data-testid={inputTestId}
          value={query}
          onChange={e => setQuery(e.target.value)}
          onKeyDown={inputAssist.handleKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder={t('assistant.panel.inputPlaceholder')}
          autoSize={{ minRows: 1, maxRows: maxRows ?? (widePanel ? 10 : 6) }}
          variant="borderless"
          style={{
            padding: '10px 12px 2px',
            resize: 'none',
            fontSize: inputFontSize,
            lineHeight: 1.6,
          }}
        />

        {/* 工具条：左侧低频动作、右侧主次动作；全部图标化以降低视觉噪声。 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 2,
            padding: '2px 6px 6px',
          }}
        >
          <Tooltip
            title={t('assistant.attachment.uploadHint', { max: 5, maxSize: 20 })}
          >
            <Button
              type="text"
              size="small"
              style={ICON_BUTTON_STYLE}
              icon={<Paperclip size={15} />}
              loading={attachmentUploading}
              disabled={attachmentsLocked || attachments.length >= 5}
              data-testid={CHAT_ATTACHMENT_BUTTON_TESTID}
              aria-label={t('assistant.attachment.upload')}
              onClick={onOpenAttachment}
            />
          </Tooltip>

          {tools}

          <span style={{ flex: 1 }} />

          {extraActions}

          {onStartAsync ? (
            <Tooltip title={asyncTooltip}>
              {/* disabled 的 antd Button 不派发鼠标事件；外层 span 保证 tooltip 仍可见。 */}
              <span
                data-testid="assistant-async-tooltip"
                style={{ display: 'inline-flex' }}
                aria-label={asyncTooltip}
              >
                <Button
                  data-testid="assistant-async-start"
                  type="text"
                  size="small"
                  style={ICON_BUTTON_STYLE}
                  icon={<Sparkles size={15} />}
                  onClick={onStartAsync}
                  disabled={!query.trim() || attachments.length > 0}
                  aria-label={t('assistant.panel.asyncStart')}
                />
              </span>
            </Tooltip>
          ) : null}

          {streaming ? (
            <Tooltip title={t('assistant.panel.stopGenerating')}>
              <Button
                shape="circle"
                size="small"
                danger
                icon={<StopCircle size={15} />}
                onClick={onStop}
                aria-label={t('assistant.panel.stopGenerating')}
              />
            </Tooltip>
          ) : (
            <Tooltip title={t('assistant.panel.send')}>
              <Button
                data-testid="assistant-panel-send"
                shape="circle"
                size="small"
                type="primary"
                icon={<Send size={14} />}
                onClick={onSend}
                disabled={!query.trim()}
                aria-label={t('assistant.panel.send')}
              />
            </Tooltip>
          )}
        </div>

        {/* `/` 快捷指令与 `@` 引用菜单（定位在容器内）。 */}
        {inputAssist.assist ? (
          <InputDockAssistMenu
            mode={inputAssist.assist.mode}
            items={inputAssist.assist.items}
            activeIndex={inputAssist.assist.activeIndex}
            hint={inputAssist.assist.hint}
            onSelect={inputAssist.selectItem}
            onHighlight={inputAssist.highlightItem}
          />
        ) : null}
      </div>
    </div>
  );
};

export default AssistantComposer;
