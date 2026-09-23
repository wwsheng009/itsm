/**
 * FE-1 契约用例：公共类型 / 默认上传器工厂 / 类型守卫 / 10MB 统一上限。
 * 方案：docs/plan/generic-attachment-richtext-control-plan.md §3.1、§3.4 限额决策、§7 FE-1。
 */

import {
  createAttachmentDeleter,
  createAttachmentUploader,
  isAttachmentHostContext,
  isAttachmentRef,
  isAttachmentUsage,
  normalizeAttachmentHost,
  type AttachmentTransport,
} from '@/lib/upload/attachment-uploader';
import { DEFAULT_ATTACHMENT_MAX_SIZE_MB, type AttachmentRef } from '@/lib/upload/types';
import { validateAttachmentFile } from '@/components/business/AttachmentField';

const sampleRef: AttachmentRef = {
  id: 7,
  bizType: 'ticket',
  bizId: 10,
  usage: 'comment_attachment',
  fileName: 'a.png',
  fileSize: 12,
  mimeType: 'image/png',
};

/** 构造指定字节数的文件（不实际分配内存） */
const fileOfSize = (name: string, bytes: number, type = 'application/pdf'): File => {
  const file = new File(['x'], name, { type });
  Object.defineProperty(file, 'size', { value: bytes });
  return file;
};

describe('FE-1 公共契约', () => {
  it('单文件上限统一为 10MB（v1.0），前端默认值不得放宽到 50MB', () => {
    expect(DEFAULT_ATTACHMENT_MAX_SIZE_MB).toBe(10);

    const elevenMB = fileOfSize('big.pdf', 11 * 1024 * 1024);
    // FE-7：校验结论新增 `code`（i18n key 由 code 决定），中文 reason 仍为无 i18n 上下文的兜底
    expect(validateAttachmentFile(elevenMB)).toEqual({
      ok: false,
      code: 'tooLarge',
      reason: '「big.pdf」超过 10MB 上限',
    });

    const exactlyTenMB = fileOfSize('ok.pdf', 10 * 1024 * 1024);
    expect(validateAttachmentFile(exactlyTenMB)).toEqual({ ok: true });
  });

  describe('类型守卫', () => {
    it('isAttachmentUsage 只接受契约内的三种用途', () => {
      expect(isAttachmentUsage('attachment')).toBe(true);
      expect(isAttachmentUsage('inline_image')).toBe(true);
      expect(isAttachmentUsage('comment_attachment')).toBe(true);
      expect(isAttachmentUsage('avatar')).toBe(false);
      expect(isAttachmentUsage(undefined)).toBe(false);
    });

    it('isAttachmentHostContext 要求 bizType 非空 + bizId 正整数', () => {
      expect(isAttachmentHostContext({ bizType: 'ticket', bizId: 10 })).toBe(true);
      expect(isAttachmentHostContext({ bizType: 'ticket', bizId: 10, usage: 'comment_attachment' })).toBe(true);
      expect(isAttachmentHostContext({ bizType: '  ', bizId: 10 })).toBe(false);
      expect(isAttachmentHostContext({ bizType: 'ticket', bizId: 0 })).toBe(false);
      expect(isAttachmentHostContext({ bizType: 'ticket', bizId: 1.5 })).toBe(false);
      expect(isAttachmentHostContext({ bizType: 'ticket', bizId: 10, usage: 'avatar' })).toBe(false);
      expect(isAttachmentHostContext(null)).toBe(false);
    });

    it('isAttachmentRef 覆盖 id / 宿主 / usage / 文件名等契约字段', () => {
      expect(isAttachmentRef(sampleRef)).toBe(true);
      expect(isAttachmentRef({ ...sampleRef, id: 0 })).toBe(false);
      expect(isAttachmentRef({ ...sampleRef, usage: 'avatar' })).toBe(false);
      expect(isAttachmentRef({ ...sampleRef, bizType: '' })).toBe(false);
      expect(isAttachmentRef({ ...sampleRef, fileName: undefined })).toBe(false);
      expect(isAttachmentRef('not-an-object')).toBe(false);
    });

    it('normalizeAttachmentHost 补默认用途并裁剪空白，非法输入抛错', () => {
      expect(normalizeAttachmentHost({ bizType: ' ticket ', bizId: 3 })).toEqual({
        bizType: 'ticket',
        bizId: 3,
        usage: 'attachment',
      });
      expect(normalizeAttachmentHost({ bizType: 'ticket', bizId: 3, usage: 'inline_image' }).usage).toBe('inline_image');
      expect(() => normalizeAttachmentHost({ bizType: '', bizId: 3 })).toThrow(/宿主上下文非法/);
    });
  });

  describe('createAttachmentUploader', () => {
    it('归一化宿主、透传进度并返回契约内的附件引用', async () => {
      const upload = jest.fn().mockResolvedValue(sampleRef);
      const transport: AttachmentTransport = { upload, remove: jest.fn() };
      const uploader = createAttachmentUploader(transport);
      const onProgress = jest.fn();
      const file = new File(['x'], 'a.png', { type: 'image/png' });

      const result = await uploader(file, { bizType: ' ticket ', bizId: 10 }, onProgress);

      expect(upload).toHaveBeenCalledWith(file, { bizType: 'ticket', bizId: 10, usage: 'attachment' }, onProgress);
      expect(result).toEqual(sampleRef);
    });

    it('宿主非法时直接拒绝且不触发传输层', async () => {
      const upload = jest.fn();
      const uploader = createAttachmentUploader({ upload });

      await expect(uploader(new File(['x'], 'a.png'), { bizType: 'ticket', bizId: -1 })).rejects.toThrow(
        /宿主上下文非法/
      );
      expect(upload).not.toHaveBeenCalled();
    });

    it('响应缺少契约字段时立即暴露，避免脏数据流入宿主表单', async () => {
      const upload = jest.fn().mockResolvedValue({ id: 1, fileName: 'a.png' });
      const uploader = createAttachmentUploader({ upload });

      await expect(uploader(new File(['x'], 'a.png'), { bizType: 'ticket', bizId: 10 })).rejects.toThrow(
        /响应结构非法/
      );
    });
  });

  describe('createAttachmentDeleter', () => {
    it('按附件 ID 调用删除传输层', async () => {
      const remove = jest.fn().mockResolvedValue(undefined);
      const deleter = createAttachmentDeleter({ remove });

      await deleter(sampleRef);

      expect(remove).toHaveBeenCalledWith(sampleRef.id);
    });

    it('非法引用不触发删除', async () => {
      const remove = jest.fn();
      const deleter = createAttachmentDeleter({ remove });

      await expect(deleter({ ...sampleRef, id: 0 })).rejects.toThrow(/附件引用非法/);
      expect(remove).not.toHaveBeenCalled();
    });
  });
});
