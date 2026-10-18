import dayjs, { type Dayjs } from 'dayjs';
import utc from 'dayjs/plugin/utc';

dayjs.extend(utc);

/**
 * API 日期字符串 → Ant Design DatePicker 需要的 Dayjs（项目域日期按 UTC 日历日）。
 *
 * DatePicker 会直接调用 `value.isValid()`；把字符串塞进表单会抛
 * `TypeError: t.isValid is not a function`（2026-10-18 里程碑编辑实测）。
 * 项目域日期是 date-only：列表用 `slice(0,10)` 展示 UTC 日历日，因此预填也取
 * UTC 日历日，避免 `2026-11-14T16:00Z` 在 CST 下显示成 11-15。空值/非法值返回
 * undefined，避免把坏值传给组件。
 */
export function dateValue(value?: string | null): Dayjs | undefined {
  if (!value) return undefined;
  const parsed = dayjs.utc(value);
  if (!parsed.isValid()) return undefined;
  return dayjs(parsed.format('YYYY-MM-DD'));
}

/** Dayjs → 项目域 date-only API 值（UTC 00:00）；空值/非法值返回 undefined（JSON 序列化时省略）。 */
export function isoValue(value?: Dayjs | null): string | undefined {
  if (!value || !value.isValid()) return undefined;
  return `${value.format('YYYY-MM-DD')}T00:00:00.000Z`;
}
