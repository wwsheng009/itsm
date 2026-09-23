'use client';

/**
 * AttachmentField —— 可嵌入表单的附件字段壳层（受控值 + 两段式上传）
 *
 * 设计文档：docs/architecture/ticket-create-page-rich-input-optimization.md §4.5 / §6.2
 *
 * 两种工作模式由是否注入 `uploader` 决定：
 *  - 暂存模式（创建页）：只暂存 File，提交拿到工单 ID 后由父级调用 `uploadAttachmentItems`
 *    统一上传；失败可重试，不阻断其他文件。
 *  - 即时模式（编辑页）：注入绑定工单的上传/删除实现，选择文件即上传，删除即解绑。
 *
 * 组件本身不依赖任何业务 API，便于在工单创建页、编辑页、回复框等场景复用。
 */

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { App, Button, Progress, Space, Tag, Tooltip, Typography, Upload } from 'antd';
import {
  FileArchive,
  FileSpreadsheet,
  FileText,
  Image as ImageIcon,
  Paperclip,
  RotateCcw,
  Trash2,
  UploadCloud,
} from 'lucide-react';

const { Text } = Typography;

export type AttachmentItemStatus = 'pending' | 'uploading' | 'done' | 'error';

export interface AttachmentFieldItem {
  /** 前端唯一键（暂存/上传全生命周期稳定） */
  uid: string;
  name: string;
  size: number;
  /** MIME type */
  type: string;
  /** 暂存模式下的原始文件；已上传项可为空 */
  file?: File;
  /** 上传成功后后端返回的附件 ID */
  attachmentId?: number;
  /** 上传成功后的可访问地址 */
  url?: string;
  status: AttachmentItemStatus;
  progress?: number;
  error?: string;
}

export interface AttachmentUploadResult {
  id: number;
  url?: string;
}

export type AttachmentUploader = (
  file: File,
  onProgress?: (percent: number) => void
) => Promise<AttachmentUploadResult>;

export type AttachmentDeleter = (item: AttachmentFieldItem) => Promise<void> | void;

export interface AttachmentFieldProps {
  value?: AttachmentFieldItem[];
  onChange?: (items: AttachmentFieldItem[]) => void;
  disabled?: boolean;
  /** 最大文件数，默认 10 */
  maxCount?: number;
  /** 单文件最大体积（MB），默认 50 */
  maxSizeMB?: number;
  /** 白名单 accept 字符串 */
  accept?: string;
  /** 注入后进入即时上传模式 */
  uploader?: AttachmentUploader;
  /** 即时模式下删除已上传附件的回调（未注入时仅从列表移除） */
  onDeleteUploaded?: AttachmentDeleter;
  /** 卡片标题，默认「附件」 */
  title?: React.ReactNode;
  showTitle?: boolean;
  hint?: string;
  dataTestId?: string;
}

/** 附件白名单：文档 / 表格 / 图片 / 压缩包 */
export const ACCEPTED_ATTACHMENT_EXTENSIONS = [
  'png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg',
  'pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx',
  'txt', 'csv', 'md', 'json', 'log', 'xml',
  'zip', 'rar', '7z', 'tar', 'gz',
];

export const ACCEPT_ATTACHMENT_STRING = ACCEPTED_ATTACHMENT_EXTENSIONS.map((ext) => `.${ext}`).join(',');

/**
 * 附件体积 / 扩展名校验（不依赖 MIME，浏览器对 zip 等类型 MIME 常为空）。
 */
export function validateAttachmentFile(
  file: File,
  options: { maxSizeMB?: number } = {}
): { ok: true } | { ok: false; reason: string } {
  const maxSizeMB = options.maxSizeMB ?? 50;
  if (file.size > maxSizeMB * 1024 * 1024) {
    return { ok: false, reason: `「${file.name}」超过 ${maxSizeMB}MB 上限` };
  }
  if (file.size === 0) {
    // 后端拒绝 0 字节附件（ent 的 file_size 校验），提前给出可读原因，避免上传后才失败
    return { ok: false, reason: `「${file.name}」是空文件（0 字节），无法上传` };
  }
  const ext = file.name.includes('.') ? file.name.split('.').pop()!.toLowerCase() : '';
  if (!ext || !ACCEPTED_ATTACHMENT_EXTENSIONS.includes(ext)) {
    return { ok: false, reason: `「${file.name}」类型不在允许范围内` };
  }
  return { ok: true };
}

