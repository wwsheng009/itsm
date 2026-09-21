'use client';

/**
 * DynamicFieldRenderer
 *
 * 工单自定义字段的「类型 -> 控件」映射渲染器。
 *
 * 背景：`tickets/create/page.tsx` 原先在表单内联 `field.type === 'textarea' ? ... : ...`
 * 的分支，导致：
 *   - 新增字段类型时需要在 JSX 中继续堆叠三元表达式；
 *   - 富文本(textarea)类型无法统一接入 RichTextEditor / 附件粘贴上传能力。
 *
 * 该组件把类型分支收敛到一处，并保证所有控件共享同一套
 * disabled / placeholder / 校验提示 / 选项解析行为。
 *
 * 设计约束（见 docs/architecture/ticket-create-page-rich-input-optimization.md §4.6）：
 *   - 不直接依赖后端领域类型，使用结构化入参，避免与 API 层类型耦合；
 *   - 富文本走 RichTextEditor（含粘贴/拖拽图片上传）；
 *   - 单行/多行文本、数字、日期、下拉、多选、布尔、附件等均在此收敛。
 */

import React from 'react';
import {
  Input,
  InputNumber,
  Select,
  DatePicker,
  Switch,
  Radio,
  Checkbox,
} from 'antd';
import type { Dayjs } from 'dayjs';
import RichTextEditor from './RichTextEditor';

const { TextArea } = Input;

/** 结构化字段定义（与后端 CustomFieldDefinition 兼容的超集）。 */
export interface DynamicFieldDef {
  id: number;
  /** schema 存储 key（snake_case），提交时透传，不做驼峰转换。 */
  key?: string;
  name: string;
  type: string;
  required?: boolean;
  placeholder?: string;
  description?: string;
  defaultValue?: unknown;
  /** 选项：可能是字符串数组，也可能是 { label, value } 数组。 */
  options?: unknown;
  /** 数字类型的范围约束。 */
  min?: number;
  max?: number;
  /** 文本类型的最大长度。 */
  maxLength?: number;
}

/** 规范化后的下拉选项。 */
export interface NormalizedOption {
  label: string;
  value: string | number | boolean;
}

export interface DynamicFieldRendererProps {
  field: DynamicFieldDef;
  value: unknown;
  onChange: (value: unknown) => void;
  disabled?: boolean;
  /** 富文本编辑器粘贴/拖拽图片时的上传器；缺省时禁用图片上传。 */
  uploadImage?: (file: File) => Promise<{ url: string; attachmentId?: number }>;
  /** 富文本编辑器最小高度（px）。 */
  editorMinHeight?: number;
}

/**
 * 把后端返回的 options 统一成 { label, value }。
 * 后端可能返回：['a','b'] / [{label,value}] / [{name,id}] / JSON 字符串。
 */
export function normalizeOptions(raw: unknown): NormalizedOption[] {
  if (raw == null) return [];

  let list: unknown = raw;
  if (typeof raw === 'string') {
    const trimmed = raw.trim();
    if (!trimmed) return [];
    if (trimmed.startsWith('[') || trimmed.startsWith('{')) {
      try {
        list = JSON.parse(trimmed);
      } catch {
        // 退化为逗号分隔
        return trimmed
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean)
          .map((s) => ({ label: s, value: s }));
      }
    } else {
      return trimmed
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
        .map((s) => ({ label: s, value: s }));
    }
  }

  if (!Array.isArray(list)) return [];

  return list
    .map((item) => {
      if (item == null) return null;
      if (typeof item === 'string' || typeof item === 'number') {
        return { label: String(item), value: item as string | number };
      }
      if (typeof item === 'object') {
        const obj = item as Record<string, unknown>;
        const value = (obj.value ?? obj.id ?? obj.key ?? obj.code) as
          | string
          | number
          | boolean
          | undefined;
        const label = (obj.label ?? obj.name ?? obj.title ?? value) as string | undefined;
        if (value === undefined || value === null) return null;
        return { label: String(label ?? value), value };
      }
      return null;
    })
    .filter((v): v is NormalizedOption => v !== null);
}

