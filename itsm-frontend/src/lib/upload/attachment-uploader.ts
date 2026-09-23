/**
 * `AttachmentUploader` 默认实现（工厂）+ 类型守卫
 *
 * 依据：`docs/plan/generic-attachment-richtext-control-plan.md` §2.2 / §3.1 / §7 FE-1。
 *
 * 设计要点：
 *  - **单一上传入口**：全站只允许这一份默认实现，业务域不得再直连 httpClient 上传；
 *  - **零业务 API 依赖**：本模块不 import httpClient / 任何域客户端，HTTP 细节由传输层
 *    （`lib/api/attachment-api.ts`，FE-4）实现并注入，便于组件层单测与后续替换端点；
 *  - **端点解析策略**：传输层按宿主选择「通用路由 / 域内别名路由」，本模块只保证
 *    宿主上下文合法（bizType 非空、bizId 为正整数、usage 落在契约枚举内）。
 */

import {
  ATTACHMENT_USAGES,
  type AttachmentDeleter,
  type AttachmentHostContext,
  type AttachmentRef,
  type AttachmentUploader,
  type AttachmentUsage,
} from './types';

/** 上传传输层端口：由 `lib/api/attachment-api.ts` 实现（通用 A1 或域内别名路由） */
export interface AttachmentUploadTransport {
  upload(
    file: File,
    host: Required<AttachmentHostContext>,
    onProgress?: (percent: number) => void
  ): Promise<AttachmentRef>;
}

/** 删除传输层端口：由 `lib/api/attachment-api.ts` 实现（A5 通用或域内别名路由） */
export interface AttachmentDeleteTransport {
  remove(id: number): Promise<void>;
}

/** 同时提供上传与删除的传输层（应用级 Provider 注入用） */
export type AttachmentTransport = AttachmentUploadTransport & AttachmentDeleteTransport;

/** 用途枚举守卫 */
export function isAttachmentUsage(value: unknown): value is AttachmentUsage {
  return typeof value === 'string' && (ATTACHMENT_USAGES as readonly string[]).includes(value);
}

/** 宿主上下文守卫：bizType 非空字符串 + bizId 正整数 + usage 合法（缺省允许） */
export function isAttachmentHostContext(value: unknown): value is AttachmentHostContext {
  if (!value || typeof value !== 'object') return false;
  const host = value as Partial<AttachmentHostContext>;
  const bizTypeOk = typeof host.bizType === 'string' && host.bizType.trim().length > 0;
  const bizIdOk = typeof host.bizId === 'number' && Number.isInteger(host.bizId) && host.bizId > 0;
  const usageOk = host.usage === undefined || isAttachmentUsage(host.usage);
  return bizTypeOk && bizIdOk && usageOk;
}

/** 附件元数据守卫：字段与后端 `AttachmentRef` 同名同义（§3.1） */
export function isAttachmentRef(value: unknown): value is AttachmentRef {
  if (!value || typeof value !== 'object') return false;
  const ref = value as Partial<AttachmentRef>;
  return (
    typeof ref.id === 'number' &&
    Number.isInteger(ref.id) &&
    ref.id > 0 &&
    typeof ref.bizType === 'string' &&
    ref.bizType.trim().length > 0 &&
    typeof ref.bizId === 'number' &&
    Number.isInteger(ref.bizId) &&
    ref.bizId > 0 &&
    isAttachmentUsage(ref.usage) &&
    typeof ref.fileName === 'string' &&
    typeof ref.fileSize === 'number' &&
    typeof ref.mimeType === 'string'
  );
}

/** 归一化宿主上下文：补齐默认 usage，非法输入直接抛错（避免把脏 host 发给后端） */
export function normalizeAttachmentHost(host: AttachmentHostContext): Required<AttachmentHostContext> {
  if (!isAttachmentHostContext(host)) {
    throw new Error('附件宿主上下文非法：bizType 必填且非空，bizId 必须为正整数，usage 必须合法');
  }
  return {
    bizType: host.bizType.trim(),
    bizId: host.bizId,
    usage: host.usage ?? 'attachment',
  };
}

/**
 * 以传输层为依赖构造唯一上传实现：
 *  - 宿主归一化（默认 usage = 'attachment'）；
 *  - 返回值做契约守卫（后端新增字段不影响，缺必填字段立即暴露，避免脏数据流入宿主表单）。
 */
export function createAttachmentUploader(transport: AttachmentUploadTransport): AttachmentUploader {
  return async (file: File, host: AttachmentHostContext, onProgress?: (percent: number) => void) => {
    const normalizedHost = normalizeAttachmentHost(host);
    const ref = await transport.upload(file, normalizedHost, onProgress);
    if (!isAttachmentRef(ref)) {
      throw new Error('附件上传响应结构非法：缺少 id / fileName / usage 等契约字段');
    }
    return ref;
  };
}

/** 以传输层为依赖构造删除实现（A5；被引用时后端返回 409/6105，由调用方提示） */
export function createAttachmentDeleter(transport: AttachmentDeleteTransport): AttachmentDeleter {
  return async (attachment: AttachmentRef) => {
    if (!isAttachmentRef(attachment)) {
      throw new Error('附件引用非法：无法删除');
    }
    await transport.remove(attachment.id);
  };
}
