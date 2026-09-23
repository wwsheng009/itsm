/**
 * FE-7：附件错误码 → i18n 文案映射单测。
 *
 * 覆盖：
 *  1. 61xx + 2003 登记完整性与 zh-CN/en 双语齐备（FE-7 验收：6101-6107 全部有中英文案）；
 *  2. 错误码提取的多形态兼容（http-client 直挂 code / axios 风格 response.data.code / 字符串码）；
 *  3. 文案优先级：登记码 → 后端 message → 兜底文案；
 *  4. 组件层无硬编码中文回归（防止后续把文案写回 JSX）。
 */
import fs from 'fs';
import path from 'path';

import { translations } from '@/lib/i18n/translations';
import {
  ATTACHMENT_ERROR_CODES,
  ATTACHMENT_ERROR_CODE_VALUES,
  ATTACHMENT_ERROR_I18N_KEYS,
  ATTACHMENT_ERROR_KEYS,
  ATTACHMENT_FIELD_I18N_KEYS,
  ATTACHMENT_REASON_I18N_KEYS,
  attachmentErrorI18nKey,
  extractAttachmentErrorCode,
  formatAttachmentError,
  resolveAttachmentErrorI18nKey,
  translateOrFallback,
  type AttachmentTranslateFn,
} from '@/lib/upload/error-messages';

type Locale = 'zh-CN' | 'en-US';
type Dict = Record<string, unknown>;

const LOCALES: readonly Locale[] = ['zh-CN', 'en-US'];

/** 按 dot-path 读翻译表（与 useI18n 的解析规则一致） */
const lookup = (locale: Locale, key: string): unknown => {
  let node: unknown = (translations as unknown as Dict)[locale];
  for (const part of key.split('.')) {
    if (node && typeof node === 'object' && part in (node as Dict)) {
      node = (node as Dict)[part];
    } else {
      return undefined;
    }
  }
  return node;
};

/** 指定语言的翻译函数（占位符替换规则与 useI18n 一致） */
const makeT = (locale: Locale): AttachmentTranslateFn => (key, params) => {
  const value = lookup(locale, key);
  if (typeof value !== 'string') return key;
  let result = value;
  Object.keys(params ?? {}).forEach((paramKey) => {
    result = result.replace(new RegExp(`\\{${paramKey}\\}`, 'g'), String(params![paramKey]));
  });
  return result;
};

const zh = makeT('zh-CN');
const en = makeT('en-US');

describe('FE-7 附件错误码 → i18n key 映射', () => {
  it('61xx 段与 2003 兜底码全部登记', () => {
    expect([...ATTACHMENT_ERROR_CODE_VALUES].sort((a, b) => a - b)).toEqual([
      2003, 6101, 6102, 6103, 6104, 6105, 6106, 6107,
    ]);
    Object.values(ATTACHMENT_ERROR_CODES).forEach((code) => {
      expect(typeof ATTACHMENT_ERROR_I18N_KEYS[code]).toBe('string');
      expect(ATTACHMENT_ERROR_I18N_KEYS[code].startsWith('attachment.error.')).toBe(true);
    });
  });

  it('每个错误码在 zh-CN / en 都有非空文案（且两种语言不同）', () => {
    ATTACHMENT_ERROR_CODE_VALUES.forEach((code) => {
      const zhText = zh(resolveAttachmentErrorI18nKey(code)!);
      const enText = en(resolveAttachmentErrorI18nKey(code)!);
      expect(zhText.length).toBeGreaterThan(0);
      expect(enText.length).toBeGreaterThan(0);
      expect(zhText).not.toBe(enText); // 两种语言文案确实不同
    });
  });

  it('未登记码返回 null（不误伤其它业务码）', () => {
    expect(resolveAttachmentErrorI18nKey(0)).toBeNull();
    expect(resolveAttachmentErrorI18nKey(1001)).toBeNull();
    expect(resolveAttachmentErrorI18nKey(null)).toBeNull();
    expect(resolveAttachmentErrorI18nKey(undefined)).toBeNull();
  });

  it('AttachmentField 全部字段文案 key 在两种语言下齐备', () => {
    const keys = [
      ...Object.values(ATTACHMENT_FIELD_I18N_KEYS),
      ...Object.values(ATTACHMENT_REASON_I18N_KEYS),
      ...Object.values(ATTACHMENT_ERROR_KEYS),
    ];
    expect(keys.length).toBeGreaterThan(30);
    LOCALES.forEach((locale) => {
      keys.forEach((key) => {
        const value = lookup(locale, key);
        expect(typeof value).toBe('string');
        expect(value as string).not.toHaveLength(0);
        // key 必须真实命中翻译表（未命中时 useI18n 会回显 key 本身）
        expect(value).not.toBe(key);
      });
    });
  });

  it('超限文案带 {maxSizeMB} 且中英均可替换为服务端限额', () => {
    const event = { code: ATTACHMENT_ERROR_CODES.tooLarge };
    expect(zh(formatAttachmentError(event, zh, { maxSizeMB: 25 }))).toContain('25MB');
    expect(en(formatAttachmentError(event, en, { maxSizeMB: 25 }))).toContain('25MB');
  });
});

