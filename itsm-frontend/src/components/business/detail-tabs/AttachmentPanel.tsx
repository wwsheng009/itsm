
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Upload,
  Button,
  App,
  List,
  Typography,
  Progress,
  Modal,
  Space,
  Empty,
  Spin,
  Alert,
} from 'antd';
import type { UploadFile, RcFile } from 'antd/es/upload/interface';
import {
  File as FileIcon,
  Image as ImageIcon,
  FileText,
  Music,
  Video,
  Archive,
  Download,
  Eye,
  Trash2,
  Upload as UploadIcon,
} from 'lucide-react';
import type { AttachmentAdapter, AttachmentItem, TargetType } from './types';
import { useI18n } from '@/lib/i18n/useI18n';
import { httpClient } from '@/lib/api/http-client';

const { Text } = Typography;

export interface AttachmentPanelProps {
  targetType: TargetType;
  targetId: number | string;
  adapter: AttachmentAdapter;
  maxSize?: number; // bytes, default 50MB
  accept?: string;
  currentUserId?: number;
  formatDateTime?: (dateString: string) => string;
}

interface AttachmentPreviewState {
  name: string;
  mimeType: string;
  loading: boolean;
  failed: boolean;
  url?: string;
  error?: string;
}

const formatFileSize = (bytes: number): string => {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i];
};

const getFileIcon = (mimeType: string) => {
  if (!mimeType) return <FileIcon size={20} />;
  if (mimeType.startsWith('image/')) return <ImageIcon size={20} />;
  if (mimeType.startsWith('video/')) return <Video size={20} />;
  if (mimeType.startsWith('audio/')) return <Music size={20} />;
  if (mimeType.includes('pdf') || mimeType.includes('word') || mimeType.includes('excel'))
    return <FileText size={20} />;
  if (mimeType.includes('zip') || mimeType.includes('rar')) return <Archive size={20} />;
  return <FileIcon size={20} />;
};

