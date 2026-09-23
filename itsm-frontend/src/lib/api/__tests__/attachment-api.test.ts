/**
 * FE-4 验收：通用附件客户端 `AttachmentApi` 的端点解析与契约护栏。
 *
 * 覆盖计划 §2.1（唯一 http-client 入口）、§3.2（A1-A6 路由）、§7 FE-4：
 *  - 工单 → 一律域内端点（D5 不破坏、权限沿用宿主资源码）；非默认用途由 BE-10 透传通用表，
 *    不能走通用 A1：通用兜底码 attachment:write 仅 admin/sysadmin 持有，普通用户必 403；
 *  - knowledge_article / service_request → BE-5 域内别名路由；
 *  - 其它宿主 → 通用 A1-A6；
 *  - 旧响应 → 统一契约补齐；非法响应必须抛错而不是静默通过。
 */

import { AttachmentApi } from '@/lib/api/attachment-api';
import { httpClient } from '@/lib/api/http-client';

jest.mock('@/lib/api/http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    delete: jest.fn(),
    patch: jest.fn(),
  },
}));

jest.mock('@/lib/api/api-config', () => ({
  API_BASE_URL: 'http://localhost:8090',
}));

const mockGet = httpClient.get as jest.Mock;
const mockPost = httpClient.post as jest.Mock;
const mockDelete = httpClient.delete as jest.Mock;

/** 通用契约响应样本（后端 AttachmentRef 形状） */
function refPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    bizType: 'ticket',
    bizId: 10,
    usage: 'attachment',
    fileName: 'a.png',
    fileSize: 12,
    mimeType: 'image/png',
    fileUrl: '/api/v1/attachments/7/content',
    previewUrl: '/api/v1/attachments/7/content?disposition=inline',
    ...overrides,
  };
}

const file = () => new File(['x'], 'a.png', { type: 'image/png' });