let attachmentUidSeed = 0;
const nextAttachmentUid = (): string => {
  attachmentUidSeed += 1;
  return `att-${Date.now().toString(36)}-${attachmentUidSeed}`;
};

/** 由原始文件构建暂存条目（不做校验，校验见 validateAttachmentFile） */
export function createAttachmentItems(files: File[]): AttachmentFieldItem[] {
  return files.map((file) => ({
    uid: nextAttachmentUid(),
    name: file.name,
    size: file.size,
    type: file.type,
    file,
    status: 'pending' as const,
    progress: 0,
  }));
}

/**
 * 批量上传暂存条目（创建页提交后调用）。
 * 顺序执行避免并发打满后端；单项失败不影响其他项，返回值保留每项最终状态。
 */
export async function uploadAttachmentItems(
  items: AttachmentFieldItem[],
  uploader: AttachmentUploader,
  onItemUpdate?: (uid: string, patch: Partial<AttachmentFieldItem>) => void
): Promise<AttachmentFieldItem[]> {
  const result = items.map((item) => ({ ...item }));

  for (let i = 0; i < result.length; i += 1) {
    const item = result[i];
    if (item.status === 'done' || !item.file) continue;

    result[i] = { ...item, status: 'uploading', progress: 0, error: undefined };
    onItemUpdate?.(item.uid, { status: 'uploading', progress: 0, error: undefined });

    try {
      const uploaded = await uploader(item.file, (percent) => {
        result[i] = { ...result[i], progress: percent };
        onItemUpdate?.(item.uid, { progress: percent });
      });
      const donePatch: Partial<AttachmentFieldItem> = {
        status: 'done',
        attachmentId: uploaded.id,
        url: uploaded.url,
        progress: 100,
        error: undefined,
      };
      result[i] = { ...result[i], ...donePatch };
      onItemUpdate?.(item.uid, donePatch);
    } catch (e) {
      const reason = e instanceof Error ? e.message : '上传失败';
      result[i] = { ...result[i], status: 'error', error: reason };
      onItemUpdate?.(item.uid, { status: 'error', error: reason });
    }
  }

  return result;
}

export function formatAttachmentSize(bytes: number): string {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  return `${Math.round((bytes / Math.pow(1024, i)) * 100) / 100} ${units[i]}`;
}

const renderFileIcon = (item: AttachmentFieldItem) => {
  const mime = item.type || '';
  const ext = item.name.includes('.') ? item.name.split('.').pop()!.toLowerCase() : '';
  const cls = 'w-4 h-4';
  if (mime.startsWith('image/')) return <ImageIcon className={`${cls} text-purple-500`} />;
  if (mime.includes('pdf') || ext === 'pdf') return <FileText className={`${cls} text-red-500`} />;
  if (['xls', 'xlsx', 'csv'].includes(ext) || mime.includes('excel') || mime.includes('spreadsheet')) {
    return <FileSpreadsheet className={`${cls} text-green-600`} />;
  }
  if (['zip', 'rar', '7z', 'tar', 'gz'].includes(ext)) return <FileArchive className={`${cls} text-amber-600`} />;
  if (['doc', 'docx'].includes(ext) || mime.includes('word')) return <FileText className={`${cls} text-blue-600`} />;
  return <FileText className={`${cls} text-gray-500`} />;
};

