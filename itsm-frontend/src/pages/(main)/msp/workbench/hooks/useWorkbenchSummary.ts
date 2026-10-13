/**
 * useWorkbenchSummary：跨租户工作台计数摘要（与顶栏客户徽标同源）。
 *
 * 数据源：GET /api/v1/msp/workbench/summary（服务端 30s 缓存；
 * open = 未关闭 = status ∉ resolved/closed/cancelled）。
 * 用 react-query 统一缓存键，供 CustomerFilter 之外的消费方（工作台「当前范围摘要」）
 * 复用同一份数据，避免各处文案/口径漂移。
 */
import { useQuery } from '@tanstack/react-query';
import { getWorkbenchSummary, type WorkbenchSummary } from '@/lib/api/msp-workbench-api';

export const WORKBENCH_SUMMARY_QUERY_KEY = ['msp', 'workbench', 'summary'] as const;

export function useWorkbenchSummary(options?: { enabled?: boolean }) {
  return useQuery<WorkbenchSummary>({
    queryKey: WORKBENCH_SUMMARY_QUERY_KEY,
    queryFn: () => getWorkbenchSummary(),
    staleTime: 30_000,
    enabled: options?.enabled ?? true,
  });
}
