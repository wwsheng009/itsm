/**
 * 通用附件 API 客户端（FE-4）——**全站唯一**附件 HTTP 实现。
 *
 * 依据：`docs/plan/generic-attachment-richtext-control-plan.md` §2.1/§2.2/§3.1/§7 FE-4。
 *
 * 端点解析（§2.1「`http-client → /api/v1/attachments*`（或域内别名路由）」）：
 *  - `ticket` → 旧域内端点 `/api/v1/tickets/:id/attachments`：URL 形态不变（D5 永不破坏），
 *    静态权限沿用 `ticket:read/create/delete`，BE-6 薄适配按开关转发通用服务、
 *    BE-10 起支持可选 `usage` 表单字段（内嵌图片 / 评论附件）；
 *  - `knowledge_article` / `service_request` → BE-5 域内别名路由（静态权限复用宿主资源码）；
 *  - 其余宿主（无域内端点）→ 通用 A1-A6，静态权限为兜底码 `attachment:*`。
 *
 * **BE-10 修订（2026-09-23）**：通用 A1-A6 的静态码是兜底码（§4.3 仅绑定 admin/sysadmin），
 * 普通用户经通用路由必被 403；因此工单域不再按 usage 分流到通用路由，一律走域内端点，
 * 用途由后端透传到通用表。通用路由仅服务「没有域内端点」的宿主。
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
  change: (bizId) => `/api/v1/changes/${bizId}/attachments`,
  incident: (bizId) => `/api/v1/incidents/${bizId}/attachments`,
  problem: (bizId) => `/api/v1/problems/${bizId}/attachments`,
  release: (bizId) => `/api/v1/releases/${bizId}/attachments`,
  cmdb_ci: (bizId) => `/api/v1/cmdb/cis/${bizId}/attachments`,
  known_error: (bizId) => `/api/v1/known-errors/${bizId}/attachments`,
};

/** 工单域内容地址（旧端点形态，D5 永不失效；静态权限 `ticket:read`） */
export function ticketAttachmentContentUrl(ticketId: number, id: number): string {
  return `/api/v1/tickets/${ticketId}/attachments/${id}`;
}

/** 工单域预览地址（旧端点形态；服务端按 preview 语义内联，供 `<img src>` 使用） */
export function ticketAttachmentPreviewUrl(ticketId: number, id: number): string {
  return `/api/v1/tickets/${ticketId}/attachments/${id}/preview`;
}

/** 知识库域内容地址（BE-5 别名路由，静态权限 `knowledge:read`） */
export function knowledgeAttachmentContentUrl(articleId: number, id: number): string {
  return `/api/v1/knowledge/articles/${articleId}/attachments/${id}`;
}

/** 知识库域预览地址（BE-5 别名路由；服务端按 preview 语义内联，供 `<img src>` 使用） */
export function knowledgeAttachmentPreviewUrl(articleId: number, id: number): string {
  return `/api/v1/knowledge/articles/${articleId}/attachments/${id}/preview`;
}

/** 服务请求域内容地址（BE-5 别名路由，静态权限 `service_request:read`） */
export function serviceRequestAttachmentContentUrl(requestId: number, id: number): string {
  return `/api/v1/service-requests/${requestId}/attachments/${id}`;
}

/** 服务请求域预览地址（BE-5 别名路由） */
export function serviceRequestAttachmentPreviewUrl(requestId: number, id: number): string {
  return `/api/v1/service-requests/${requestId}/attachments/${id}/preview`;
}

/** 变更域内容地址（BE-5 别名路由，静态权限 `change:read`） */
export function changeAttachmentContentUrl(changeId: number, id: number): string {
  return `/api/v1/changes/${changeId}/attachments/${id}`;
}

/** 变更域预览地址（BE-5 别名路由） */
export function changeAttachmentPreviewUrl(changeId: number, id: number): string {
  return `/api/v1/changes/${changeId}/attachments/${id}/preview`;
}

/** 事件域内容地址（BE-5 别名路由，静态权限 `incident:read`） */
export function incidentAttachmentContentUrl(incidentId: number, id: number): string {
  return `/api/v1/incidents/${incidentId}/attachments/${id}`;
}

