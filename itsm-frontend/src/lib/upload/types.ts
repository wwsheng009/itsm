/**
 * 通用附件公共类型（唯一契约来源）
 *
 * 依据：`docs/plan/generic-attachment-richtext-control-plan.md` §3.1「前端公共契约（TypeScript）」。
 * 约束（§2.2 分层）：
 *  - 业务域不得私自定义副本，一律从本模块导入；
 *  - 本模块只放类型与常量，不依赖 httpClient / 任何业务 API，保证组件层可独立测试；
 *  - 字段名与后端 `handlers/attachment/handler.go#AttachmentRef` 同名同义。
 */

/** 附件用途：普通附件 / 正文内嵌图片 / 评论附件 */
export type AttachmentUsage = 'attachment' | 'inline_image' | 'comment_attachment';

/** 附件宿主上下文：bizType 对应后端 `attachments.biz_type`，bizId 为宿主单据 ID */
export interface AttachmentHostContext {
  /** 'ticket' | 'knowledge_article' | 'service_request' | ... */
  bizType: string;
  /** 宿主单据 ID */
  bizId: number;
  /** 缺省为 'attachment' */
  usage?: AttachmentUsage;
}

/** 附件元数据（A1-A6 统一响应体） */
export interface AttachmentRef {
  id: number;
  bizType: string;
  bizId: number;
  usage: AttachmentUsage;
  fileName: string;
  fileSize: number;
  mimeType: string;
  /** 下载 / 展示地址（后端给出，通常是站内代理地址） */
  fileUrl?: string;
  /** inline 预览地址（图片用；等于 fileUrl 的 inline 形态） */
  previewUrl?: string;
  sha256?: string;
  uploadedBy?: number;
  /**
   * 上传人展示信息（可选）：通用响应只给 `uploadedBy`（ID），旧工单域内响应另带昵称，
   * 映射时保留以免列表展示退化（FE-3）。
   */
  uploader?: { id: number; name?: string; username?: string };
  createdAt?: string;
}

/**
 * 唯一上传入口类型；所有组件 / 域共用（§2.2「单一上传入口」）。
 * 实现只允许一份：`lib/upload/attachment-uploader.ts` 的工厂产物。
 */
export type AttachmentUploader = (
  file: File,
  host: AttachmentHostContext,
  onProgress?: (percent: number) => void
) => Promise<AttachmentRef>;

/** 删除入口：即时模式下解绑已上传附件 */
export type AttachmentDeleter = (attachment: AttachmentRef) => Promise<void>;

/**
 * 单文件大小上限（MB）。v1.0 决策：前后端统一 10MB，禁止「前端允许、后端 413」
 * （§3.4 限额决策）。前端组件默认值必须引用本常量，不得散落魔法数字。
 */
export const DEFAULT_ATTACHMENT_MAX_SIZE_MB = 10;

/** 单宿主附件数量上限（后端默认 100，前端表单壳层默认 10） */
export const DEFAULT_ATTACHMENT_MAX_COUNT = 10;

/** 富文本单次粘贴 / 拖拽图片数量上限（编辑器默认 9） */
export const DEFAULT_RICHTEXT_IMAGE_MAX_COUNT = 9;

/** 全部合法用途（用于类型守卫与测试遍历） */
export const ATTACHMENT_USAGES: readonly AttachmentUsage[] = [
  'attachment',
  'inline_image',
  'comment_attachment',
];
