
/**
 * CommentAttachmentField / CommentAttachmentList —— 评论附件的选择与展示（FE-9）。
 *
 * 依据：`docs/plan/generic-attachment-richtext-control-plan.md` §6.2-7 / §7 FE-9。
 *  - 选择 + 上传复用 `AttachmentField` 的即时模式（体积与扩展名校验、上传进度、
 *    失败重试、10MB 上限、i18n 全部继承），本组件只把「评论附件上传」契约适配成
 *    `AttachmentField` 需要的 `(file, onProgress) => { id, url }` 并注入解绑回调；
 *  - 展示消费 BE-11 的 `attachmentRefs`（域名内下载 / 预览地址）；`attachments`
 *    中拿不到元数据的 ID 归入「已失效」占位，不在 UI 上静默丢失；
 *  - 组件不直连任何 HTTP 实现（upload / remove 由调用方注入），沿用 `AttachmentField`
 *    的可测试性约束（§2.2 分层：业务域只注入宿主上下文）。
 */

import React from 'react';
import { Button, Space, Tag, Typography } from 'antd';
import { Download, Eye, Paperclip } from 'lucide-react';
import { DEFAULT_ATTACHMENT_MAX_COUNT } from '@/lib/upload/types';
import type { CommentAttachment } from '@/types/comment';
import { useI18n } from '@/lib/i18n/useI18n';
import AttachmentField, {
  formatAttachmentSize,
  type AttachmentFieldItem,
} from './AttachmentField';

const { Text } = Typography;

export interface CommentAttachmentUploadResult {
  id: number;
  /** 上传后的展示地址（可选；缺失时由渲染侧用域内端点兜底） */
  url?: string;
}

export interface CommentAttachmentFieldProps {
  value: AttachmentFieldItem[];
  onChange: (items: AttachmentFieldItem[]) => void;
  /** 选择文件即上传（先上传后绑定；宿主与 usage 由调用方固定） */
  upload: (
    file: File,
    onProgress?: (percent: number) => void
  ) => Promise<CommentAttachmentUploadResult>;
  /** 解绑已上传附件；被评论引用时后端返回 409 / 6105，由调用方提示 */
  remove?: (attachmentId: number) => Promise<void>;
  disabled?: boolean;
  maxCount?: number;
  dataTestId?: string;
}

export const CommentAttachmentField: React.FC<CommentAttachmentFieldProps> = ({
  value,
  onChange,
  upload,
  remove,
  disabled,
  maxCount,
  dataTestId = 'comment-attachment-field',
}) => {
  const { t } = useI18n();

  return (
    <AttachmentField
      value={value}
      onChange={onChange}
      disabled={disabled}
      maxCount={maxCount ?? DEFAULT_ATTACHMENT_MAX_COUNT}
      title={t('attachments.upload')}
      dataTestId={dataTestId}
      uploader={(file, onProgress) => upload(file, onProgress)}
      onDeleteUploaded={
        remove
          ? (item: AttachmentFieldItem) =>
              item.attachmentId ? remove(item.attachmentId) : undefined
          : undefined
      }
    />
  );
};

export interface CommentAttachmentListProps {
  /** BE-11 展示元数据（仅存活、同宿主、usage=comment_attachment 的记录） */
  refs?: CommentAttachment[];
  /** 评论绑定的附件 ID 总数（含拿不到元数据的失效项） */
  totalCount?: number;
  dataTestId?: string;
}

export const CommentAttachmentList: React.FC<CommentAttachmentListProps> = ({
  refs,
  totalCount,
  dataTestId = 'comment-attachment-list',
}) => {
  const { t } = useI18n();
  const items = refs ?? [];
  const boundCount = totalCount ?? items.length;
  const missing = Math.max(0, boundCount - items.length);

  if (items.length === 0 && missing === 0) return null;

  return (
    <Space orientation="vertical" size={2} style={{ width: '100%' }} data-testid={dataTestId}>
      {items.map((ref) => (
        <div key={ref.id} className="flex items-center space-x-2 text-sm" data-testid="comment-attachment-item">
          <Paperclip className="w-3.5 h-3.5 text-gray-400 shrink-0" />
          <a
            href={ref.downloadUrl}
            className="truncate text-blue-600 hover:text-blue-500"
            title={ref.fileName}
          >
            {ref.fileName}
          </a>
          <Text type="secondary" className="text-xs whitespace-nowrap">
            {formatAttachmentSize(ref.fileSize)}
          </Text>
          <Button
            type="link"
            size="small"
            icon={<Download className="w-3 h-3" />}
            href={ref.downloadUrl}
            aria-label={`${t('attachments.download')} ${ref.fileName}`}
          >
            {t('attachments.download')}
          </Button>
          {ref.previewUrl && (
            <Button
              type="link"
              size="small"
              icon={<Eye className="w-3 h-3" />}
              href={ref.previewUrl}
              target="_blank"
              rel="noreferrer"
              aria-label={`${t('attachments.preview')} ${ref.fileName}`}
            >
              {t('attachments.preview')}
            </Button>
          )}
        </div>
      ))}
      {missing > 0 && (
        <Tag color="default" data-testid="comment-attachment-expired">
          {t('attachments.expired')}
        </Tag>
      )}
    </Space>
  );
};
