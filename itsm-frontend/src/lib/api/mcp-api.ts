import { httpClient, type HttpClientError } from './http-client';

/**
 * MCP 外部工具管理 API 客户端（M0-12，对接 M0-08 冻结契约）。
 *
 * 契约来源：`itsm-backend/handlers/mcp/handler.go` + `itsm-backend/mcp/admin/service.go`，
 * 端点前缀 `/api/v1/ai/mcp-servers`：
 *  - 读（`mcp:read`）  ：列表、健康摘要、详情、工具清单、事件
 *  - 写（`mcp:admin`） ：CRUD、测试连接、启停/重载、工具治理、凭据轮换
 *
 * 回滚语义（D6 四级开关）：`mcp.enabled=false` 时 bootstrap 不注入 handler，整组路由
 * **不注册** → gin 404 且无 errorCode；服务未就绪（加密缺失等）时为 503 + `unavailable`。
 * 二者都通过本文件的判定入口识别，UI 统一降级为"功能未启用"引导。
 *
 * 安全：请求/响应均不含凭据明文——服务端只回掩码（`credential_masked`/`headers_masked`），
 * 更新时空凭据 = 不修改；本文件不做任何凭据本地持久化。
 */

// ==================== 错误码（§5.5 枚举，与后端 errors.go 一一对应） ====================

export const MCP_ERROR_MESSAGES: Record<string, string> = {
  invalid_transport: '传输类型非法：一期仅支持 Streamable HTTP / SSE',
  duplicate_name: '同名服务器已存在（租户内唯一）',
  ssrf_blocked: '出站安全校验拒绝：目标地址不允许访问（内网/保留网段/metadata 端点）',
  connect_timeout: '建连超时：请检查地址可达性与网络策略',
  tls_error: 'TLS 握手或证书校验失败',
  auth_required: '服务端要求认证或凭据被拒：请检查凭据配置',
  protocol_mismatch: 'MCP 协议版本不匹配（不降级）：请升级服务端或调整版本',
  unreachable: '目标不可达：DNS 解析失败或网络拒绝',
  not_found: '对象不存在或已被删除',
  conflict: '版本冲突：数据已被他人修改，请刷新后重试',
  credential_error: '凭据处理失败（明文不会回显）',
  validation_failed: '参数校验失败：请检查表单必填项与格式',
  server_disabled: '服务器未启用：请先启用并确认连接成功',
  unavailable: 'MCP 管理服务未就绪（功能开关或依赖缺失）',
  internal_error: '服务内部错误，请稍后重试',
};

/** 提取错误上的字符串错误码（类型安全窄化）。 */
export const mcpErrorCode = (err: unknown): string | undefined => {
  const candidate = err as HttpClientError | undefined;
  return typeof candidate?.errorCode === 'string' && candidate.errorCode
    ? candidate.errorCode
    : undefined;
};

/** 可读错误提示：错误码映射 → 后端 message → fallback。 */
export const describeMCPError = (err: unknown, fallback = '操作失败，请稍后重试'): string => {
  const code = mcpErrorCode(err);
  if (code && MCP_ERROR_MESSAGES[code]) return MCP_ERROR_MESSAGES[code];
  if (err instanceof Error && err.message) return err.message;
  return fallback;
};

/** 灰度开关关闭判定（整组路由未注册 → 404 且无 errorCode）。 */
export const isMCPFeatureDisabled = (err: unknown): boolean => {
  const candidate = err as HttpClientError | undefined;
  if (!candidate || typeof candidate !== 'object') return false;
  if (candidate.errorCode) return false;
  return candidate.httpStatus === 404;
};

/** 管理服务未就绪判定（503 或 `unavailable` 错误码）。 */
export const isMCPServiceUnavailable = (err: unknown): boolean => {
  const code = mcpErrorCode(err);
  if (code === 'unavailable') return true;
  const candidate = err as HttpClientError | undefined;
  return candidate?.httpStatus === 503;
};

// ==================== 类型（对齐 admin 包 DTO） ====================