/** 事件域预览地址（BE-5 别名路由） */
export function incidentAttachmentPreviewUrl(incidentId: number, id: number): string {
  return `/api/v1/incidents/${incidentId}/attachments/${id}/preview`;
}

/** 问题域内容地址（BE-5 别名路由，静态权限 `problem:read`） */
export function problemAttachmentContentUrl(problemId: number, id: number): string {
  return `/api/v1/problems/${problemId}/attachments/${id}`;
}

/** 问题域预览地址（BE-5 别名路由） */
export function problemAttachmentPreviewUrl(problemId: number, id: number): string {
  return `/api/v1/problems/${problemId}/attachments/${id}/preview`;
}

/** 发布域内容地址（BE-5 别名路由，静态权限 `release:read`） */
export function releaseAttachmentContentUrl(releaseId: number, id: number): string {
  return `/api/v1/releases/${releaseId}/attachments/${id}`;
}

/** 发布域预览地址（BE-5 别名路由） */
export function releaseAttachmentPreviewUrl(releaseId: number, id: number): string {
  return `/api/v1/releases/${releaseId}/attachments/${id}/preview`;
}

/** CMDB CI 域内容地址（BE-5 别名路由，静态权限 `cmdb:read`） */
export function cmdbCiAttachmentContentUrl(ciId: number, id: number): string {
  return `/api/v1/cmdb/cis/${ciId}/attachments/${id}`;
}

/** CMDB CI 域预览地址（BE-5 别名路由） */
export function cmdbCiAttachmentPreviewUrl(ciId: number, id: number): string {
  return `/api/v1/cmdb/cis/${ciId}/attachments/${id}/preview`;
}

/** 已知错误域内容地址（BE-5 别名路由，静态权限 `problem:read`：KEDB 复用 problem:* 词表） */
export function knownErrorAttachmentContentUrl(knownErrorId: number, id: number): string {
  return `/api/v1/known-errors/${knownErrorId}/attachments/${id}`;
}

/** 已知错误域预览地址（BE-5 别名路由） */
export function knownErrorAttachmentPreviewUrl(knownErrorId: number, id: number): string {
  return `/api/v1/known-errors/${knownErrorId}/attachments/${id}/preview`;
}

/**
 * 域内地址构造表（与 `DOMAIN_LIST_PATHS` 同源）。
 *
 * 用途有二：①旧域内响应的兜底地址；②把域内端点透出的**通用 A4 地址**改写回域内地址——
 * 通用 A4 的静态码是兜底码 `attachment:read`（§4.3 仅 admin/sysadmin 持有），普通用户
 * 直接渲染 `<img src="/api/v1/attachments/:id/content?...">` 会 403（与 BE-10 同类的权限回归）。
 */
const DOMAIN_CONTENT_URLS: Record<string, (bizId: number, id: number) => string> = {
  ticket: ticketAttachmentContentUrl,
  knowledge_article: knowledgeAttachmentContentUrl,
  service_request: serviceRequestAttachmentContentUrl,
  change: changeAttachmentContentUrl,
  incident: incidentAttachmentContentUrl,
  problem: problemAttachmentContentUrl,
  release: releaseAttachmentContentUrl,
  cmdb_ci: cmdbCiAttachmentContentUrl,
  known_error: knownErrorAttachmentContentUrl,
};

const DOMAIN_PREVIEW_URLS: Record<string, (bizId: number, id: number) => string> = {
  ticket: ticketAttachmentPreviewUrl,
  knowledge_article: knowledgeAttachmentPreviewUrl,
  service_request: serviceRequestAttachmentPreviewUrl,
  change: changeAttachmentPreviewUrl,
  incident: incidentAttachmentPreviewUrl,
  problem: problemAttachmentPreviewUrl,
  release: releaseAttachmentPreviewUrl,
  cmdb_ci: cmdbCiAttachmentPreviewUrl,
  known_error: knownErrorAttachmentPreviewUrl,
};

/** 是否通用 A4 地址（`/api/v1/attachments/...`） */
function isGenericAttachmentUrl(url?: string): boolean {
  return typeof url === 'string' && url.startsWith(`${GENERIC_ATTACHMENTS_PATH}/`);
}

