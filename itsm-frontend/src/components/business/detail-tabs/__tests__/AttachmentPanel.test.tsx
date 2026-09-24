/**
 * AttachmentPanel 预览回归测试
 *
 * 背景：内容接口响应统一带 `X-Frame-Options: DENY`（middleware/security.go），
 * 直接把接口地址塞进 <iframe> 会被浏览器拦截，页面表现为“拒绝连接”。
 * 这里锁定修复后的行为：
 *   - 预览经 httpClient 以 blob 方式带凭证拉取，再用 blob URL 渲染；
 *   - 图片渲染 <img src="blob:...">，其他可预览类型渲染 <iframe src="blob:...">；
 *   - 接口失败（如文件缺失返回 4004）展示可读错误，而不是空白或“拒绝连接”。
 */

import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { App } from 'antd';
import { AttachmentPanel } from '../AttachmentPanel';
import type { AttachmentAdapter, AttachmentItem } from '../types';

const mockRequest = jest.fn();
jest.mock('@/lib/api/http-client', () => ({
  httpClient: { request: (...args: unknown[]) => mockRequest(...args) },
}));

const dict: Record<string, string> = {
  'attachments.preview': '预览',
  'attachments.download': '下载',
  'attachments.upload': '上传',
  'attachments.previewFailed': '预览加载失败，请下载后查看',
};
const mockT = jest.fn((key: string, options?: unknown) =>
  typeof options === 'string' ? options : (dict[key] ?? key),
);
jest.mock('@/lib/i18n/useI18n', () => ({
  useI18n: () => ({ t: mockT, language: 'zh-CN' }),
}));

const makeItem = (over: Partial<AttachmentItem> = {}): AttachmentItem => ({
  id: 27,
  fileName: '20836938.png',
  fileSize: 1024,
  mimeType: 'image/png',
  createdAt: '2026-09-24T06:14:00Z',
  ...over,
});

const makeAdapter = (items: AttachmentItem[]): AttachmentAdapter => ({
  list: jest.fn().mockResolvedValue(items),
  upload: jest.fn(),
  remove: jest.fn(),
  getDownloadUrl: (targetId, attachmentId) =>
    `/api/v1/tickets/${targetId}/attachments/${attachmentId}`,
  getPreviewUrl: (targetId, attachmentId) =>
    `/api/v1/tickets/${targetId}/attachments/${attachmentId}/preview`,
});

const renderPanel = (items: AttachmentItem[]) =>
  render(
    <App>
      <AttachmentPanel targetType="ticket" targetId={10} adapter={makeAdapter(items)} />
    </App>,
  );

describe('AttachmentPanel 预览', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    Object.defineProperty(global.URL, 'createObjectURL', {
      writable: true,
      value: jest.fn(() => 'blob:http://localhost/attachment-27'),
    });
    Object.defineProperty(global.URL, 'revokeObjectURL', {
      writable: true,
      value: jest.fn(),
    });
  });

  it('图片预览经 blob 拉取，并用 <img> 渲染而非直接内嵌接口地址', async () => {
    mockRequest.mockResolvedValueOnce(new Blob(['png-bytes'], { type: 'image/png' }));
    renderPanel([makeItem()]);

    fireEvent.click(await screen.findByText('预览'));

    await waitFor(() => {
      expect(mockRequest).toHaveBeenCalledWith({
        url: '/api/v1/tickets/10/attachments/27/preview',
        method: 'GET',
        responseType: 'blob',
      });
    });

    const img = await screen.findByAltText('20836938.png');
    expect(img.getAttribute('src')).toBe('blob:http://localhost/attachment-27');
    expect(document.querySelector('iframe')).toBeNull();
  });

  it('PDF 预览用 blob URL 承载 iframe，地址不再是接口 URL', async () => {
    mockRequest.mockResolvedValueOnce(new Blob(['pdf-bytes'], { type: 'application/pdf' }));
    renderPanel([makeItem({ id: 30, fileName: 'guide.pdf', mimeType: 'application/pdf' })]);

    fireEvent.click(await screen.findByText('预览'));

    const frame = await waitFor(() => {
      const el = document.querySelector('iframe');
      expect(el).toBeTruthy();
      return el as HTMLIFrameElement;
    });
    expect(frame.getAttribute('src')).toBe('blob:http://localhost/attachment-27');
    expect(frame.getAttribute('src')).not.toContain('/api/v1/');
  });

  it('接口失败（如附件文件缺失）展示可读错误，而不是空白或拒绝连接', async () => {
    mockRequest.mockRejectedValueOnce(new Error('附件不存在或无法访问'));
    renderPanel([makeItem()]);

    fireEvent.click(await screen.findByText('预览'));

    expect(await screen.findByText('附件不存在或无法访问')).toBeInTheDocument();
    expect(document.querySelector('iframe')).toBeNull();
  });

  it('关闭弹窗会释放 blob URL', async () => {
    mockRequest.mockResolvedValueOnce(new Blob(['png-bytes'], { type: 'image/png' }));
    renderPanel([makeItem()]);

    fireEvent.click(await screen.findByText('预览'));
    await screen.findByAltText('20836938.png');

    fireEvent.click(document.querySelector('.ant-modal-close') as HTMLElement);

    await waitFor(() => {
      expect(global.URL.revokeObjectURL).toHaveBeenCalledWith('blob:http://localhost/attachment-27');
    });
  });
});
