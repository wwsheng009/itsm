/**
 * 通知深链目标探测：删除单据通知的前端兜底。
 *
 * 背景：通知 action_url（/tickets/N、/changes/N、/service-requests/N、
 * /msp/workbench?ticket=N&customerTenantId=M）指向的单据可能已被删除。
 * 后端已在删除路径联动清理通知，并提供存量孤儿巡检；本工具在跳转前做一次
 * 轻量探测，命中 404 时提示"单据已删除"，避免用户落入空白/报错详情页。
 *
 * 探测语义：
 * - 'ok'      目标存在（或无法判定但请求成功）→ 正常跳转；
 * - 'missing' 目标明确不存在（HTTP 404）→ 阻止跳转并提示；
 * - 'unknown' 非单据深链 / 无权限（403 等）/ 网络异常 → 按原行为跳转，由目标页处理。
 */
import { httpClient, type HttpClientError } from '@/lib/api/http-client';

export type NotificationTargetKind = 'ticket' | 'change' | 'service_request' | 'msp_ticket';

export interface NotificationTarget {
  kind: NotificationTargetKind;
  id: number;
  /** 仅 MSP 工作台深链携带。 */
  customerTenantId?: number;
}

export type NotificationTargetStatus = 'ok' | 'missing' | 'unknown';

const TICKET_PATH_RE = /^\/tickets\/(\d+)(?:[/?].*)?$/;
const CHANGE_PATH_RE = /^\/changes\/(\d+)(?:[/?].*)?$/;
const SR_PATH_RE = /^\/service-requests\/(\d+)(?:[/?].*)?$/;
const MSP_TICKET_PARAM_RE = /[?&]ticket=(\d+)(?:&|$)/;
const CUSTOMER_TENANT_PARAM_RE = /[?&]customerTenantId=(\d+)(?:&|$)/;

/** 解析 action_url 指向的单据；无法识别时返回 null（按普通站内链接处理）。 */
export function parseNotificationTarget(actionUrl: string): NotificationTarget | null {
  if (!actionUrl || !actionUrl.startsWith('/') || actionUrl.startsWith('//')) return null;

  const ticket = TICKET_PATH_RE.exec(actionUrl);
  if (ticket) return { kind: 'ticket', id: Number(ticket[1]) };

  const change = CHANGE_PATH_RE.exec(actionUrl);
  if (change) return { kind: 'change', id: Number(change[1]) };

  const sr = SR_PATH_RE.exec(actionUrl);
  if (sr) return { kind: 'service_request', id: Number(sr[1]) };

  // 仅限 MSP 工作台路径，避免把任意页面的 ticket= 查询参数误判为单据深链。
  if (actionUrl.startsWith('/msp/workbench')) {
    const msp = MSP_TICKET_PARAM_RE.exec(actionUrl);
    if (msp) {
      const tenant = CUSTOMER_TENANT_PARAM_RE.exec(actionUrl);
      return {
        kind: 'msp_ticket',
        id: Number(msp[1]),
        customerTenantId: tenant ? Number(tenant[1]) : undefined,
      };
    }
  }

  return null;
}

function detailPath(target: NotificationTarget): string | null {
  switch (target.kind) {
    case 'ticket':
      return `/api/v1/tickets/${target.id}`;
    case 'change':
      return `/api/v1/changes/${target.id}`;
    case 'service_request':
      return `/api/v1/service-requests/${target.id}`;
    case 'msp_ticket':
      return target.customerTenantId
        ? `/api/v1/msp/tickets/${target.id}?customerTenantId=${target.customerTenantId}`
        : null;
    default:
      return null;
  }
}

/** 探测通知目标是否仍可访问。仅 404 判定为 missing，其它错误不阻断跳转。 */
export async function checkNotificationTarget(actionUrl: string): Promise<NotificationTargetStatus> {
  const target = parseNotificationTarget(actionUrl);
  if (!target) return 'unknown';
  const path = detailPath(target);
  if (!path) return 'unknown';
  try {
    await httpClient.get(path);
    return 'ok';
  } catch (err) {
    const candidate = err as HttpClientError | undefined;
    return candidate?.httpStatus === 404 ? 'missing' : 'unknown';
  }
}
