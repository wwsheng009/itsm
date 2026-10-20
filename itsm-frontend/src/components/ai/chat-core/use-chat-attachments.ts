/**
 * T5-03b / composer 重构：AI 对话附件上传逻辑（Dock / 工作区共用）。
 *
 * 抽出本 hook 的原因：附件入口需要被放进 composer 的工具条（与发送按钮同一行），
 * 而附件芯片要显示在输入框上方；两者在 DOM 上不再相邻，无法继续用一个自包含组件承载。
 * hook 只维护"选择 → 校验 → 上传 → 元数据"这条链路，视图层自行决定按钮与芯片放在哪里。
 *
 * 契约（与原 ChatAttachmentPicker 完全一致）：
 * - 上传走 `aiUploadAttachment`（ai:read 专用端点）；
 * - 只维护元数据，不把文件内容塞进对话请求；
 * - 最多 5 个、单文件 20MB；失败如实置错，不产生芯片。
 */
import { useCallback, useRef, useState } from 'react';

import { aiUploadAttachment, type AIAttachmentRef } from '@/lib/api/ai-api';
import { validateAttachmentFile } from '@/components/common/attachment/AttachmentField';
import { DEFAULT_ATTACHMENT_MAX_SIZE_MB } from '@/lib/upload/types';
import { useI18n } from '@/lib/i18n/useI18n';

export const CHAT_ATTACHMENT_INPUT_TESTID = 'chat-attachment-input';
export const CHAT_ATTACHMENT_BUTTON_TESTID = 'chat-attachment-upload';
export const CHAT_ATTACHMENT_LIST_TESTID = 'chat-attachment-list';

export const MAX_CHAT_ATTACHMENTS = 5;
export const CHAT_ATTACHMENT_MAX_SIZE_MB = DEFAULT_ATTACHMENT_MAX_SIZE_MB;

export interface UseChatAttachmentsOptions {
  attachments: AIAttachmentRef[];
  onChange: (next: AIAttachmentRef[]) => void;
  disabled?: boolean;
}

export interface UseChatAttachmentsResult {
  inputRef: React.RefObject<HTMLInputElement | null>;
  uploading: boolean;
  error: string | null;
  /** 是否还能继续添加（未达上限）。 */
  canAddMore: boolean;
  max: number;
  maxSizeMB: number;
  /** 打开系统文件选择框。 */
  openPicker: () => void;
  /** 绑定到隐藏 `<input type="file">`。 */
  onInputChange: (event: React.ChangeEvent<HTMLInputElement>) => void;
  remove: (id: number) => void;
  clearError: () => void;
}

export function useChatAttachments({
  attachments,
  onChange,
  disabled = false,
}: UseChatAttachmentsOptions): UseChatAttachmentsResult {
  const { t } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleFiles = useCallback(
    async (files: File[]) => {
      if (files.length === 0 || uploading || disabled) return;
      const room = MAX_CHAT_ATTACHMENTS - attachments.length;
      if (room <= 0) {
        setError(t('assistant.attachment.limit', { max: MAX_CHAT_ATTACHMENTS }));
        return;
      }
      const accepted = files.slice(0, room);
      if (files.length > room) {
        setError(t('assistant.attachment.limit', { max: MAX_CHAT_ATTACHMENTS }));
      }
      setUploading(true);
      setError(null);
      const uploaded: AIAttachmentRef[] = [];
      try {
        for (const file of accepted) {
          const verdict = validateAttachmentFile(file, {
            maxSizeMB: CHAT_ATTACHMENT_MAX_SIZE_MB,
          });
          if (!verdict.ok) {
            setError(verdict.reason);
            continue;
          }
          try {
            uploaded.push(await aiUploadAttachment(file));
          } catch {
            setError(t('assistant.attachment.uploadFailed'));
          }
        }
        if (uploaded.length > 0) {
          onChange([...attachments, ...uploaded]);
        }
      } finally {
        setUploading(false);
        if (inputRef.current) inputRef.current.value = '';
      }
    },
    [attachments, disabled, onChange, t, uploading]
  );

  const onInputChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      const files = Array.from(event.target.files ?? []);
      void handleFiles(files);
    },
    [handleFiles]
  );

  const remove = useCallback(
    (id: number) => {
      onChange(attachments.filter(item => item.id !== id));
    },
    [attachments, onChange]
  );

  const openPicker = useCallback(() => {
    if (disabled || uploading) return;
    inputRef.current?.click();
  }, [disabled, uploading]);

  const clearError = useCallback(() => setError(null), []);

  return {
    inputRef,
    uploading,
    error,
    canAddMore: attachments.length < MAX_CHAT_ATTACHMENTS,
    max: MAX_CHAT_ATTACHMENTS,
    maxSizeMB: CHAT_ATTACHMENT_MAX_SIZE_MB,
    openPicker,
    onInputChange,
    remove,
    clearError,
  };
}
