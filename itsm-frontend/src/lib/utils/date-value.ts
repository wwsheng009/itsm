import dayjs, { type Dayjs } from 'dayjs';

/**
 * API 日期字符串 → Ant Design DatePicker 需要的 Dayjs。
 *
 * DatePicker 会直接调用 `value.isValid()`；把字符串塞进表单会抛
 * `TypeError: t.isValid is not a function`（2026-10-18 里程碑编辑实测）。
 * 空值/非法值返回 undefined，避免把坏值传给组件。
 */
export function dateValue(value?: string | null): Dayjs | undefined {
  if (!value) return undefined;
  const parsed = dayjs(value);
  return parsed.isValid() ? parsed : undefined;
}

/** Dayjs → API 需要的 ISO 字符串；空值/非法值返回 undefined（JSON 序列化时省略）。 */
export function isoValue(value?: Dayjs | null): string | undefined {
  return value && value.isValid() ? value.toISOString() : undefined;
}
