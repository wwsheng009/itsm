/**
 * FE-9 回归：评论附件字段壳层与展示列表。
 *
 * 覆盖：
 *   - `CommentAttachmentField` 注入 `AttachmentField` 的契约（默认 10 条上限、
 *     即时上传模式、解绑回调只透传 attachmentId、未注入 remove 时不删服务端）
 *   - `CommentAttachmentList` 消费 BE-11 `attachmentRefs`：文件名 / 体积 / 下载 /
 *     预览链接，以及 `attachments` 多于元数据时的「已失效」占位
 */

import React from 'react';
import { render, screen } from '@testing-library/react';
import {
  CommentAttachmentField,
  CommentAttachmentList,
} from '../CommentAttachmentField';
import type { AttachmentFieldItem } from '../AttachmentField';
import { DEFAULT_ATTACHMENT_MAX_COUNT } from '@/lib/upload/types';

jest.mock('@/lib/i18n/useI18n', () => ({
  useI18n: () => ({ t: (key: string) => key, language: 'zh-CN' }),
}));

// 字段壳层只负责「契约转接」，因此这里替换掉真正的 AttachmentField（其行为由
// 原生 attachment-api / TicketAttachmentSection 用例覆盖），捕获被注入的 props。
let capturedProps: Record<string, unknown> = {};

jest.mock('../AttachmentField', () => ({
  __esModule: true,
  default: (props: Record<string, unknown>) => {
    capturedProps = props;
    return <div data-testid="attachment-field-stub" />;
  },
  formatAttachmentSize: (bytes: number) => `${bytes} B`,
}));

const ITEM: AttachmentFieldItem = {
  uid: 'att-1',
  name: '截图.png',
  size: 1024,
  type: 'image/png',
  attachmentId: 42,
  status: 'done',
  progress: 100,
};

describe('CommentAttachmentField（FE-9）', () => {
  beforeEach(() => {
    capturedProps = {};
  });

  it('默认 10 条上限并以即时上传模式注入 uploader', async () => {
    const upload = jest.fn().mockResolvedValue({ id: 42, url: '/api/v1/tickets/7/attachments/42/preview' });
    render(
      <CommentAttachmentField value={[]} onChange={jest.fn()} upload={upload} />
    );

    expect(capturedProps.maxCount).toBe(DEFAULT_ATTACHMENT_MAX_COUNT);
    expect(capturedProps.title).toBe('attachments.upload');
    expect(capturedProps.dataTestId).toBe('comment-attachment-field');

    const onProgress = jest.fn();
    const file = new File(['x'], '截图.png', { type: 'image/png' });
    await expect(
      (capturedProps.uploader as (f: File, p?: (n: number) => void) => Promise<unknown>)(
        file,
        onProgress
      )
    ).resolves.toEqual({ id: 42, url: '/api/v1/tickets/7/attachments/42/preview' });
    expect(upload).toHaveBeenCalledWith(file, onProgress);
  });

  it('注入 remove 时解绑回调按 attachmentId 透传', async () => {
    const remove = jest.fn().mockResolvedValue(undefined);
    render(
      <CommentAttachmentField
        value={[ITEM]}
        onChange={jest.fn()}
        upload={jest.fn()}
        remove={remove}
      />
    );

    await (capturedProps.onDeleteUploaded as (item: AttachmentFieldItem) => Promise<void>)(ITEM);

    expect(remove).toHaveBeenCalledWith(42);
  });

  it('未注入 remove 时不替换删除实现（AttachmentField 退化为仅本地移除）', () => {
    render(<CommentAttachmentField value={[]} onChange={jest.fn()} upload={jest.fn()} />);

    expect(capturedProps.onDeleteUploaded).toBeUndefined();
  });
});

describe('CommentAttachmentList（FE-9）', () => {
  const REFS = [
    {
      id: 42,
      fileName: '截图.png',
      fileSize: 2048,
      mimeType: 'image/png',
      downloadUrl: '/api/v1/tickets/7/attachments/42',
      previewUrl: '/api/v1/tickets/7/attachments/42/preview',
    },
    {
      id: 43,
      fileName: '需求.docx',
      fileSize: 4096,
      mimeType: 'application/msword',
      downloadUrl: '/api/v1/tickets/7/attachments/43',
    },
  ];

  it('渲染 BE-11 元数据：文件名、下载地址与图片预览地址', () => {
    render(<CommentAttachmentList refs={REFS} totalCount={2} />);

    expect(screen.getByText('截图.png')).toHaveAttribute('href', REFS[0].downloadUrl);
    expect(screen.getByText('需求.docx')).toHaveAttribute('href', REFS[1].downloadUrl);
    expect(screen.getAllByTestId('comment-attachment-item')).toHaveLength(2);
    // 图片有预览入口、文档没有
    expect(screen.getByLabelText('attachments.preview 截图.png')).toBeInTheDocument();
    expect(screen.queryByLabelText('attachments.preview 需求.docx')).not.toBeInTheDocument();
  });

  it('元数据缺失（attachments 中有已失效 ID）时给出失效占位而非静默丢弃', () => {
    render(<CommentAttachmentList refs={REFS} totalCount={3} />);

    expect(screen.getByTestId('comment-attachment-expired')).toHaveTextContent(
      'attachments.expired'
    );
  });

  it('无附件时不渲染任何内容', () => {
    const { container } = render(<CommentAttachmentList refs={[]} totalCount={0} />);

    expect(container).toBeEmptyDOMElement();
  });
});