describe('AttachmentApi 端点解析', () => {
  beforeEach(() => jest.clearAllMocks());

  describe('upload (A1)', () => {
    it('工单默认用途走旧域内端点，且只提交 file 字段', async () => {
      mockPost.mockResolvedValue({ id: 7, ticketId: 10, fileName: 'a.png' });

      const result = await AttachmentApi.upload(file(), { bizType: 'ticket', bizId: 10 });

      const [url, body] = mockPost.mock.calls[0];
      expect(url).toBe('/api/v1/tickets/10/attachments');
      expect([...(body as FormData).keys()]).toEqual(['file']);
      // 旧响应补齐契约字段 + 沿用旧预览地址
      expect(result).toMatchObject({
        id: 7,
        bizType: 'ticket',
        bizId: 10,
        usage: 'attachment',
        fileName: 'a.png',
        previewUrl: '/api/v1/tickets/10/attachments/7/preview',
      });
    });

    it('工单非默认用途同样走域内端点（BE-10），显式携带 usage', async () => {
      mockPost.mockResolvedValue({
        id: 7,
        ticketId: 10,
        fileName: 'a.png',
        usage: 'comment_attachment',
      });

      await AttachmentApi.upload(file(), {
        bizType: 'ticket',
        bizId: 10,
        usage: 'comment_attachment',
      });

      const [url, body] = mockPost.mock.calls[0];
      expect(url).toBe('/api/v1/tickets/10/attachments');
      expect((body as FormData).get('usage')).toBe('comment_attachment');
      // 域内端点由路径携带宿主，不得再冗余提交 bizType/bizId
      expect((body as FormData).get('bizType')).toBeNull();
      expect((body as FormData).get('bizId')).toBeNull();
    });

    it('knowledge_article 走 BE-5 域内别名路由并保留 usage 语义', async () => {
      mockPost.mockResolvedValue(refPayload({ bizType: 'knowledge_article', bizId: 5, usage: 'inline_image' }));

      await AttachmentApi.upload(file(), {
        bizType: 'knowledge_article',
        bizId: 5,
        usage: 'inline_image',
      });

      const [url, body] = mockPost.mock.calls[0];
      expect(url).toBe('/api/v1/knowledge/articles/5/attachments');
      expect((body as FormData).get('usage')).toBe('inline_image');
    });

    it('service_request 走 BE-5 域内别名路由', async () => {
      mockPost.mockResolvedValue(refPayload({ bizType: 'service_request', bizId: 3 }));

      await AttachmentApi.upload(file(), { bizType: 'service_request', bizId: 3 });

      expect(mockPost.mock.calls[0][0]).toBe('/api/v1/service-requests/3/attachments');
    });

    it('未知宿主走通用 A1，支持 clientToken 幂等回放', async () => {
      mockPost.mockResolvedValue(refPayload({ bizType: 'other_host', bizId: 1 }));

      await AttachmentApi.upload(file(), { bizType: 'other_host', bizId: 1 }, { clientToken: 'tok-1' });

      const [url, body] = mockPost.mock.calls[0];
      expect(url).toBe('/api/v1/attachments');
      expect((body as FormData).get('clientToken')).toBe('tok-1');
    });

    it('宿主上下文非法时抛错，不发请求', async () => {
      await expect(AttachmentApi.upload(file(), { bizType: 'ticket', bizId: 0 })).rejects.toThrow();
      expect(mockPost).not.toHaveBeenCalled();
    });

    it('通用端点返回非契约结构时必须抛错（防止静默透传脏数据）', async () => {
      mockPost.mockResolvedValue({ id: 7 } as unknown as never);

      await expect(AttachmentApi.upload(file(), { bizType: 'other_host', bizId: 1 })).rejects.toThrow(
        /响应结构非法/
      );
    });
  });

  describe('list (A2)', () => {
    it('工单域内列表保持旧 URL 并映射为契约数组', async () => {
      mockGet.mockResolvedValue({ attachments: [{ id: 1, ticketId: 10, fileName: 'x.pdf' }], total: 1 });

      const result = await AttachmentApi.list({ bizType: 'ticket', bizId: 10 });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/tickets/10/attachments');
      expect(result.total).toBe(1);
      expect(result.attachments[0]).toMatchObject({ id: 1, bizType: 'ticket', bizId: 10 });
    });

    it('通用列表携带 bizType/bizId/usage 与分页参数', async () => {
      mockGet.mockResolvedValue({ attachments: [refPayload({ bizType: 'other_host', bizId: 2 })], total: 1 });

      await AttachmentApi.list({
        bizType: 'other_host',
        bizId: 2,
        usage: 'attachment',
        page: 1,
        pageSize: 20,
      });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/attachments', {
        bizType: 'other_host',
        bizId: 2,
        usage: 'attachment',
        page: 1,
        pageSize: 20,
      });
    });
  });

  describe('get / batchQuery (A3/A6)', () => {
    it('get 走通用明细路由并守卫响应', async () => {
      mockGet.mockResolvedValue(refPayload());
      const ref = await AttachmentApi.get(7);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/attachments/7');
      expect(ref.id).toBe(7);
    });

    it('batchQuery 提交 { ids } 并接受后端裸数组响应（handler.go BatchQuery）', async () => {
      mockPost.mockResolvedValue([refPayload()]);

      const refs = await AttachmentApi.batchQuery([7]);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/attachments/batch-query', { ids: [7] });
      expect(refs).toHaveLength(1);
    });

    it('batchQuery 空数组不发请求（A6 ≤200 条约束的前置短路）', async () => {
      await expect(AttachmentApi.batchQuery([])).resolves.toEqual([]);
      expect(mockPost).not.toHaveBeenCalled();
    });
  });

  describe('remove (A5)', () => {
    it('按引用解析：工单引用删除走旧域内 URL（权限沿用 ticket:delete）', async () => {
      mockDelete.mockResolvedValue(undefined);

      await AttachmentApi.remove(refPayload({ id: 9 }) as never);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/tickets/10/attachments/9');
    });

    it('未知宿主引用删除走通用 A5', async () => {
      mockDelete.mockResolvedValue(undefined);

      await AttachmentApi.remove(refPayload({ id: 9, bizType: 'other_host', bizId: 4 }) as never);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/attachments/9');
    });

    it('引用非法时抛错且不发请求', async () => {
      await expect(AttachmentApi.remove({ id: 9 } as never)).rejects.toThrow(/引用非法/);
      expect(mockDelete).not.toHaveBeenCalled();
    });
  });

  describe('地址与传输层端口', () => {
    it('内容 / 预览地址按 A4 约定生成', () => {
      expect(AttachmentApi.getContentUrl(7)).toBe('/api/v1/attachments/7/content');
      expect(AttachmentApi.getPreviewUrl(7)).toBe('/api/v1/attachments/7/content?disposition=inline');
    });

    it('uploader() 经 FE-1 工厂校验宿主后落到同一上传实现', async () => {
      mockPost.mockResolvedValue(refPayload());

      const upload = AttachmentApi.uploader();
      await upload(file(), { bizType: 'ticket', bizId: 10 });

      expect(mockPost.mock.calls[0][0]).toBe('/api/v1/tickets/10/attachments');
    });

    it('deleter() 按引用选择端点', async () => {
      mockDelete.mockResolvedValue(undefined);

      await AttachmentApi.deleter()(refPayload({ id: 9 }) as never);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/tickets/10/attachments/9');
    });

    it('transport().remove 无宿主上下文，恒走通用 A5', async () => {
      mockDelete.mockResolvedValue(undefined);

      await AttachmentApi.transport().remove(9);

      expect(mockDelete).toHaveBeenCalledWith('/api/v1/attachments/9');
    });
  });
});
