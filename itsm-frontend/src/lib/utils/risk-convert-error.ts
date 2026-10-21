import type { HttpClientError } from '@/lib/api/http-client';

export type RiskConvertTarget = 'problem' | 'change';

const TARGET_LABEL: Record<RiskConvertTarget, string> = {
  problem: '问题单',
  change: '变更单',
};

/** 从 http-client 错误中取后端包络 data（409 的 existingId / 500 的 createdId 等）。 */
function readErrorData(error: unknown): Record<string, unknown> | null {
  const data = (error as Partial<HttpClientError> | undefined)?.data;
  return data && typeof data === 'object' ? (data as Record<string, unknown>) : null;
}

/**
 * 风险/问题转换失败 → 用户可读文案。
 *
 * 背景：后端 `ConvertRisk` 对同目标二次转换返回 409 + `data.existingId`
 * （handlers/project/handler.go），未装配端口返回 503，端口失败返回 500；
 * 但 `http-client` 的异常只带 code/message 时，调用方曾把所有情况压成一句
 * 「服务未配置或参数无效」。这里按 HTTP 状态 + 后端明细分流，并附请求号便于报障。
 */
export function describeRiskConvertError(error: unknown, target: RiskConvertTarget): string {
  const err = error as Partial<HttpClientError> | undefined;
  const data = readErrorData(error);
  const existingId = typeof data?.existingId === 'number' ? data.existingId : undefined;
  const label = TARGET_LABEL[target];
  const requestId = err?.requestId ? `（请求号 ${err.requestId}）` : '';

  if (existingId) {
    return `该风险/问题已转为${label} #${existingId}，请直接查看，勿重复转换`;
  }
  if (err?.httpStatus === 409 || err?.code === 4090) {
    if (data?.hint === 'reopen_before_conversion' || err?.message?.includes('risk is closed')) {
      return `转换失败：该风险/问题已关闭，请先重开后再转换${requestId}`;
    }
    return `该风险/问题已转换，请刷新后查看既有${label}${requestId}`;
  }
  if (err?.httpStatus === 400 || err?.code === 1001) {
    if (err?.message?.includes('project is closed')) {
      return `转换失败：项目已结项或已取消，不能继续转换${requestId}`;
    }
    return `转换失败：${err?.message || '参数无效，请检查必填项'}${requestId}`;
  }
  if (err?.httpStatus === 403 || err?.code === 2003) {
    return `转换失败：缺少项目写权限${requestId}`;
  }
  if (err?.httpStatus === 404) {
    return `转换失败：风险/问题或项目不存在，请刷新后重试${requestId}`;
  }
  if (err?.httpStatus === 503 || err?.code === 5003) {
    return `${label}服务暂时不可用（或未配置），请稍后重试或联系管理员${requestId}`;
  }
  if (err?.httpStatus === 500 || err?.code === 5001) {
    return `转换失败：${err?.message || `${label}服务内部错误`}${requestId}`;
  }
  return `转换失败：${err?.message || '请稍后重试或联系管理员'}${requestId}`;
}