/** 传输类型（一期仅远程）。 */
export type MCPTransport = 'streamable_http' | 'sse';
/** 凭据类型（`none` = 无凭据）。 */
export type MCPCredentialType = 'none' | 'static_header' | 'oauth2';
/** 信任级别（D7 默认 untrusted）。 */
export type MCPTrustLevel = 'trusted' | 'untrusted';
/** 工具风险级别（Phase 1 仅展示与统计）。 */
export type MCPRisk = 'read' | 'plan' | 'act_low' | 'act_medium' | 'act_high';

/** 管理面服务器视图（凭据字段恒为掩码）。 */
export interface MCPServer {
  id: number;
  name: string;
  display_name: string;
  transport: MCPTransport | string;
  url: string;
  credential_type: MCPCredentialType | string;
  trust_level: MCPTrustLevel | string;
  enabled: boolean;
  status: string;
  running_status: string;
  last_error: string;
  protocol_version: string;
  server_info: string;
  timeout_ms: number;
  max_parallel_calls: number;
  max_retry: number;
  version: number;
  tool_count: number;
  enabled_tool_count: number;
  quarantined_tool_count: number;
  headers_masked: Record<string, string>;
  credential_masked: Record<string, string>;
  created_at: string;
  updated_at: string;
}

/** 列表/健康摘要计数。 */
export interface MCPServerSummary {
  total: number;
  enabled: number;
  connected: number;
  error: number;
  auth_required: number;
  tools: number;
  enabled_tools: number;
  quarantined_tools: number;
}

export interface MCPServerListResult {
  items: MCPServer[];
  summary: MCPServerSummary;
}

/** 工具治理视图（三态分离：enabled=生效中 / configured_enabled=配置值 / healthy=发现态）。 */
export interface MCPTool {
  id: number;
  raw_name: string;
  callable_name: string;
  description: string;
  input_schema: string;
  schema_hash: string;
  read_only: boolean;
  risk: MCPRisk | string;
  category: string;
  enabled: boolean;
  healthy: boolean;
  configured_enabled: boolean;
  quarantined: boolean;
  quarantine_reason: string;
  last_error: string;
  discovered_at: string;
  updated_at: string;
  server_running_state: string;
}

export interface MCPToolPreview {
  raw_name: string;
  callable_name: string;
  description: string;
}

/** 测试连接结果（同步 ≤10s；不落库）。 */
export interface MCPConnectionTestResult {
  ok: boolean;
  protocol_version: string;
  server_name: string;
  server_version: string;
  tools: MCPToolPreview[];
  tool_count: number;
  duration_ms: number;
  error_code?: string;
  message?: string;
}

/**
 * 服务器生命周期事件（内存环形缓冲）。
 *
 * 注意：后端 `manager.Event` 未加 json tag，字段名按 Go 导出名序列化（PascalCase）。
 * `Type` 取值形如 `mcp.server.connected` / `mcp.tools.discovered` / `mcp.tool.quarantined`。
 */
export interface MCPServerEvent {
  Type: string;
  TenantID: number;
  ServerID: number;
  Server: string;
  Tool: string;
  Detail: string;
  At: string;
}

export interface MCPCreateServerRequest {
  name: string;
  display_name: string;
  transport: MCPTransport;
  url: string;
  headers?: Record<string, string>;
  credential_type?: MCPCredentialType;
  credential?: Record<string, string>;
  timeout_ms?: number;
  max_parallel_calls?: number;
  max_retry?: number;
  trust_level?: MCPTrustLevel;
}

/** 更新请求：`version` 必填（乐观锁）；空/缺省 credential 与 headers = 不修改。 */
export interface MCPUpdateServerRequest {
  version: number;
  display_name?: string;
  url?: string;
  transport?: MCPTransport;
  credential_type?: MCPCredentialType;
  headers?: Record<string, string>;
  credential?: Record<string, string>;
  timeout_ms?: number;
  max_parallel_calls?: number;
  max_retry?: number;
  trust_level?: MCPTrustLevel;
}

export interface MCPTestServerRequest {
  transport?: MCPTransport;
  url?: string;
  headers?: Record<string, string>;
  credential?: Record<string, string>;
}

export interface MCPClassificationRequest {
  read_only?: boolean;
  risk?: MCPRisk;
  category?: string;
}

