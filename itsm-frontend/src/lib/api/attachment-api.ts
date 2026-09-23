/**
 * 通用附件 API 客户端（FE-4）——**全站唯一**附件 HTTP 实现。
 *
 * 依据：`docs/plan/generic-attachment-richtext-control-plan.md` §2.1/§2.2/§3.1/§7 FE-4。
 *
 * 端点解析（§2.1「`http-client → /api/v1/attachments*`（或域内别名路由）」）：
 *  - `ticket` + 默认用途 → 旧域内端点 `/api/v1/tickets/:id/attachments`：URL 形态不变（D5 永不破坏），
 *    静态权限沿用 `ticket:read/create/delete`，BE-6 薄适配在写开关打开时转发通用服务；
 *  - `knowledge_article` / `service_request` → BE-5 域内别名路由（静态权限复用宿主资源码）；
 *  - 其余宿主、以及需要在工单域表达 `usage`（`comment_attachment` / `inline_image`）时 → 通用 A1-A6。
 *
 * 契约护栏：上传前经 `normalizeAttachmentHost` 归一化，响应经 `isAttachmentRef` 守卫；旧域内响应
 * （`TicketAttachment`：无 bizType/bizId/usage）在 `toAttachmentRef` 内补齐，调用方只看到统一契约。
 */

import { httpClient } from './http-client';
import {
  createAttachmentUploader,
  isAttachmentRef,
  normalizeAttachmentHost,
  type AttachmentTransport,
} from '@/lib/upload/attachment-uploader';
import type {
  AttachmentDeleter,
  AttachmentHostContext,
  AttachmentRef,
  AttachmentUploader,
  AttachmentUsage,
} from '@/lib/upload/types';

/** 通用附件根路径（A1/A2/A6；A3/A4/A5 为其子路径） */
export const GENERIC_ATTACHMENTS_PATH = '/api/v1/attachments';

/** 域内别名路由（BE-5）：静态权限沿用宿主资源码，普通用户不会被 attachment:* 兜底码拦住 */
const DOMAIN_LIST_PATHS: Record<string, (bizId: number) => string> = {
  knowledge_article: (bizId) => `/api/v1/knowledge/articles/${bizId}/attachments`,
  service_request: (bizId) => `/api/v1/service-requests/${bizId}/attachments`,
};

export interface AttachmentUploadOptions {
  /** 上传进度（0-100） */
  onProgress?: (percent: number) => void;
  /** A1 幂等令牌：同一 token 重复提交由后端回放同一条记录 */
  clientToken?: string;
}

export interface AttachmentListParams {
  bizType: string;
  bizId: number;
  usage?: AttachmentUsage;
  page?: number;
  pageSize?: number;
}

export interface AttachmentListResult {
  attachments: AttachmentRef[];
  total: number;
}

/** A4 内容地址（下载语义：`Content-Disposition: attachment`，支持 Range） */
export function attachmentContentUrl(id: number): string {
  return `${GENERIC_ATTACHMENTS_PATH}/${id}/content`;
}

/** A4 内容地址（预览语义：`?disposition=inline`；后端仅对安全位图内联，SVG 等仍强制下载） */
export function attachmentPreviewUrl(id: number): string {
  return `${GENERIC_ATTACHMENTS_PATH}/${id}/content?disposition=inline`;
}

/**
 * 解析域内端点（命中则返回该宿主的旧 URL 前缀，否则 null = 走通用 A1）。
 * 工单域只有默认用途能在旧端点上表达：`usage != attachment` 时必须走通用端点，
 * 否则会静默丢失用途语义（旧 handler 固定写入 `usage=attachment`）。
 */
function resolveDomainPath(host: Required<AttachmentHostContext>): string | null {
  if (host.bizType === 'ticket') {
    return host.usage === 'attachment' ? `/api/v1/tickets/${host.bizId}/attachments` : null;
  }
  const factory = DOMAIN_LIST_PATHS[host.bizType];
  return factory ? factory(host.bizId) : null;
}