export const AttachmentPanel: React.FC<AttachmentPanelProps> = ({
  targetId,
  adapter,
  maxSize = 50 * 1024 * 1024,
  accept,
  currentUserId,
  formatDateTime,
}) => {
  const { t, language } = useI18n();
  const { message } = App.useApp();
  const [items, setItems] = useState<AttachmentItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [uploadProgress, setUploadProgress] = useState<number>(0);
  const [uploading, setUploading] = useState(false);
  const [preview, setPreview] = useState<AttachmentPreviewState | null>(null);

  // 预览统一走 blob URL，而不是把接口地址直接塞进 iframe：
  // 内容接口响应带 `X-Frame-Options: DENY`，直接内嵌会被浏览器拦成“拒绝连接”。
  const previewObjectUrlRef = useRef<string | null>(null);

  const locale = language === 'en-US' ? 'en-US' : 'zh-CN';
  const defaultFormat = useCallback(
    (s: string) => (s ? new Date(s).toLocaleString(locale) : ''),
    [locale],
  );
  const fmt = formatDateTime ?? defaultFormat;

  const fetchList = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await adapter.list(targetId);
      setItems(res || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : t('attachments.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [adapter, targetId, t]);

  useEffect(() => {
    void fetchList();
  }, [fetchList]);

  useEffect(
    () => () => {
      if (previewObjectUrlRef.current) {
        URL.revokeObjectURL(previewObjectUrlRef.current);
        previewObjectUrlRef.current = null;
      }
    },
    [],
  );

  const beforeUpload = (file: RcFile) => {
    if (file.size > maxSize) {
      message.error(t('attachments.sizeExceeded', { size: formatFileSize(maxSize) }));
      return Upload.LIST_IGNORE;
    }
    return true;
  };

  const customRequest = async (options: {
    file: File | RcFile | Blob;
    onSuccess?: (response: unknown) => void;
    onError?: (err: Error) => void;
  }) => {
    const file = options.file as File;
    setUploading(true);
    setUploadProgress(0);
    try {
      await adapter.upload(targetId, file, (p) => setUploadProgress(p));
      options.onSuccess?.({});
      message.success(t('attachments.uploadSuccess', { name: file.name }));
      await fetchList();
    } catch (e) {
      const err = e instanceof Error ? e : new Error(t('attachments.uploadFailed'));
      options.onError?.(err);
      message.error(err.message);
    } finally {
      setUploading(false);
      setUploadProgress(0);
    }
  };

  const handleDelete = (item: AttachmentItem) => {
    Modal.confirm({
      title: t('common.confirm'),
      content: t('attachments.deleteConfirm', { name: item.fileName }),
      okText: t('common.delete'),
      okType: 'danger',
      cancelText: t('common.cancel'),
      onOk: async () => {
        try {
          await adapter.remove(targetId, item.id);
          message.success(t('attachments.deleteSuccess'));
          await fetchList();
        } catch (e) {
          message.error(
            e instanceof Error ? e.message : t('attachments.deleteFailed'),
          );
        }
      },
    });
  };

  const handleDownload = (item: AttachmentItem) => {
    const url = adapter.getDownloadUrl(targetId, item.id);
    const a = document.createElement('a');
    a.href = url;
    a.download = item.fileName;
    a.target = '_blank';
    a.rel = 'noreferrer';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  };

  const closePreview = () => {
    if (previewObjectUrlRef.current) {
      URL.revokeObjectURL(previewObjectUrlRef.current);
      previewObjectUrlRef.current = null;
    }
    setPreview(null);
  };

  const handlePreview = async (item: AttachmentItem) => {
    if (!adapter.getPreviewUrl) {
      message.warning(t('attachments.previewNotSupported'));
      return;
    }

    const url = adapter.getPreviewUrl(targetId, item.id);
    const mimeType = item.mimeType || '';
    setPreview({ name: item.fileName, mimeType, loading: true, failed: false });

    try {
      // 带凭证（cookie + CSRF/租户头）拉取二进制后转 blob URL：blob 不受
      // X-Frame-Options 约束，同时接口异常（如文件已丢失的 4004）能拿到可读错误信息。
      const blob = await httpClient.request<Blob>({ url, method: 'GET', responseType: 'blob' });
      if (!blob || blob.size === 0) {
        throw new Error(t('attachments.previewFailed'));
      }
      if (previewObjectUrlRef.current) {
        URL.revokeObjectURL(previewObjectUrlRef.current);
      }
      previewObjectUrlRef.current = URL.createObjectURL(blob);
      setPreview({
        name: item.fileName,
        mimeType,
        loading: false,
        failed: false,
        url: previewObjectUrlRef.current,
      });
    } catch (e) {
      setPreview({
        name: item.fileName,
        mimeType,
        loading: false,
        failed: true,
        error: e instanceof Error ? e.message : t('attachments.previewFailed'),
      });
    }
  };

  const isPreviewable = (mime: string) =>
    mime.startsWith('image/') || mime.includes('pdf') || mime.startsWith('text/');

  const canManage = (a: AttachmentItem) =>
    currentUserId ? a.uploader?.id === currentUserId : true;

  if (loading && items.length === 0) {
    return (
      <div className="p-6 text-center">
        <Spin />
      </div>
    );
  }

  return (
    <div className="p-6">
      {error && (
        <Alert
          message={error}
          type="error"
          showIcon
          closable
          className="mb-4"
          onClose={() => setError(null)}
          action={
            <Button size="small" type="link" onClick={() => void fetchList()}>
              {t('common.retry')}
            </Button>
          }
        />
      )}

      <div className="mb-6">
        <Upload
          multiple
          showUploadList={false}
          beforeUpload={beforeUpload}
          customRequest={customRequest as never}
          accept={accept}
        >
          <Button icon={<UploadIcon size={14} />} loading={uploading}>
            {t('attachments.upload')}
          </Button>
        </Upload>
        {uploading && (
          <div className="mt-3">
            <Progress percent={Math.round(uploadProgress)} size="small" />
          </div>
        )}
        <Text type="secondary" className="ml-3 text-xs">
          {t('attachments.maxSize', { size: formatFileSize(maxSize) })}
        </Text>
      </div>

      {items.length === 0 ? (
        <Empty description={t('attachments.empty')} />
      ) : (
        <List
          itemLayout="horizontal"
          dataSource={items}
          renderItem={(item) => (
            <List.Item
              actions={[
                adapter.getPreviewUrl && isPreviewable(item.mimeType) ? (
                  <Button
                    key="preview"
                    type="link"
                    size="small"
                    icon={<Eye size={14} />}
                    onClick={() => void handlePreview(item)}
                  >
                    {t('attachments.preview')}
                  </Button>
                ) : null,
                <Button
                  key="download"
                  type="link"
                  size="small"
                  icon={<Download size={14} />}
                  onClick={() => handleDownload(item)}
                >
                  {t('attachments.download')}
                </Button>,
                canManage(item) ? (
                  <Button
                    key="delete"
                    type="link"
                    size="small"
                    danger
                    icon={<Trash2 size={14} />}
                    onClick={() => handleDelete(item)}
                  >
                    {t('common.delete')}
                  </Button>
                ) : null,
              ].filter(Boolean) as React.ReactNode[]}
            >
              <List.Item.Meta
                avatar={getFileIcon(item.mimeType)}
                title={item.fileName}
                description={
                  <Space size="small" wrap>
                    <Text type="secondary" className="text-xs">
                      {formatFileSize(item.fileSize)}
                    </Text>
                    <Text type="secondary" className="text-xs">
                      {item.uploader?.name ||
                        item.uploader?.username ||
                        t('detailTabs.unknownUser')}
                    </Text>
                    <Text type="secondary" className="text-xs">
                      {fmt(item.createdAt)}
                    </Text>
                  </Space>
                }
              />
            </List.Item>
          )}
        />
      )}

      <Modal
        title={preview?.name ?? ''}
        open={!!preview}
        onCancel={closePreview}
        footer={null}
        width={900}
      >
        {preview?.loading && (
          <div className="py-16 text-center">
            <Spin />
          </div>
        )}
        {preview?.failed && (
          <Alert type="error" showIcon message={preview.error || t('attachments.previewFailed')} />
        )}
        {preview?.url &&
          !preview.loading &&
          !preview.failed &&
          (preview.mimeType.startsWith('image/') ? (
            <img
              src={preview.url}
              alt={preview.name}
              style={{ display: 'block', width: '100%', maxHeight: '70vh', objectFit: 'contain' }}
            />
          ) : (
            <iframe
              src={preview.url}
              style={{ width: '100%', height: '70vh', border: 0 }}
              title={preview.name}
            />
          ))}
      </Modal>
    </div>
  );
};

export default AttachmentPanel;