describe('FE-7 错误码提取', () => {
  it('优先读取 http-client 直挂的 code（含 httpStatus / requestId）', () => {
    const error = Object.assign(new Error('文件超过 10MB 上限 [RID: abc]'), {
      code: 6103,
      httpStatus: 413,
      requestId: 'abc',
    });
    expect(extractAttachmentErrorCode(error)).toBe(6103);
    expect(attachmentErrorI18nKey(error)).toBe(ATTACHMENT_ERROR_I18N_KEYS[6103]);
  });

  it('兼容 axios 风格 response.data.code 与字符串码', () => {
    expect(extractAttachmentErrorCode({ response: { data: { code: 6106 } } })).toBe(6106);
    expect(extractAttachmentErrorCode({ message: 'x', code: '6107' })).toBe(6107);
    expect(extractAttachmentErrorCode({ data: { code: 2003 } })).toBe(2003);
  });

  it('无码错误返回 null，不抛异常', () => {
    expect(extractAttachmentErrorCode(new Error('boom'))).toBeNull();
    expect(extractAttachmentErrorCode('boom')).toBeNull();
    expect(extractAttachmentErrorCode(undefined)).toBeNull();
    expect(extractAttachmentErrorCode({ code: 'not-a-number' })).toBeNull();
  });
});

describe('FE-7 文案格式化', () => {
  it('登记码 → i18n 文案（覆盖所有 61xx/2003）', () => {
    ATTACHMENT_ERROR_CODE_VALUES.forEach((code) => {
      const text = formatAttachmentError({ code }, zh, { maxSizeMB: 10 });
      expect(text).toBe(zh(ATTACHMENT_ERROR_I18N_KEYS[code], { maxSizeMB: 10 }));
      expect(text).not.toBe(ATTACHMENT_ERROR_I18N_KEYS[code]);
    });
  });

  it('未登记码 → 后端 message 原样透出（保留 RID 便于排查）', () => {
    const error = Object.assign(new Error('宿主不存在或无权访问 [RID: rid-1]'), { code: 2004 });
    expect(formatAttachmentError(error, zh)).toBe('宿主不存在或无权访问 [RID: rid-1]');
  });

  it('无 message → 兜底文案（默认上传失败，可指定删除失败）', () => {
    expect(formatAttachmentError({ code: 9999 }, zh)).toBe(zh(ATTACHMENT_ERROR_KEYS.uploadFailed));
    expect(formatAttachmentError({}, zh, { fallbackKey: ATTACHMENT_ERROR_KEYS.deleteFailed })).toBe(
      zh(ATTACHMENT_ERROR_KEYS.deleteFailed)
    );
    // fallbackText 只在兜底 key 也取不到译文时生效（无 i18n 上下文 / 文案缺失）
    expect(formatAttachmentError({}, undefined, { fallbackText: '自定义兜底' })).toBe('自定义兜底');
  });

  it('无 i18n 上下文（纯函数路径）不抛异常且回落到 message / 兜底文本', () => {
    expect(formatAttachmentError({ code: 6105, message: '附件已被引用，无法删除' })).toBe(
      '附件已被引用，无法删除'
    );
    expect(formatAttachmentError({ code: 6105 }, undefined, { fallbackText: '上传失败' })).toBe('上传失败');
  });

  it('translateOrFallback：命中返回译文，未命中/无 t 返回兜底', () => {
    expect(translateOrFallback(zh, ATTACHMENT_ERROR_KEYS.inUse, 'fallback')).toBe(
      zh(ATTACHMENT_ERROR_KEYS.inUse)
    );
    expect(translateOrFallback(zh, 'attachment.error.notRegistered', 'fallback')).toBe('fallback');
    expect(translateOrFallback(undefined, ATTACHMENT_ERROR_KEYS.inUse, 'fallback')).toBe('fallback');
  });
});

describe('FE-7 组件层无硬编码中文回归', () => {
  const componentPath = path.join(
    process.cwd(),
    'src/components/common/attachment/AttachmentField.tsx'
  );
  const source = fs.readFileSync(componentPath, 'utf8');

  it('已替换的历史文案不再出现在组件源码中（只允许存在于 translations.ts）', () => {
    const legacyLiterals = [
      '点击或拖拽文件到此处',
      '正在上传 ',
      '最多上传 ',
      '重试上传 ',
      '移除附件 ',
      '删除（同时解绑后端附件）',
      '个附件将在工单创建成功后自动上传',
      '提交工单不会因此中断',
      '选择或拖拽附件上传',
      '单文件不超过 ',
    ];
    legacyLiterals.forEach((literal) => {
      expect(source).not.toContain(literal);
    });
  });

  it('组件通过 i18n key 常量取文案（FIELD_KEYS / REASON_KEYS / formatAttachmentError）', () => {
    expect(source).toContain('useI18n');
    expect(source).toContain('ATTACHMENT_FIELD_I18N_KEYS as FIELD_KEYS');
    expect(source).toContain('ATTACHMENT_REASON_I18N_KEYS as REASON_KEYS');
    expect(source).toContain('formatAttachmentError');
  });
});
