import { httpClient, type HttpClientError } from './http-client';

/**
 * 多 LLM Provider 管理 API 客户端（主计划《多 LLM Provider 支持与可切换方案》§3.4 / FE-1）。
 *
 * 契约来源：`itsm-backend/dto/llm_provider_dto.go`，共 10 个端点，全部要求 `system:write`
 *（D12：仅系统管理员，读端点在列）。本文件只做「类型 + 解包 + 错误码映射」，不含 UI 逻辑。
 *
 * 错误：后端错误响应形状为 `{ code:int, errorCode:string, message:string }`；字符串
 * `errorCode` 由 `http-client` 的 `HttpClientError.errorCode` 透出，使用 `describeLLMProviderError`
 * 可得到可读中文提示（FE-1 验收：错误码可读提示）。
 *
 * 灰度：`LLM_MULTI_PROVIDER_ENABLED` 关闭时整组路由不注册（BE-4 §3.4），表现为
 * **404 且无 errorCode** —— `isLLMProviderFeatureDisabled` 是唯一判定入口（FE-2/FE-3/FE-4 共用）。
 */

// ==================== 错误码（§3.4「错误码统一」） ====================

export const LLM_PROVIDER_FORBIDDEN = 'AI_PROVIDER_FORBIDDEN';
export const LLM_PROVIDER_NOT_FOUND = 'AI_PROVIDER_NOT_FOUND';
export const LLM_PROVIDER_DISABLED = 'AI_PROVIDER_DISABLED';
export const LLM_PROVIDER_KEY_MISSING = 'AI_PROVIDER_KEY_MISSING';
export const LLM_PROVIDER_UNAVAILABLE = 'AI_PROVIDER_UNAVAILABLE';
export const LLM_PROVIDER_IS_DEFAULT = 'AI_PROVIDER_IS_DEFAULT';
export const LLM_PROTOCOL_NOT_IMPLEMENTED = 'AI_PROTOCOL_NOT_IMPLEMENTED';

/** 字符串错误码 → 可读中文提示（未命中时回落后端 message）。 */
export const LLM_PROVIDER_ERROR_MESSAGES: Record<string, string> = {
  [LLM_PROVIDER_FORBIDDEN]: '没有权限使用该 Provider（仅系统管理员可切换）',
  [LLM_PROVIDER_NOT_FOUND]: '所选 Provider 不存在或已被删除',
  [LLM_PROVIDER_DISABLED]: '所选 Provider 已禁用',
  [LLM_PROVIDER_KEY_MISSING]: '所选 Provider 未配置 API Key',
  [LLM_PROVIDER_UNAVAILABLE]: 'Provider 管理服务不可用',
  [LLM_PROVIDER_IS_DEFAULT]: '默认 Provider 不能删除，请先切换默认实例',
  [LLM_PROTOCOL_NOT_IMPLEMENTED]: '该协议尚未接入（待独立计划）',
  AI_PROTOCOL_INVALID: '协议取值非法',
  AI_PROTOCOL_VARIANT_INVALID: '协议变体非法',
  AI_ADAPTER_OPTIONS_INVALID: '高级参数非法（需为 JSON 对象、≤4KB 且不含敏感键）',
  AI_PROVIDER_NAME_CONFLICT: '同名 Provider 已存在',
  AI_PROVIDER_NAME_INVALID: 'Provider 标识（key）非法',
  AI_PROVIDER_ENDPOINT_REQUIRED: '该协议/变体必须填写 Endpoint',
  AI_PROVIDER_VALIDATION_ERROR: '参数校验失败',
  AI_PROVIDER_INTERNAL_ERROR: '服务内部错误，请稍后重试',
};

/** 提取错误上的字符串错误码（类型安全窄化）。 */
export const llmProviderErrorCode = (err: unknown): string | undefined => {
  const candidate = err as HttpClientError | undefined;
  return typeof candidate?.errorCode === 'string' && candidate.errorCode
    ? candidate.errorCode
    : undefined;
};

/**
 * 可读错误提示：优先 §3.4 错误码映射，其次后端 message，最后 fallback。
 * 已附加 RID 后缀（http-client 行为），此处不重复拼接。
 */
