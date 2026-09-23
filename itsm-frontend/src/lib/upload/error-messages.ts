/**
 * 附件错误码 → i18n key 单一映射（FE-7）
 *
 * 依据：`docs/plan/generic-attachment-richtext-control-plan.md` §3.4「错误码落地要求」第 3 条：
 *  - `http-client` 保持通用解包（只把后端 `code` 挂到 `Error.code`，不做任何附件特判）；
 *  - 附件文案集中在本模块映射，中英文案落在 `lib/i18n/translations.ts`；
 *  - 组件层禁止再写死中文提示（FE-7 验收）。
 *
 * 后端同值常量：`itsm-backend/common/response.go`（61xx 段 + 2003 ForbiddenCode）。
 * 本模块不依赖 httpClient / 任何业务 API，保证独立可测。
 */
import { DEFAULT_ATTACHMENT_MAX_SIZE_MB } from './types';

/** 现有兜底码（common/response.go#ForbiddenCode）：无宿主持有权限 */
export const ATTACHMENT_FORBIDDEN_CODE = 2003;

/** 附件错误码常量（与后端 common/response.go 同值） */
export const ATTACHMENT_ERROR_CODES = {
  /** 403：无宿主持有权限 / 角色缺 `attachment:*` */
  forbidden: ATTACHMENT_FORBIDDEN_CODE,
  /** 404：宿主不存在、已删除或跨租户 */
  hostNotFound: 6101,
  /** 400：文件名非法（路径穿越 / 超长 / 清洗后为空） */
  invalidFilename: 6102,
  /** 413：单文件超限 */
  tooLarge: 6103,
  /** 415：MIME / 扩展名不允许（含病毒扫描拒绝） */
  typeNotAllowed: 6104,
  /** 409：附件被宿主正文或评论引用，仍要删 */
  inUse: 6105,
  /** 422：宿主 / 评论附件配额超限 */
  quotaExceeded: 6106,
  /** 429：附件专属频率超限 */
  rateLimited: 6107,
} as const;

/** 全部需要 i18n 文案的附件错误码（测试遍历用） */
export const ATTACHMENT_ERROR_CODE_VALUES: readonly number[] = Object.values(ATTACHMENT_ERROR_CODES);

/** 附件错误文案 key（含无 code 的兜底文案） */
export const ATTACHMENT_ERROR_KEYS = {
  forbidden: 'attachment.error.forbidden',
  hostNotFound: 'attachment.error.hostNotFound',
  invalidFilename: 'attachment.error.invalidFilename',
  tooLarge: 'attachment.error.tooLarge',
  typeNotAllowed: 'attachment.error.typeNotAllowed',
  inUse: 'attachment.error.inUse',
  quotaExceeded: 'attachment.error.quotaExceeded',
  rateLimited: 'attachment.error.rateLimited',
  uploadFailed: 'attachment.error.uploadFailed',
  deleteFailed: 'attachment.error.deleteFailed',
} as const;

/** 后端错误码 → i18n key（§3.4 权威表） */
export const ATTACHMENT_ERROR_I18N_KEYS: Readonly<Record<number, string>> = {
  [ATTACHMENT_ERROR_CODES.forbidden]: ATTACHMENT_ERROR_KEYS.forbidden,
  [ATTACHMENT_ERROR_CODES.hostNotFound]: ATTACHMENT_ERROR_KEYS.hostNotFound,
  [ATTACHMENT_ERROR_CODES.invalidFilename]: ATTACHMENT_ERROR_KEYS.invalidFilename,
  [ATTACHMENT_ERROR_CODES.tooLarge]: ATTACHMENT_ERROR_KEYS.tooLarge,
  [ATTACHMENT_ERROR_CODES.typeNotAllowed]: ATTACHMENT_ERROR_KEYS.typeNotAllowed,
  [ATTACHMENT_ERROR_CODES.inUse]: ATTACHMENT_ERROR_KEYS.inUse,
  [ATTACHMENT_ERROR_CODES.quotaExceeded]: ATTACHMENT_ERROR_KEYS.quotaExceeded,
  [ATTACHMENT_ERROR_CODES.rateLimited]: ATTACHMENT_ERROR_KEYS.rateLimited,
};

/**
 * 附件字段（AttachmentField）用户可见文案 key。
 * 组件内一律 `t(KEY)` 取文案，禁止硬编码中文（FE-7 验收）。
 */
export const ATTACHMENT_FIELD_I18N_KEYS = {
  title: 'attachment.field.title',
  dropText: 'attachment.field.dropText',
  draggerAria: 'attachment.field.draggerAria',
  hintDefault: 'attachment.field.hintDefault',
  uploadingCount: 'attachment.field.uploadingCount',
  maxCountExceeded: 'attachment.field.maxCountExceeded',
  uploadFailedItem: 'attachment.field.uploadFailedItem',
  deleteFailedItem: 'attachment.field.deleteFailedItem',
  errorSummary: 'attachment.field.errorSummary',
  pendingSummary: 'attachment.field.pendingSummary',
  retry: 'attachment.field.retry',
  retryAria: 'attachment.field.retryAria',
  remove: 'attachment.field.remove',
  removeAria: 'attachment.field.removeAria',
  removeUploaded: 'attachment.field.removeUploaded',
  statusPending: 'attachment.field.status.pending',
  statusUploading: 'attachment.field.status.uploading',
  statusDone: 'attachment.field.status.done',
  statusError: 'attachment.field.status.error',
  reasonTooLarge: 'attachment.field.reason.tooLarge',
  reasonEmptyFile: 'attachment.field.reason.emptyFile',
  reasonTypeNotAllowed: 'attachment.field.reason.typeNotAllowed',
} as const;