/** 按类型判断是否应使用富文本编辑器。 */
export function isRichTextFieldType(type: string): boolean {
  const t = (type || '').toLowerCase();
  return t === 'textarea' || t === 'richtext' || t === 'rich_text' || t === 'html';
}

/** 按类型判断是否是多选控件。 */
export function isMultiValueFieldType(type: string): boolean {
  const t = (type || '').toLowerCase();
  return t === 'multiselect' || t === 'multi_select' || t === 'multi-select' || t === 'checkbox';
}

const DynamicFieldRenderer: React.FC<DynamicFieldRendererProps> = ({
  field,
  value,
  onChange,
  disabled = false,
  uploadImage,
  editorMinHeight,
}) => {
  const type = (field.type || 'text').toLowerCase();
  const placeholder = field.placeholder || `请输入${field.name}`;
  const options = React.useMemo(() => normalizeOptions(field.options), [field.options]);

  switch (type) {
    // ---------- 富文本（含粘贴/拖拽图片上传） ----------
    case 'textarea':
    case 'richtext':
    case 'rich_text':
    case 'html':
      return (
        <RichTextEditor
          value={typeof value === 'string' ? value : ''}
          onChange={(html) => onChange(html)}
          placeholder={placeholder}
          disabled={disabled}
          uploadImage={uploadImage}
          minHeight={editorMinHeight}
        />
      );

    // ---------- 多行纯文本 ----------
    case 'text_multiline':
    case 'longtext':
      return (
        <TextArea
          value={typeof value === 'string' ? value : ''}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          disabled={disabled}
          maxLength={field.maxLength}
          showCount={Boolean(field.maxLength)}
          autoSize={{ minRows: 3, maxRows: 8 }}
        />
      );

    // ---------- 数字 ----------
    case 'number':
    case 'integer':
    case 'float':
    case 'decimal':
      return (
        <InputNumber
          value={typeof value === 'number' ? value : undefined}
          onChange={(v) => onChange(v)}
          placeholder={placeholder}
          disabled={disabled}
          style={{ width: '100%' }}
          min={field.min}
          max={field.max}
        />
      );

    // ---------- 日期 / 时间 ----------
    case 'date':
    case 'datetime':
    case 'datetime-local':
      return (
        <DatePicker
          value={(value as Dayjs) ?? null}
          onChange={(d) => onChange(d ?? null)}
          placeholder={placeholder}
          disabled={disabled}
          style={{ width: '100%' }}
          showTime={type !== 'date'}
        />
      );

    // ---------- 布尔 ----------
    case 'boolean':
    case 'bool':
    case 'switch':
      return (
        <Switch
          checked={Boolean(value)}
          onChange={(checked) => onChange(checked)}
          disabled={disabled}
        />
      );

    // ---------- 单选 ----------
    case 'select':
    case 'enum':
    case 'radio':
      return (
        <Select
          value={(value as string | number) ?? undefined}
          onChange={(v) => onChange(v)}
          placeholder={placeholder}
          disabled={disabled}
          style={{ width: '100%' }}
          allowClear
          showSearch
          optionFilterProp="label"
          options={options}
        />
      );

    // ---------- 多选 ----------
    case 'multiselect':
    case 'multi_select':
    case 'multi-select':
    case 'checkbox':
      return (
        <Select
          mode="multiple"
          value={Array.isArray(value) ? (value as (string | number)[]) : []}
          onChange={(v) => onChange(v)}
          placeholder={placeholder}
          disabled={disabled}
          style={{ width: '100%' }}
          allowClear
          optionFilterProp="label"
          options={options}
        />
      );

    // ---------- 默认识别为单行文本 ----------
    case 'text':
    case 'string':
    case 'input':
    default:
      return (
        <Input
          value={typeof value === 'string' ? value : ''}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          disabled={disabled}
          maxLength={field.maxLength}
        />
      );
  }
};

export default DynamicFieldRenderer;
