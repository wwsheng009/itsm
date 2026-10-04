/**
 * 创建工单错误 → 用户可读文案（IP-P2-6 租户硬配额的 UI 呈现收口）。
 *
 * 背景：`POST /api/v1/tickets` 的超限拒绝由后端 `handlers/ticket/handler.go:149`
 * 统一映射为 422（`TENANT_QUOTA_EXCEEDED`），但 envelope.message 是英文技术文案
 * （`tenant quota exceeded: maxTicketsPerMonth (limit=..., used=...)`）。
 * UI 层验收（flow-msp-error-presentation.spec.ts）要求错误态可见且对用户可读，
 * 此处做一次集中翻译，避免各调用点各自拼装。
 *
 * 判定依据是 `httpStatus`（HTTP 422），**不是** `code`——后者是 envelope 内的业务码
 * （`common.UnprocessableEntityCode`），与 HTTP 状态码不同值（见 http-client.ts:568-571）。
 */
export function mapTicketCreateError(e: unknown): string {
  const err = e as
    | { httpStatus?: number; code?: number; message?: string; error?: { message?: string } }
    | null;

  // 该端点当前唯一使用 422 的场景就是租户硬配额（见文件头注释）；按 HTTP 状态判定，
  // 不依赖英文 message 内容，避免后端文案本地化后失配。
  if (err?.httpStatus === 422) {
    return '本月工单数量已达租户配额上限，请联系租户管理员调整配额后重试';
  }

  return err?.message || err?.error?.message || '创建工单失败，请检查输入或重新登录';
}