export interface MCPBulkToolRequest {
  tools?: string[];
  enabled: boolean;
}

export interface MCPRotateCredentialRequest {
  credential_type: MCPCredentialType;
  credential?: Record<string, string>;
  headers?: Record<string, string>;
}

// ==================== 客户端 ====================

const BASE = '/api/v1/ai/mcp-servers';

class MCPApi {
  /** 服务器列表 + 摘要。 */
  async listServers(): Promise<MCPServerListResult> {
    const r = await httpClient.get<MCPServerListResult>(BASE);
    return { items: r.items || [], summary: r.summary };
  }

  /** 健康摘要（复用列表投影，读端点为 /health）。 */
  async healthSummary(): Promise<MCPServerListResult> {
    const r = await httpClient.get<MCPServerListResult>(`${BASE}/health`);
    return { items: r.items || [], summary: r.summary };
  }

  async getServer(id: number): Promise<MCPServer> {
    return httpClient.get<MCPServer>(`${BASE}/${id}`);
  }

  async createServer(payload: MCPCreateServerRequest): Promise<MCPServer> {
    return httpClient.post<MCPServer>(BASE, payload);
  }

  async updateServer(id: number, payload: MCPUpdateServerRequest): Promise<MCPServer> {
    return httpClient.put<MCPServer>(`${BASE}/${id}`, payload);
  }

  async deleteServer(id: number): Promise<{ deleted: boolean }> {
    return httpClient.delete<{ deleted: boolean }>(`${BASE}/${id}`);
  }

  /** 测试连接（覆盖项为空则用已存配置；不落库）。 */
  async testServer(id: number, payload?: MCPTestServerRequest): Promise<MCPConnectionTestResult> {
    return httpClient.post<MCPConnectionTestResult>(`${BASE}/${id}/test`, payload ?? {});
  }

  /** 启用（202 + 状态回读：调用方按 2s 轮询 getServer）。 */
  async enableServer(id: number): Promise<MCPServer> {
    return httpClient.post<MCPServer>(`${BASE}/${id}/enable`, {});
  }

  async disableServer(id: number): Promise<MCPServer> {
    return httpClient.post<MCPServer>(`${BASE}/${id}/disable`, {});
  }

  async reloadServer(id: number): Promise<MCPServer> {
    return httpClient.post<MCPServer>(`${BASE}/${id}/reload`, {});
  }

  async listTools(id: number): Promise<MCPTool[]> {
    const r = await httpClient.get<{ items: MCPTool[]; total: number }>(`${BASE}/${id}/tools`);
    return r.items || [];
  }

  async setToolEnabled(id: number, callableName: string, enabled: boolean): Promise<MCPTool> {
    const suffix = enabled ? 'enable' : 'disable';
    return httpClient.post<MCPTool>(
      `${BASE}/${id}/tools/${encodeURIComponent(callableName)}/${suffix}`,
      {},
    );
  }

  /** 批量启停（`tools` 为空 = 全部）。 */
  async bulkSetTools(
    id: number,
    payload: MCPBulkToolRequest,
  ): Promise<{ affected: number; enabled: boolean }> {
    return httpClient.post<{ affected: number; enabled: boolean }>(`${BASE}/${id}/tools/bulk`, payload);
  }

  /** 工具分类标注（read_only/risk/category）。 */
  async setToolClassification(
    id: number,
    callableName: string,
    payload: MCPClassificationRequest,
  ): Promise<MCPTool> {
    return httpClient.put<MCPTool>(
      `${BASE}/${id}/tools/${encodeURIComponent(callableName)}/classification`,
      payload,
    );
  }

  /** 凭据轮换（只写不读回）。 */
  async rotateCredential(id: number, payload: MCPRotateCredentialRequest): Promise<MCPServer> {
    return httpClient.post<MCPServer>(`${BASE}/${id}/rotate-credential`, payload);
  }

  async listEvents(id: number): Promise<MCPServerEvent[]> {
    const r = await httpClient.get<{ items: MCPServerEvent[]; total: number }>(`${BASE}/${id}/events`);
    return r.items || [];
  }
}

export const mcpApi = new MCPApi();
export default mcpApi;
