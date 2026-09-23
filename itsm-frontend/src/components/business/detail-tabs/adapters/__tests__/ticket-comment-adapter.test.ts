/**
 * FE-9 回归：评论附件「先上传后绑定」契约（usage=comment_attachment）。
 *
 * 覆盖：
 *   - 上传宿主上下文固定为 { bizType: 'ticket', bizId, usage: 'comment_attachment' }
 *     （BE-10：工单一律走域内端点，普通用户不会命中兜底码 attachment:*）
 *   - 展示地址优先 previewUrl，缺失时回落 fileUrl
 *   - 解绑走 AttachmentApi.removeById 同一宿主上下文
 *   - 适配器声明评论附件能力，CommentPanel 据此展示 / 隐藏附件入口
 */

import { ticketCommentAdapter } from '../ticket-comment-adapter';

const mockUpload = jest.fn();
const mockRemoveById = jest.fn();

jest.mock('@/lib/api/attachment-api', () => ({
  ...jest.requireActual('@/lib/api/attachment-api'),
  AttachmentApi: {
    upload: (...args: unknown[]) => mockUpload(...args),
    removeById: (...args: unknown[]) => mockRemoveById(...args),
  },
}));

jest.mock('@/lib/api/ticket-comment-api', () => ({
  TicketCommentApi: {
    getComments: jest.fn(),
    createComment: jest.fn(),
    updateComment: jest.fn(),
    deleteComment: jest.fn(),
  },
}));

const HOST = { bizType: 'ticket', bizId: 7, usage: 'comment_attachment' };

describe('ticketCommentAdapter 评论附件（FE-9）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('uploadAttachment：宿主上下文固定 comment_attachment，返回 id 与展示地址', async () => {
    mockUpload.mockResolvedValue({
      id: 42,
      bizType: 'ticket',
      bizId: 7,
      usage: 'comment_attachment',
      fileName: '截图.png',
      fileSize: 1024,
      mimeType: 'image/png',
      previewUrl: '/api/v1/tickets/7/attachments/42/preview',
    });
    const file = new File(['x'], '截图.png', { type: 'image/png' });
    const onProgress = jest.fn();

    const result = await ticketCommentAdapter.uploadAttachment!(7, file, onProgress);

    expect(mockUpload).toHaveBeenCalledWith(file, HOST, { onProgress });
    expect(result).toEqual({ id: 42, url: '/api/v1/tickets/7/attachments/42/preview' });
  });

  it('uploadAttachment：非图片无 previewUrl 时回落 fileUrl', async () => {
    mockUpload.mockResolvedValue({
      id: 43,
      bizType: 'ticket',
      bizId: 7,
      usage: 'comment_attachment',
      fileName: '需求.docx',
      fileSize: 2048,
      mimeType: 'application/msword',
      fileUrl: '/api/v1/tickets/7/attachments/43',
    });

    const result = await ticketCommentAdapter.uploadAttachment!(
      7,
      new File(['x'], '需求.docx')
    );

    expect(result).toEqual({ id: 43, url: '/api/v1/tickets/7/attachments/43' });
  });

  it('removeAttachment：解绑未绑定附件走同一宿主上下文', async () => {
    mockRemoveById.mockResolvedValue(undefined);

    await ticketCommentAdapter.removeAttachment!(7, 42);

    expect(mockRemoveById).toHaveBeenCalledWith(42, HOST);
  });

  it('适配器声明评论附件能力（CommentPanel 据此决定是否渲染附件入口）', () => {
    expect(typeof ticketCommentAdapter.uploadAttachment).toBe('function');
    expect(typeof ticketCommentAdapter.removeAttachment).toBe('function');
  });
});
