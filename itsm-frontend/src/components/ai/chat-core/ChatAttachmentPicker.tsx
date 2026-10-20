/**
 * T5-03b：AI 对话附件选择器（Dock / 工作区共用）。
 *
 * - 上传走 `aiUploadAttachment`（ai:read 专用端点）；
 * - 只维护元数据，不把文件内容塞进对话请求；
 * - 最多 5 个、单文件 20MB；发送由会话内核负责，组件不自动提交。
 *
 * composer 重构（2026-10-19）：上传链路抽到 `use-chat-attachments`，
 * 本组件保留「自包含」形态供工作区使用；Dock 面板改为把按钮放进 composer 工具条、
 * 芯片单独渲染在输入框上方，避免附件入口独占一整行。
 */
import React from 'react';
import { Button, Space, Tag, Tooltip, Typography } from 'antd';
import { Paperclip } from 'lucide-react';

import type { AIAttachmentRef } from '@/lib/api/ai-api';
import { ACCEPT_ATTACHMENT_STRING } from '@/components/common/attachment/AttachmentField';
import { useI18n } from '@/lib/i18n/useI18n';
import {
  CHAT_ATTACHMENT_BUTTON_TESTID,
  CHAT_ATTACHMENT_INPUT_TESTID,
  useChatAttachments,
} from './use-chat-attachments';

const { Text } = Typography;

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
  const {
    inputRef,
    uploading,
    error,
    canAddMore,
    max,
    maxSizeMB,
    openPicker,
    onInputChange,
    remove,
  } = useChatAttachments({ attachments, onChange, disabled });

  return (
    <Space direction="vertical" size={4} style={{ width: '100%' }}>
      <input
        ref={inputRef}
        type="file"
        multiple
        accept={ACCEPT_ATTACHMENT_STRING}
        style={{ display: 'none' }}
        data-testid={CHAT_ATTACHMENT_INPUT_TESTID}
        onChange={onInputChange}
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
        <Tooltip title={t('assistant.attachment.uploadHint', { max, maxSize: maxSizeMB })}>
          <Button
            type="text"
            size="small"
            icon={<Paperclip size={14} />}
            loading={uploading}
            disabled={disabled || !canAddMore}
            data-testid={CHAT_ATTACHMENT_BUTTON_TESTID}
            onClick={openPicker}
          >
            {uploading ? t('assistant.attachment.uploading') : t('assistant.attachment.upload')}
          </Button>
        </Tooltip>
      </Space>
      {attachments.length > 0 ? (
        <Text type="secondary" style={{ fontSize: 11 }}>
          {t('assistant.attachment.count', { count: attachments.length, max })}
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
