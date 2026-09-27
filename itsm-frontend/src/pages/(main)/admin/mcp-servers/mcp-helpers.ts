import type {
  MCPConnectionTestResult,
  MCPServer,
  MCPServerSummary,
  MCPTool,
} from '@/lib/api/mcp-api';

/**
 * MCP 管理页纯逻辑（M0-12）：状态口径、掩码展示、表单校验与轮询判定。
 * 与 UI 解耦，便于组件测试直接断言（不渲染 antd）。
 */

/** 服务器状态色（三态分离：enabled=管理位 / running_status=运行态 / status=最近一次结果）。 */
export type StatusTone = 'success' | 'warning' | 'error' | 'default';

/** 运行态色：connected=success、connecting/degraded=warning、error=error、其余 default。 */
export const runningStatusTone = (server: Pick<MCPServer, 'running_status' | 'enabled'>): StatusTone => {
  if (!server.enabled) return 'default';
  switch (server.running_status) {
    case 'connected':
      return 'success';
    case 'connecting':
    case 'reconnecting':
    case 'degraded':
      return 'warning';
    case 'error':
      return 'error';
    default:
      return 'default';
  }
};

/** 健康摘要中的错误计数（含认证失效，用于高亮）。 */
export const hasHealthAlert = (summary?: MCPServerSummary): boolean =>
  !!summary && (summary.error > 0 || summary.auth_required > 0);

/** 工具生效口径：配置开启 + 服务器运行可用 + 未隔离 + 健康。 */
export const isToolEffective = (tool: MCPTool): boolean =>
  tool.enabled && tool.healthy && !tool.quarantined;

/** 工具状态标签（优先级：隔离 > 配置关闭 > 不健康 > 生效）。 */
export const toolStatusKey = (tool: MCPTool): 'quarantined' | 'disabled' | 'unhealthy' | 'active' => {
  if (tool.quarantined) return 'quarantined';
  if (!tool.configured_enabled) return 'disabled';
  if (!tool.healthy) return 'unhealthy';
  return 'active';
};

/** 掩码键值对 → 展示行（空对象 = 无）。 */
export const maskEntries = (masked?: Record<string, string>): Array<{ key: string; value: string }> =>
  Object.entries(masked ?? {}).map(([key, value]) => ({ key, value }));

/** 掩码展示：仅展示键名与掩码值，永不展示原始输入（更新时空 = 不修改）。 */
export const maskedHint = (masked?: Record<string, string>): string => {
  const keys = Object.keys(masked ?? {});
  return keys.length ? keys.join(', ') : '';
};

/** 名称校验：后端 `^[a-z0-9_-]{1,32}$`（Save 后不可改）。 */
export const SERVER_NAME_PATTERN = /^[a-z0-9_-]{1,32}$/;

export const validateServerName = (value?: string): string | undefined => {
  const trimmed = (value ?? '').trim();
  if (!trimmed) return 'validation.nameRequired';
  if (!SERVER_NAME_PATTERN.test(trimmed)) return 'validation.namePattern';
  return undefined;
};

/** URL 校验：仅 http(s)、必须带主机、不得含用户信息（与后端 ValidateURL 口径一致）。 */
export const validateServerURL = (value?: string): string | undefined => {
  const trimmed = (value ?? '').trim();
  if (!trimmed) return 'validation.urlRequired';
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return 'validation.urlScheme';
    if (!parsed.hostname) return 'validation.urlHost';
    if (parsed.username || parsed.password) return 'validation.urlUserInfo';
    return undefined;
  } catch {
    return 'validation.urlInvalid';
  }
};

/** 超时范围（毫秒，与后端 MinTimeoutMS/MaxTimeoutMS 一致）。 */
export const TIMEOUT_RANGE = { min: 1000, max: 300000 } as const;
/** 并发上限范围（与后端 MinParallelCalls/MaxParallelCalls 一致）。 */
export const PARALLEL_RANGE = { min: 1, max: 32 } as const;
/** 重试上限（后端限制 0–5）。 */
export const RETRY_RANGE = { min: 0, max: 5 } as const;

/** 凭据类型是否需要填写键值（none = 不需要）。 */
export const credentialRequired = (credentialType?: string): boolean =>
  credentialType === 'static_header' || credentialType === 'oauth2';

/**
 * 轮询截止判定（D8：202 + 2s 轮询回读）。
 * `startedAtMs` 起始时间；超过 `timeoutMs` 视为超时（UI 提示"仍在处理"）。
 */
export const pollExpired = (startedAtMs: number, nowMs: number, timeoutMs = 30000): boolean =>
  nowMs - startedAtMs >= timeoutMs;

/** 是否应继续轮询：服务器仍在过渡态（connecting/reconnecting）或管理位刚变更。 */
export const shouldKeepPolling = (server: Pick<MCPServer, 'running_status'>, targetEnabled?: boolean): boolean => {
  if (targetEnabled === true && server.running_status !== 'connected' && server.running_status !== 'error') {
    return true;
  }
  if (targetEnabled === false && server.running_status === 'disconnecting') return true;
  return server.running_status === 'connecting' || server.running_status === 'reconnecting';
};

/** 测试连接结果的展示摘要键（成功/失败共用）。 */
export const testResultSummary = (result: MCPConnectionTestResult): string => {
  if (result.ok) {
    return `${result.server_name || '-'} · ${result.protocol_version || '-'} · ${result.tool_count} tools · ${result.duration_ms}ms`;
  }
  return `${result.error_code || 'error'}${result.message ? ` · ${result.message}` : ''}`;
};

/** 汇总卡计数（空对象兜底，避免 undefined 渲染）。 */
export const emptySummary: MCPServerSummary = {
  total: 0,
  enabled: 0,
  connected: 0,
  error: 0,
  auth_required: 0,
  tools: 0,
  enabled_tools: 0,
  quarantined_tools: 0,
};

/** 批量操作二次确认文案所需的计数。 */
export const bulkConfirmCount = (tools: MCPTool[]): number => tools.length;