export const describeLLMProviderError = (err: unknown, fallback = '操作失败，请稍后重试'): string => {
  const code = llmProviderErrorCode(err);
  if (code && LLM_PROVIDER_ERROR_MESSAGES[code]) return LLM_PROVIDER_ERROR_MESSAGES[code];
  if (err instanceof Error && err.message) return err.message;
  return fallback;
};

/**
 * 灰度开关关闭判定：整组路由未注册时 gin 返回 404 且无 errorCode。
 * 注意：管理端点自身的资源级 404（如 `/providers/:id`）**带** errorCode，不会误判。
 */
export const isLLMProviderFeatureDisabled = (err: unknown): boolean => {
  const candidate = err as HttpClientError | undefined;
  if (!candidate || typeof candidate !== 'object') return false;
  if (candidate.errorCode) return false;
  // 403（非管理员）也视为不可用，但不属于「开关关闭」，单独由权限面处理。
  return candidate.httpStatus === 404;
};

/** 管理服务未就绪（开关开启但加密服务缺失 → handler 返回 503）。 */
export const isLLMProviderServiceUnavailable = (err: unknown): boolean => {
  const code = llmProviderErrorCode(err);
  if (code === LLM_PROVIDER_UNAVAILABLE) return true;
  const candidate = err as HttpClientError | undefined;
  return candidate?.httpStatus === 503;
};

// ==================== 类型（对齐 dto/llm_provider_dto.go） ====================

/** 协议/实例能力位（§3.4 available 响应）。 */
export interface LLMCapabilities {
  supportsStream: boolean;
  supportsTools: boolean;
  supportsReasoning: boolean;
  implemented: boolean;
}

/** 协议下拉选项（后端 `LLMProtocolOptions()` 的 DTO 投影，FE-2 数据源）。 */
export interface LLMProtocolOption {
  protocol: string;
  implemented: boolean;
  variants: string[];
  capabilities: LLMCapabilities;
}

/**
 * 管理列表 / 详情项。
 * 安全：不含明文密钥与密文；`maskedApiKey` 是 `common.MaskSecret` 结果。
 */
export interface LLMProvider {
  id: number;
  key: string;
  displayName: string;
  protocol: string;
  variant: string;
  model: string;
  endpoint: string;
  deployment: string;
  adapterOptions?: Record<string, unknown>;
  enabled: boolean;
  isDefault: boolean;
  source: string;
  status: string;
  lastError?: string;
  lastTestedAt?: string;
  hasApiKey: boolean;
  maskedApiKey?: string;
  createdAt: string;
  updatedAt: string;
}

/** GET /api/v1/ai/providers 响应 data。 */
export interface LLMProviderListResponse {
  items: LLMProvider[];
  total: number;
  protocolOptions: LLMProtocolOption[];
}

/** GET /api/v1/ai/providers/available 响应项（无密钥，选择器数据源）。 */
export interface LLMProviderAvailable {
  key: string;
  displayName: string;
  protocol: string;
  variant: string;
  model: string;
  supportsStream: boolean;
  supportsTools: boolean;
  supportsReasoning: boolean;
  implemented: boolean;
  isDefault: boolean;
}

/** POST /api/v1/ai/providers 请求体。 */
export interface LLMCreateProviderRequest {
  name: string;
  displayName: string;
  protocol: string;
  variant?: string;
  model: string;
  endpoint?: string;
  deployment?: string;
  apiKey?: string;
  adapterOptions?: Record<string, unknown>;
  enabled?: boolean;
  isDefault?: boolean;
}

/**
 * PUT /api/v1/ai/providers/:id 请求体。
 * 指针语义（后端）：缺省 = 不修改；`apiKey` 传空串 = 清空密钥；`adapterOptions` 传 `{}` = 清空。
 * `name` 不可修改（更新 name 需删除重建）。
 */
export interface LLMUpdateProviderRequest {
  displayName?: string;
  protocol?: string;
  variant?: string;
  model?: string;
  endpoint?: string;
  deployment?: string;
  apiKey?: string;
  adapterOptions?: Record<string, unknown>;
  enabled?: boolean;
}

