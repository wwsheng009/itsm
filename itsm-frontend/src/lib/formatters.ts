import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';

dayjs.extend(utc);

export const formatDateTime = (iso?: string | null) => {
  if (!iso) return '';
  const d = dayjs(iso);
  if (!d.isValid() || d.year() <= 1) return '';
  // 统一使用 YYYY-MM-DD HH:mm，避免 toLocaleString 输出 "2026/8/15 14:30:00" 这类不一致格式
  return d.format('YYYY-MM-DD HH:mm');
};

/**
 * A6：业务时间戳展示（本地时区）。
 *
 * 后端存储/传输统一为 UTC（RFC3339），直接截取字符串会把 2026-10-21 07:34(+08)
 * 显示成 2026-10-20 23:34（跨天偏移）。这里统一走 dayjs 本地解析再格式化，
 * 保证审批时间、风险更新时间、报告生成时间与用户所在时区一致。
 */
export const formatBusinessDateTime = (iso?: string | null) => {
  if (!iso) return '';
  const d = dayjs(iso);
  if (!d.isValid() || d.year() <= 1) return '';
  return d.format('YYYY-MM-DD HH:mm:ss');
};

/** A6：业务日期展示（本地时区，YYYY-MM-DD）。 */
export const formatBusinessDate = (iso?: string | null) => {
  if (!iso) return '';
  const d = dayjs(iso);
  if (!d.isValid() || d.year() <= 1) return '';
  return d.format('YYYY-MM-DD');
};

export const mapLabel = (map: Record<string, string>, value?: string) => {
  if (!value) return '';
  return map[value] || value;
};