/** 旧工单附件响应（域内 / 通用薄适配后的字段并集），只为补齐契约字段而存在 */
interface LegacyTicketAttachment {
  id: number;
  ticketId?: number;
  fileName?: string;
  fileUrl?: string;
  fileSize?: number;
  mimeType?: string;
  uploadedBy?: number;
  createdAt?: string;
}

/** 旧域内响应 → 统一契约（补齐 bizType/bizId/usage；预览地址沿用旧形态，D5） */
function toAttachmentRef(raw: LegacyTicketAttachment, host: Required<AttachmentHostContext>): AttachmentRef {
  // BE-6 写开关打开时旧端点可能直接透出通用契约字段；此时原样采信，避免二次加工。
  if (isAttachmentRef(raw)) return raw;
  const legacyPreview = `/api/v1/tickets/${host.bizId}/attachments/${raw.id}/preview`;
  return {
    id: raw.id,
    bizType: host.bizType,
    bizId: host.bizId,
    usage: host.usage,
    fileName: raw.fileName ?? '',
    fileSize: raw.fileSize ?? 0,
    mimeType: raw.mimeType ?? '',
    fileUrl: raw.fileUrl || legacyPreview,
    previewUrl: raw.fileUrl || legacyPreview,
    uploadedBy: raw.uploadedBy,
    createdAt: raw.createdAt,
  };
}

function assertRef(value: unknown, scene: string): AttachmentRef {
  if (!isAttachmentRef(value)) {
    throw new Error(`附件${scene}响应结构非法：缺少 id / fileName / usage 等契约字段`);
  }
  return value;
}

export class AttachmentApi {
  /**
   * A1 上传（唯一上传实现）。宿主上下文决定端点；返回值保证满足 `AttachmentRef` 契约。
   */
  static async upload(
    file: File,
    host: AttachmentHostContext,
    options: AttachmentUploadOptions = {}
  ): Promise<AttachmentRef> {
    const normalized = normalizeAttachmentHost(host);
    const formData = new FormData();
    formData.append('file', file);

    const domainPath = resolveDomainPath(normalized);
    if (domainPath) {
      // 域内别名（knowledge_article / service_request）复用 A1 主体，支持 usage 表单字段；
      // 旧工单端点固定写入 usage=attachment（service/ticket_attachment_service.go），因此
      // 工单 + 非默认用途已在 resolveDomainPath 改走通用端点，这里只需透传显式用途。
      if (normalized.usage !== 'attachment') formData.append('usage', normalized.usage);
      const raw = await httpClient.post<LegacyTicketAttachment>(domainPath, formData, {
        onUploadProgress: options.onProgress,
      });
      return assertRef(toAttachmentRef(raw ?? {}, normalized), '上传');
    }

    formData.append('bizType', normalized.bizType);
    formData.append('bizId', String(normalized.bizId));
    formData.append('usage', normalized.usage);
    if (options.clientToken) formData.append('clientToken', options.clientToken);

    const raw = await httpClient.post<AttachmentRef>(GENERIC_ATTACHMENTS_PATH, formData, {
      onUploadProgress: options.onProgress,
    });
    return assertRef(raw, '上传');
  }

  /** A2 列表（域内端点优先，保证宿主权限与既有过滤口径不变） */
  static async list(params: AttachmentListParams): Promise<AttachmentListResult> {
    const normalized = normalizeAttachmentHost({
      bizType: params.bizType,
      bizId: params.bizId,
      usage: params.usage,
    });

    const domainPath = resolveDomainPath(normalized);
    if (domainPath) {
      const raw = await httpClient.get<{ attachments?: LegacyTicketAttachment[]; total?: number }>(domainPath);
      const attachments = (raw?.attachments ?? []).map((item) => toAttachmentRef(item, normalized));
      return { attachments, total: raw?.total ?? attachments.length };
    }

    const raw = await httpClient.get<{ attachments?: unknown[]; total?: number }>(GENERIC_ATTACHMENTS_PATH, {
      bizType: normalized.bizType,
      bizId: normalized.bizId,
      usage: params.usage,
      page: params.page,
      pageSize: params.pageSize,
    });
    const attachments = (raw?.attachments ?? []).map((item) => assertRef(item, '列表'));
    return { attachments, total: raw?.total ?? attachments.length };
  }