/** POST /api/v1/ai/providers/:id/test 响应 data（HTTP 恒 200，连通性失败体现在 ok=false）。 */
export interface LLMProviderTestResponse {
  id: number;
  key: string;
  ok: boolean;
  status: string;
  error?: string;
  testedAt: string;
}

/** POST /api/v1/ai/providers/import-static 响应 data（幂等）。 */
export interface LLMImportStaticResponse {
  updated: boolean;
  created: boolean;
  provider: LLMProvider;
}

/** GET/PUT /api/v1/ai/user-preference 响应 data。 */
export interface LLMUserPreferenceResponse {
  providerKey: string;
  effectiveProviderKey: string;
  source: string;
}

/** DELETE /api/v1/ai/providers/:id 响应 data。 */
export interface LLMProviderDeleteResponse {
  id: number;
  deleted: boolean;
}

// ==================== 端点（10 条，全部 system:write） ====================

/**
 * 多 LLM Provider 管理 API。
 *
 * 方法名与 `router/llm_provider_routes.go` 一一对应；全部路径挂在 `/api/v1/ai` 下。
 * 调用方负责权限判断（`hasPermission('system','write')`）与灰度探测
 *（`isLLMProviderFeatureDisabled`）。
 */
export class LLMProviderApi {
  /** GET /api/v1/ai/providers —— 管理列表（含掩码密钥、状态、默认标记）。 */
  static async listProviders(): Promise<LLMProviderListResponse> {
    return httpClient.get<LLMProviderListResponse>('/api/v1/ai/providers');
  }

  /** POST /api/v1/ai/providers —— 新建实例。 */
  static async createProvider(req: LLMCreateProviderRequest): Promise<LLMProvider> {
    return httpClient.post<LLMProvider>('/api/v1/ai/providers', req);
  }

  /** PUT /api/v1/ai/providers/:id —— 更新（缺省字段不修改）。 */
  static async updateProvider(id: number, req: LLMUpdateProviderRequest): Promise<LLMProvider> {
    return httpClient.put<LLMProvider>(`/api/v1/ai/providers/${id}`, req);
  }

  /** DELETE /api/v1/ai/providers/:id —— 软删（默认实例返回 409 AI_PROVIDER_IS_DEFAULT）。 */
  static async deleteProvider(id: number): Promise<LLMProviderDeleteResponse> {
    return httpClient.delete<LLMProviderDeleteResponse>(`/api/v1/ai/providers/${id}`);
  }

  /** POST /api/v1/ai/providers/:id/test —— 连通性测试（结果内联，HTTP 恒 200）。 */
  static async testProvider(id: number): Promise<LLMProviderTestResponse> {
    return httpClient.post<LLMProviderTestResponse>(`/api/v1/ai/providers/${id}/test`, {});
  }

  /** POST /api/v1/ai/providers/:id/default —— 设租户默认（事务内互斥切换）。 */
  static async setDefaultProvider(id: number): Promise<LLMProvider> {
    return httpClient.post<LLMProvider>(`/api/v1/ai/providers/${id}/default`, {});
  }

  /** POST /api/v1/ai/providers/import-static —— 静态配置导入（幂等）。 */
  static async importStatic(): Promise<LLMImportStaticResponse> {
    return httpClient.post<LLMImportStaticResponse>('/api/v1/ai/providers/import-static', {});
  }

  /** GET /api/v1/ai/providers/available —— 选择器数据（无密钥，仅系统管理员可读）。 */
  static async listAvailable(): Promise<LLMProviderAvailable[]> {
    return httpClient.get<LLMProviderAvailable[]>('/api/v1/ai/providers/available');
  }

  /** GET /api/v1/ai/user-preference —— 我的默认（含生效值 effectiveProviderKey）。 */
  static async getUserPreference(): Promise<LLMUserPreferenceResponse> {
    return httpClient.get<LLMUserPreferenceResponse>('/api/v1/ai/user-preference');
  }

  /**
   * PUT /api/v1/ai/user-preference —— 设置/清除我的默认。
   * `null` = 清除（跟随租户默认）。
   */
  static async setUserPreference(providerKey: string | null): Promise<LLMUserPreferenceResponse> {
    return httpClient.put<LLMUserPreferenceResponse>('/api/v1/ai/user-preference', { providerKey });
  }
}
