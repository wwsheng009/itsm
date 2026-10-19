/**
 * T5-03b：AI 对话附件选择器（Dock / 工作区共用）。
 *
 * - 上传走 `aiUploadAttachment`（ai:read 专用端点）；
 * - 只维护元数据，不把文件内容塞进对话请求；
 * - 最多 5 个、单文件 20MB；发送由会话内核负责，组件不自动提交。
 */
import React, { useCallback, useRef, useState } from 'react';
import { Button, Space, Tag, Tooltip, Typography } from 'antd';
import { Paperclip } from 'lucide-react';

import { aiUploadAttachment, type AIAttachmentRef } from '@/lib/api/ai-api';
import {
  ACCEPT_ATTACHMENT_STRING,
  validateAttachmentFile,
} from '@/components/common/attachment/AttachmentField';
import { DEFAULT_ATTACHMENT_MAX_SIZE_MB } from '@/lib/upload/types';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

export const CHAT_ATTACHMENT_INPUT_TESTID = 'chat-attachment-input';
export const CHAT_ATTACHMENT_BUTTON_TESTID = 'chat-attachment-upload';

const MAX_ATTACHMENTS = 5;
const MAX_SIZE_MB = DEFAULT_ATTACHMENT_MAX_SIZE_MB;

interface ChatAttachmentPickerProps {
  attachments: AIAttachmentRef[];
  onChange: (next: AIAttachmentRef[]) => void;
  disabled?: boolean;
}

const ChatAttachmentPicker: React.FC<ChatAttachmentPickerProps> = ({
  attachments,
  onChange,
  disabled = false,
}) => {
  const { t } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleFiles = useCallback(
    async (files: File[]) => {
      if (files.length === 0 || uploading || disabled) return;
      const room = MAX_ATTACHMENTS - attachments.length;
      if (room <= 0) {
        setError(t('assistant.attachment.limit', { max: MAX_ATTACHMENTS }));
        return;
      }
      const accepted = files.slice(0, room);
      if (files.length > room) {
        setError(t('assistant.attachment.limit', { max: MAX_ATTACHMENTS }));
      }
      setUploading(true);
      setError(null);
      const uploaded: AIAttachmentRef[] = [];
      try {
        for (const file of accepted) {
          const verdict = validateAttachmentFile(file, { maxSizeMB: MAX_SIZE_MB });
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

  const remove = useCallback(
    (id: number) => {
      onChange(attachments.filter(item => item.id !== id));
    },
    [attachments, onChange]
  );

  return (
    <Space direction="vertical" size={4} style={{ width: '100%' }}>
      <input
        ref={inputRef}
        type="file"
        multiple
        accept={ACCEPT_ATTACHMENT_STRING}
        style={{ display: 'none' }}
        data-testid={CHAT_ATTACHMENT_INPUT_TESTID}
        onChange={event => {
          const files = Array.from(event.target.files ?? []);
          void handleFiles(files);
        }}
      />
      <Space wrap size={[6, 6]} data-testid="chat-attachment-list">
        {attachments.map(item => (
          <Tag
            key={item.id}
            closable={!disabled && !uploading}
            onClose={() => remove(item.id)}
            data-testid={`chat-attachment-chip-${item.id}`}
            title={`${item.fileName} · ${item.mimeType} · ${item.fileSize} bytes`}
          >
            {item.fileName}
          </Tag>
        ))}
        <Tooltip
          title={t('assistant.attachment.uploadHint', {
            max: MAX_ATTACHMENTS,
            maxSize: MAX_SIZE_MB,
          })}
        >
          <Button
            type="text"
            size="small"
            icon={<Paperclip size={14} />}
            loading={uploading}
            disabled={disabled || attachments.length >= MAX_ATTACHMENTS}
            data-testid={CHAT_ATTACHMENT_BUTTON_TESTID}
            onClick={() => inputRef.current?.click()}
          >
            {uploading ? t('assistant.attachment.uploading') : t('assistant.attachment.upload')}
          </Button>
        </Tooltip>
      </Space>
      {attachments.length > 0 ? (
        <Text type="secondary" style={{ fontSize: 11 }}>
          {t('assistant.attachment.count', { count: attachments.length, max: MAX_ATTACHMENTS })}
        </Text>
      ) : null}
      {error ? (
        <Text type="danger" style={{ fontSize: 11 }} data-testid="chat-attachment-error">
          {error}
        </Text>
      ) : null}
    </Space>
  );
};

export default ChatAttachmentPicker;