function isImageMime(mimeType?: string): boolean {
  return (mimeType ?? '').toLowerCase().startsWith('image/');
}

/**
 * 域内端点响应归一化：把通用 A4 地址改写为域内地址（内容 / 预览），其余字段原样保留。
 * 已是域内地址（旧响应）时不做任何改写，避免破坏既有 URL 形态（D5）。
 */
function withDomainUrls(ref: AttachmentRef, host: Required<AttachmentHostContext>): AttachmentRef {
  const content = DOMAIN_CONTENT_URLS[host.bizType];
  const preview = DOMAIN_PREVIEW_URLS[host.bizType];
  if (!content || !preview) return ref;
  if (!isGenericAttachmentUrl(ref.fileUrl) && !isGenericAttachmentUrl(ref.previewUrl)) return ref;
  const next: AttachmentRef = { ...ref, fileUrl: content(host.bizId, ref.id) };
  if (ref.previewUrl || isImageMime(ref.mimeType)) {
    next.previewUrl = preview(host.bizId, ref.id);
  }
  return next;
}

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
 * 解析域内端点（命中则返回该宿主的 URL 前缀，否则 null = 走通用 A1-A6）。
 *
 * 工单域不再按 `usage` 分流：域内端点自 BE-10 起接受可选 `usage` 表单字段并透传通用表，
 * 静态权限仍是 `ticket:*`；若改走通用路由，普通用户会被兜底码 `attachment:*` 挡下（403）。
 */
function resolveDomainPath(host: Required<AttachmentHostContext>): string | null {
  if (host.bizType === 'ticket') {
    return `/api/v1/tickets/${host.bizId}/attachments`;
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
  uploader?: { id: number; name?: string; username?: string };
  createdAt?: string;
}

/** 旧域内响应 → 统一契约（补齐 bizType/bizId/usage；预览地址沿用旧形态，D5） */
function toAttachmentRef(raw: LegacyTicketAttachment, host: Required<AttachmentHostContext>): AttachmentRef {
  // BE-6 写开关打开时旧端点可能直接透出通用契约字段；此时不再补字段，但仍需把
  // 通用 A4 地址改写为域内地址（否则普通用户渲染 `<img src>` 会 403）。
  if (isAttachmentRef(raw)) return withDomainUrls(raw, host);
  const buildContent = DOMAIN_CONTENT_URLS[host.bizType];
  const buildPreview = DOMAIN_PREVIEW_URLS[host.bizType];
  const fallbackContent = buildContent ? buildContent(host.bizId, raw.id) : attachmentContentUrl(raw.id);
  const fallbackPreview = buildPreview ? buildPreview(host.bizId, raw.id) : attachmentPreviewUrl(raw.id);
  return {
    id: raw.id,
    bizType: host.bizType,
    bizId: host.bizId,
    usage: host.usage,
    fileName: raw.fileName ?? '',
    fileSize: raw.fileSize ?? 0,
    mimeType: raw.mimeType ?? '',
    // D5：旧响应自带 fileUrl 时原样保留（旧 URL 形态不得变更）；缺失时用域内内容地址兜底。
    fileUrl: raw.fileUrl || fallbackContent,
    previewUrl: raw.fileUrl || fallbackPreview,
    uploadedBy: raw.uploadedBy ?? raw.uploader?.id,
    uploader: raw.uploader,
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
      // 域内端点均接受可选 `usage` 表单字段：工单旧端点由 BE-10 透传到通用表（非默认用途
      // 不受写开关约束），BE-5 别名路由本就直接调用通用服务。
      if (normalized.usage !== 'attachment') formData.append('usage', normalized.usage);
      const raw = await httpClient.post<LegacyTicketAttachment>(domainPath, formData, {
        onUploadProgress: options.onProgress,
      });
      return assertRef(withDomainUrls(toAttachmentRef(raw ?? {}, normalized), normalized), '上传');
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
      const attachments = (raw?.attachments ?? []).map((item) =>
        withDomainUrls(toAttachmentRef(item, normalized), normalized)
      );
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