const statusTag = (item: AttachmentFieldItem) => {
  switch (item.status) {
    case 'pending':
      return <Tag style={{ marginInlineEnd: 0 }}>待上传</Tag>;
    case 'uploading':
      return <Tag color="processing" style={{ marginInlineEnd: 0 }}>上传中</Tag>;
    case 'done':
      return <Tag color="success" style={{ marginInlineEnd: 0 }}>已上传</Tag>;
    case 'error':
      return <Tag color="error" style={{ marginInlineEnd: 0 }}>上传失败</Tag>;
    default:
      return null;
  }
};

const AttachmentField: React.FC<AttachmentFieldProps> = ({
  value,
  onChange,
  disabled = false,
  maxCount = 10,
  maxSizeMB = 50,
  accept = ACCEPT_ATTACHMENT_STRING,
  uploader,
  onDeleteUploaded,
  title = '附件',
  showTitle = true,
  hint,
  dataTestId = 'attachment-field',
}) => {
  const { message } = App.useApp();
  const controlled = value !== undefined;
  const [innerList, setInnerList] = useState<AttachmentFieldItem[]>([]);
  const list = controlled ? value || [] : innerList;

  // 用 ref 保存最新列表：上传进度回调高频触发时避免闭包读到旧值
  const listRef = useRef<AttachmentFieldItem[]>(list);
  useEffect(() => {
    listRef.current = list;
  }, [list]);

  const emit = useCallback(
    (updater: (prev: AttachmentFieldItem[]) => AttachmentFieldItem[]) => {
      const next = updater(listRef.current);
      listRef.current = next;
      if (!controlled) setInnerList(next);
      onChange?.(next);
    },
    [controlled, onChange]
  );

  const patchItem = useCallback(
    (uid: string, patch: Partial<AttachmentFieldItem>) => {
      emit((prev) => prev.map((item) => (item.uid === uid ? { ...item, ...patch } : item)));
    },
    [emit]
  );

  const runUpload = useCallback(
    async (target: AttachmentFieldItem) => {
      if (!uploader || !target.file) return;
      patchItem(target.uid, { status: 'uploading', progress: 0, error: undefined });
      try {
        const uploaded = await uploader(target.file, (percent) => patchItem(target.uid, { progress: percent }));
        patchItem(target.uid, {
          status: 'done',
          attachmentId: uploaded.id,
          url: uploaded.url,
          progress: 100,
          error: undefined,
        });
      } catch (e) {
        const reason = e instanceof Error ? e.message : '上传失败';
        patchItem(target.uid, { status: 'error', error: reason });
        message.error(`「${target.name}」上传失败：${reason}`);
      }
    },
    [message, patchItem, uploader]
  );

  const handleFiles = useCallback(
    (incoming: File[]) => {
      if (incoming.length === 0) return;

      const currentCount = listRef.current.length;
      if (currentCount + incoming.length > maxCount) {
        message.warning(`最多上传 ${maxCount} 个附件，当前已有 ${currentCount} 个`);
        return;
      }

      const accepted: File[] = [];
      incoming.forEach((file) => {
        const verdict = validateAttachmentFile(file, { maxSizeMB });
        if (!verdict.ok) {
          message.warning(verdict.reason);
          return;
        }
        accepted.push(file);
      });
      if (accepted.length === 0) return;

      const items = createAttachmentItems(accepted);
      emit((prev) => [...prev, ...items]);

      if (uploader) {
        items.forEach((item) => {
          void runUpload(item);
        });
      }
    },
    [emit, maxCount, maxSizeMB, message, runUpload, uploader]
  );

  const handleRemove = useCallback(
    async (item: AttachmentFieldItem) => {
      if (item.attachmentId && onDeleteUploaded) {
        try {
          await onDeleteUploaded(item);
        } catch (e) {
          const reason = e instanceof Error ? e.message : '删除失败';
          message.error(`「${item.name}」删除失败：${reason}`);
          return;
        }
      }
      emit((prev) => prev.filter((entry) => entry.uid !== item.uid));
    },
    [emit, message, onDeleteUploaded]
  );

  const pendingCount = list.filter((item) => item.status === 'pending').length;
  const errorCount = list.filter((item) => item.status === 'error').length;
  const uploadingCount = list.filter((item) => item.status === 'uploading').length;

  return (
    <div data-testid={dataTestId}>
      {showTitle && (
        <div className="mb-2 flex items-center justify-between">
          <Space size={6}>
            <Paperclip className="w-4 h-4 text-gray-500" aria-hidden="true" />
            <Text strong>{title}</Text>
            {list.length > 0 && (
              <Text type="secondary" style={{ fontSize: 12 }}>
                {list.length}/{maxCount}
              </Text>
            )}
          </Space>
          {uploadingCount > 0 && (
            <Text type="secondary" style={{ fontSize: 12 }}>
              正在上传 {uploadingCount} 个…
            </Text>
          )}
        </div>
      )}

      <Upload.Dragger
        multiple
        disabled={disabled}
        accept={accept}
        fileList={[]}
        showUploadList={false}
        beforeUpload={(file) => {
          handleFiles([file as unknown as File]);
          return false;
        }}
        aria-label="选择或拖拽附件上传"
        data-testid={`${dataTestId}-dragger`}
      >
        <p className="ant-upload-drag-icon" style={{ marginBottom: 4 }}>
          <UploadCloud className="w-6 h-6 mx-auto text-gray-400" aria-hidden="true" />
        </p>
        <p className="ant-upload-text" style={{ fontSize: 13 }}>
          点击或拖拽文件到此处
        </p>
        <p className="ant-upload-hint" style={{ fontSize: 12 }}>
          {hint ?? `支持文档 / 表格 / 图片 / 压缩包，单文件不超过 ${maxSizeMB}MB，最多 ${maxCount} 个`}
        </p>
      </Upload.Dragger>

      {list.length > 0 && (
        <ul className="mt-3 list-none p-0 m-0" data-testid={`${dataTestId}-list`}>
          {list.map((item) => (
            <li
              key={item.uid}
              className="flex items-center gap-2 py-1.5 border-b border-gray-100 last:border-b-0"
              data-testid={`${dataTestId}-item-${item.uid}`}
            >
              <span aria-hidden="true">{renderFileIcon(item)}</span>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <Text
                    ellipsis={{ tooltip: item.name }}
                    style={{ fontSize: 13, maxWidth: 220 }}
                  >
                    {item.name}
                  </Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {formatAttachmentSize(item.size)}
                  </Text>
                  {statusTag(item)}
                </div>
                {item.status === 'uploading' && (
                  <Progress
                    percent={item.progress ?? 0}
                    size="small"
                    showInfo={false}
                    style={{ marginBottom: 0, maxWidth: 280 }}
                  />
                )}
                {item.status === 'error' && item.error && (
                  <Text type="danger" style={{ fontSize: 12 }}>
                    {item.error}
                  </Text>
                )}
              </div>

              {item.status === 'error' && uploader && (
                <Tooltip title="重试">
                  <Button
                    type="text"
                    size="small"
                    icon={<RotateCcw className="w-4 h-4" />}
                    onClick={() => void runUpload(item)}
                    aria-label={`重试上传 ${item.name}`}
                  />
                </Tooltip>
              )}

              <Tooltip title={item.status === 'done' && item.attachmentId ? '删除（同时解绑后端附件）' : '移除'}>
                <Button
                  type="text"
                  size="small"
                  danger
                  disabled={disabled || item.status === 'uploading'}
                  icon={<Trash2 className="w-4 h-4" />}
                  onClick={() => void handleRemove(item)}
                  aria-label={`移除附件 ${item.name}`}
                />
              </Tooltip>
            </li>
          ))}
        </ul>
      )}

      {errorCount > 0 && (
        <Text type="warning" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
          {errorCount} 个附件上传失败，可点击重试；提交工单不会因此中断。
        </Text>
      )}
      {pendingCount > 0 && !uploader && (
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
          {pendingCount} 个附件将在工单创建成功后自动上传。
        </Text>
      )}
    </div>
  );
};

export default AttachmentField;
