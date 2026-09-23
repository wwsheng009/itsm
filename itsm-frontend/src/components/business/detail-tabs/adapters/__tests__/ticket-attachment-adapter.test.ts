/**
 * FE-3 回归：工单附件适配器统一走 AttachmentApi（唯一附件 HTTP 实现）。
 *
 * 覆盖：
 *   - 宿主上下文显式传 { bizType: 'ticket', bizId }（usage 缺省 attachment）
 *   - AttachmentRef → AttachmentItem 字段映射（fileUrl / mimeType / uploadedBy / createdAt）
 *   - 下载 / 预览 / 删除委托 AttachmentApi 别名端点，不再触碰 TicketAttachmentApi
 */

import type { AttachmentRef } from '@/lib/upload/types';
import { ticketAttachmentAdapter } from '../ticket-attachment-adapter';

const mockList = jest.fn();
const mockUpload = jest.fn();
const mockRemoveById = jest.fn();

jest.mock('@/lib/api/attachment-api', () => ({
  // URL 辅助函数保留真实实现：本用例要钉住的是工单域 URL 形态（BE-10），
  // 用 mock 复刻等于把断言写在了假实现上。
  ...jest.requireActual('@/lib/api/attachment-api'),
  AttachmentApi: {
    list: (...args: unknown[]) => mockList(...args),
    upload: (...args: unknown[]) => mockUpload(...args),
    removeById: (...args: unknown[]) => mockRemoveById(...args),
  },
}));

// AttachmentRef 测试工厂：只填测试关注字段，其余给合法默认值。
// 经 Object.assign 合并 Partial 覆盖（对象展开会把必填字段推断为 optional）。
const REF_DEFAULTS: AttachmentRef = {
  id: 5,
  bizType: 'ticket',
  bizId: 7,
  usage: 'attachment',
  fileName: '需求文档.docx',
  fileSize: 2048,
  mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  fileUrl: '/api/v1/attachments/5/content',
  uploadedBy: 9,
  createdAt: '2026-02-01T10:00:00Z',
};

const makeRef = (overrides: Partial<AttachmentRef> = {}): AttachmentRef =>
  Object.assign({}, REF_DEFAULTS, overrides);

describe('ticket-attachment-adapter (FE-3)', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('list', () => {
    it('使用工单宿主上下文拉取列表并把 AttachmentRef 映射为 AttachmentItem', async () => {
      mockList.mockResolvedValue({ attachments: [makeRef()], total: 1 });

      const items = await ticketAttachmentAdapter.list(123);

      expect(mockList).toHaveBeenCalledWith({ bizType: 'ticket', bizId: 123 });
      expect(items).toEqual([
        {
          id: 5,
          fileName: '需求文档.docx',
          fileSize: 2048,
          mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
          fileUrl: '/api/v1/attachments/5/content',
          createdAt: '2026-02-01T10:00:00Z',
          uploader: { id: 9 },
        },
      ]);
    });

    it('字符串 targetId 归一为数字 bizId；缺省字段降级不报错', async () => {
      mockList.mockResolvedValue({
        attachments: [
          makeRef({ id: 6, createdAt: undefined, uploadedBy: undefined, fileUrl: undefined }),
        ],
        total: 1,
      });

      const items = await ticketAttachmentAdapter.list('123');

      expect(mockList).toHaveBeenCalledWith({ bizType: 'ticket', bizId: 123 });
      expect(items[0]).toMatchObject({ id: 6, createdAt: '', fileUrl: undefined });
      expect(items[0].uploader).toBeUndefined();
    });

    it('空列表安全返回', async () => {
      mockList.mockResolvedValue({ attachments: [], total: 0 });

      await expect(ticketAttachmentAdapter.list(1)).resolves.toEqual([]);
    });
  });

  describe('upload', () => {
    it('携带进度回调上传并返回映射后的条目', async () => {
      const file = new File(['x'], 'img.png', { type: 'image/png' });
      const onProgress = jest.fn();
      mockUpload.mockResolvedValue(
        makeRef({ id: 7, previewUrl: '/api/v1/attachments/7/content?disposition=inline' })
      );

      const item = await ticketAttachmentAdapter.upload(123, file, onProgress);

      expect(mockUpload).toHaveBeenCalledWith(
        file,
        { bizType: 'ticket', bizId: 123 },
        { onProgress }
      );
      expect(item.id).toBe(7);
      expect(item.fileUrl).toBe('/api/v1/attachments/5/content');
    });
  });

  describe('url helpers', () => {
    it('下载 / 预览 URL 走工单域内端点（通用 A4 兜底码仅 admin 持有）', () => {
      expect(ticketAttachmentAdapter.getDownloadUrl(123, 5)).toBe(
        '/api/v1/tickets/123/attachments/5'
      );
      expect(ticketAttachmentAdapter.getPreviewUrl?.(123, 5)).toBe(
        '/api/v1/tickets/123/attachments/5/preview'
      );
    });
  });

  describe('remove', () => {
    it('按附件 id + 工单宿主上下文删除', async () => {
      mockRemoveById.mockResolvedValue(undefined);

      await ticketAttachmentAdapter.remove(123, 5);

      expect(mockRemoveById).toHaveBeenCalledWith(5, { bizType: 'ticket', bizId: 123 });
    });
  });
});