  /** A3 明细 */
  static async get(id: number): Promise<AttachmentRef> {
    return assertRef(await httpClient.get<AttachmentRef>(`${GENERIC_ATTACHMENTS_PATH}/${id}`), '明细');
  }

  /** A6 批量元数据回填（富文本只存了 id 时用于回显） */
  static async batchQuery(ids: readonly number[]): Promise<AttachmentRef[]> {
    if (ids.length === 0) return [];
    const raw = await httpClient.post<{ attachments?: unknown[] } | unknown[]>(
      `${GENERIC_ATTACHMENTS_PATH}/batch-query`,
      { ids }
    );
    const list = Array.isArray(raw) ? raw : (raw?.attachments ?? []);
    return list.map((item) => assertRef(item, '批量查询'));
  }

  /**
   * A5 软删（按引用解析端点）：被宿主正文 / 评论引用时后端返回 409/6105，由调用方提示。
   * 已软删或跨宿主的引用按不存在处理（后端 404/6101），保持幂等语义。
   */
  static async remove(attachment: AttachmentRef): Promise<void> {
    if (!isAttachmentRef(attachment)) {
      throw new Error('附件引用非法：无法删除');
    }
    const normalized = normalizeAttachmentHost({
      bizType: attachment.bizType,
      bizId: attachment.bizId,
      usage: attachment.usage,
    });
    await AttachmentApi.removeById(attachment.id, normalized);
  }

  /** A5 变体：只有附件 ID + 宿主上下文时使用（域内端点优先） */
  static async removeById(id: number, host: AttachmentHostContext): Promise<void> {
    const normalized = normalizeAttachmentHost(host);
    const domainPath = resolveDomainPath(normalized);
    if (domainPath) {
      await httpClient.delete(`${domainPath}/${id}`);
      return;
    }
    await httpClient.delete(`${GENERIC_ATTACHMENTS_PATH}/${id}`);
  }

  /** 下载地址（通用 A4；无需携带宿主） */
  static getContentUrl(id: number): string {
    return attachmentContentUrl(id);
  }

  /** 预览地址（通用 A4 inline） */
  static getPreviewUrl(id: number): string {
    return attachmentPreviewUrl(id);
  }

  /**
   * 构造契约上传实现（`AttachmentUploader`），供 `AttachmentProvider` / 组件注入。
   * 全站只允许这一处把 HTTP 细节接到 FE-1 的工厂上。
   */
  static uploader(): AttachmentUploader {
    return createAttachmentUploader({
      upload: (file, host, onProgress) => AttachmentApi.upload(file, host, { onProgress }),
    });
  }

  /** 构造契约删除实现（`AttachmentDeleter`）：按引用自带宿主选择端点 */
  static deleter(): AttachmentDeleter {
    return (attachment) => AttachmentApi.remove(attachment);
  }

  /**
   * 传输层端口（FE-6 Provider 注入用）。
   *
   * 注意：FE-1 的删除端口契约 `AttachmentDeleteTransport.remove(id)` 不带宿主上下文，
   * 因此这里只能走通用 A5（静态权限 `attachment:delete`）。需要沿用宿主资源权限码
   * （如工单 `ticket:delete`）的调用点，直接用 `remove(ref)` / `removeById(id, host)`。
   */
  static transport(): AttachmentTransport {
    return {
      upload: (file, host, onProgress) => AttachmentApi.upload(file, host, { onProgress }),
      remove: async (id) => {
        await httpClient.delete(`${GENERIC_ATTACHMENTS_PATH}/${id}`);
      },
    };
  }
}