/** 校验失败原因码（AttachmentField#validateAttachmentFile） */
export type AttachmentValidationCode = 'tooLarge' | 'emptyFile' | 'typeNotAllowed';

/** 校验失败原因码 → i18n key */
export const ATTACHMENT_REASON_I18N_KEYS: Readonly<Record<AttachmentValidationCode, string>> = {
  tooLarge: ATTACHMENT_FIELD_I18N_KEYS.reasonTooLarge,
  emptyFile: ATTACHMENT_FIELD_I18N_KEYS.reasonEmptyFile,
  typeNotAllowed: ATTACHMENT_FIELD_I18N_KEYS.reasonTypeNotAllowed,
};

/**
 * i18n 翻译函数签名（与 `lib/i18n/useI18n` 的 `t` 结构兼容）。
 * 单独声明是为了让本模块与组件层不反向依赖 hook。
 */
export type AttachmentTranslateFn = (
  key: string,
  params?: Record<string, string | number>
) => string;

/** 取 i18n 文案；取不到（返回 key 本身）时回落到调用方给的默认值 */
export function translateOrFallback(
  t: AttachmentTranslateFn | undefined,
  key: string,
  fallback: string,
  params?: Record<string, string | number>
): string {
  if (!t) return fallback;
  const text = t(key, params);
  return text && text !== key ? text : fallback;
}

const toFiniteNumber = (value: unknown): number | null => {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
};

/**
 * 从错误对象中提取后端业务码。
 *
 * 支持 `http-client` 抛出的 `Error & { code }`，以及 axios 风格
 * `error.response.data.code` / 裸 `{ code }`（便于单测与未来客户端替换）。
 */
export function extractAttachmentErrorCode(error: unknown): number | null {
  if (!error || typeof error !== 'object') return null;
  const source = error as Record<string, unknown>;

  const direct = toFiniteNumber(source.code);
  if (direct !== null) return direct;

  const response = source.response;
  if (response && typeof response === 'object') {
    const responseData = (response as { data?: unknown }).data;
    if (responseData && typeof responseData === 'object') {
      const code = toFiniteNumber((responseData as Record<string, unknown>).code);
      if (code !== null) return code;
    }
  }

  const data = source.data;
  if (data && typeof data === 'object') {
    const code = toFiniteNumber((data as Record<string, unknown>).code);
    if (code !== null) return code;
  }

  return null;
}

/** 业务码 → i18n key；未登记码返回 null（由调用方回落到后端 message） */
export function resolveAttachmentErrorI18nKey(
  code: number | null | undefined
): string | null {
  if (code === null || code === undefined) return null;
  return ATTACHMENT_ERROR_I18N_KEYS[code] ?? null;
}

/** 错误对象 → i18n key（提取失败/未登记码返回 null） */
export function attachmentErrorI18nKey(error: unknown): string | null {
  return resolveAttachmentErrorI18nKey(extractAttachmentErrorCode(error));
}

/** 需要动态限额参数的错误码（超限提示要展示服务端限额） */
const PARAM_KEYS = new Set<string>([ATTACHMENT_ERROR_KEYS.tooLarge]);

/**
 * 统一的附件错误文案格式化：
 *  1. 命中登记码 → i18n 文案（超限带 `{maxSizeMB}`）；
 *  2. 未命中 → 后端 message（含 RID，便于支持定位）；
 *  3. 都没有 → 兜底文案（默认「附件上传失败」）。
 */
export function formatAttachmentError(
  error: unknown,
  t?: AttachmentTranslateFn,
  options: { fallbackKey?: string; fallbackText?: string; maxSizeMB?: number } = {}
): string {
  const key = attachmentErrorI18nKey(error);
  if (key) {
    const params = PARAM_KEYS.has(key)
      ? { maxSizeMB: options.maxSizeMB ?? DEFAULT_ATTACHMENT_MAX_SIZE_MB }
      : undefined;
    const text = translateOrFallback(t, key, key, params);
    if (text !== key) return text;
  }

  const message =
    error && typeof error === 'object' && typeof (error as { message?: unknown }).message === 'string'
      ? ((error as { message: string }).message || '').trim()
      : '';
  if (message) return message;

  const fallbackKey = options.fallbackKey ?? ATTACHMENT_ERROR_KEYS.uploadFailed;
  return translateOrFallback(t, fallbackKey, options.fallbackText ?? fallbackKey);
}
