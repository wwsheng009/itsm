import { httpClient } from './http-client';
import type {
  SystemConfig,
  SystemConfigListResponse,
  UpdateSystemConfigRequest,
  GetSystemConfigsParams,
} from './api-config';

// ==================== AI 能力开关（运行时能力，免重启） ====================

/**
 * 三个运行时能力开关键（与后端 `system_configs.key` 一一对应）。
 *
 * 语义：缺行 = 跟随环境默认；写入覆盖行后立即生效（缓存失效），无需重启。
 */
export type AICapabilityKey = 'mcp.enabled' | 'mcp.write_enabled' | 'bot.enabled';

/** 三个键的静态（环境/配置文件）默认值。 */
export interface AICapabilityDefaults {
  mcpEnabled: boolean;
  mcpWriteEnabled: boolean;
  botEnabled: boolean;
}

/**
 * 三态更新请求：
 * - 字段**缺省 = 不修改**；
 * - `reset` 列出的键 = 删除覆盖行、恢复跟随环境默认（后端按 snake_case key 接收）。
 */
export interface AICapabilitiesPatch {
  mcpEnabled?: boolean;
  mcpWriteEnabled?: boolean;
  botEnabled?: boolean;
  reset?: AICapabilityKey[];
}

/**
 * AI 能力开关快照（`GET/PUT /api/v1/system-configs/ai-capabilities`，camelCase）。
 *
 * - `overridden`：已被管理台覆盖的键 → true（未列出的键 = 跟随环境默认）；
 * - 权限：GET 需 `system_config:read`，PUT 需 `system_config:write`（仅超级管理员）。
 */
export interface AICapabilities {
  mcpEnabled: boolean;
  mcpWriteEnabled: boolean;
  botEnabled: boolean;
  defaults: AICapabilityDefaults;
  /**
   * 已被管理台覆盖的键 → true（未列出的键 = 跟随环境默认）。
   *
   * 注意：后端返回的 map key 是 `system_configs` 原始键名（snake_case，如 `mcp.write_enabled`），
   * 但默认 `httpClient` 会递归归一化响应对象的 key → 实际读到的可能是 `mcpWriteEnabled`。
   * 读取请统一使用 `isAICapabilityOverridden()`（两种形态都兼容）。
   */
  overridden: Partial<Record<string, boolean>>;
  updatedAt: string;
  updatedBy: string;
  keys: AICapabilityKey[];
}

const AI_CAPABILITIES_ENDPOINT = '/api/v1/system-configs/ai-capabilities';

/**
 * 能力键的候选形态：
 *  1. 后端原始 key（snake_case，含点号）：`mcp.write_enabled`；
 *  2. httpClient 归一化后的**实际**形态（只把 `_x` 转成大写，点号保留，见 http-client.ts 的 toCamelCase）：
 *     `mcp.writeEnabled`；
 *  3. 防御形态：把 `.`/`_`/`-` 全部视作分隔符的紧凑 camel：`mcpWriteEnabled`（历史/他处归一化实现）。
 */
const capabilityKeyVariants = (key: string): string[] => {
  const underscoreCamel = key.replace(/_([a-z])/g, (_m, ch: string) => ch.toUpperCase());
  const compactCamel = key.replace(/[._-]([a-z])/g, (_m, ch: string) => ch.toUpperCase());
  return [key, underscoreCamel, compactCamel];
};

/**
 * 判断某个能力键是否被管理台覆盖。
 *
 * 兼容两种响应形态：snake_case（后端原始 map key，契约示例）与 camelCase
 * （`httpClient` 默认归一化后的实际形态）。
 */
export const isAICapabilityOverridden = (
  capabilities: Pick<AICapabilities, 'overridden'> | null | undefined,
  key: AICapabilityKey,
): boolean => {
  const map = capabilities?.overridden;
  if (!map) return false;
  return capabilityKeyVariants(key).some(variant => map[variant] === true);
};

export class SystemConfigAPI {
  // 编辑器默认请求最多 1000 项；显式分页参数优先。
  static async getConfigs(params?: GetSystemConfigsParams): Promise<SystemConfigListResponse> {
    return httpClient.get<SystemConfigListResponse>('/api/v1/system-configs', {
      pageSize: 1000,
      ...params,
    });
  }

  // 获取单个配置项
  static async getConfig(id: number): Promise<SystemConfig> {
    return httpClient.get<SystemConfig>(`/api/v1/system-configs/${id}`);
  }

  // 根据键名获取配置项
  static async getConfigByKey(key: string): Promise<SystemConfig> {
    return httpClient.get<SystemConfig>(`/api/v1/system-configs/key/${key}`);
  }

  // 更新配置项
  static async updateConfig(id: number, data: UpdateSystemConfigRequest): Promise<SystemConfig> {
    return httpClient.put<SystemConfig>(`/api/v1/system-configs/${id}`, data);
  }

  // 批量更新配置项
  static async updateConfigs(data: UpdateSystemConfigRequest[]): Promise<SystemConfig[]> {
    return httpClient.put<SystemConfig[]>('/api/v1/system-configs/batch', data);
  }

  // 获取系统状态信息
  static async getSystemStatus(): Promise<Record<string, unknown>> {
    return httpClient.get<Record<string, unknown>>('/api/v1/system-configs/status');
  }

  /**
   * 读取 AI 能力开关（三键生效值 + 来源 + 静态默认 + 最近更新人/时间）。
   *
   * 需要 `system_config:read`；能力服务未就绪时后端返回 503。
   */
  static async getAICapabilities(): Promise<AICapabilities> {
    return httpClient.get<AICapabilities>(AI_CAPABILITIES_ENDPOINT);
  }

  /**
   * 更新 AI 能力开关（三态：字段缺省 = 不改；`reset` = 恢复跟随环境默认）。
   *
   * 需要 `system_config:write`（仅超级管理员）；返回保存后的同构快照。
   */
  static async updateAICapabilities(patch: AICapabilitiesPatch): Promise<AICapabilities> {
    return httpClient.put<AICapabilities>(AI_CAPABILITIES_ENDPOINT, patch);
  }
}
