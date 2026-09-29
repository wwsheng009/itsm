import { httpClient, type HttpClientError } from './http-client';

/**
 * Bot 模板与工具授权管理 API 客户端（B2-03，对接 B2-01 冻结契约）。
 *
 * 契约来源：`itsm-backend/router/bot_routes.go` + `itsm-backend/handlers/ai/bot_admin.go`，
 * 端点前缀 `/api/v1/admin/bots`：
 *  - 读（`ai:read`） ：列表、详情、授权清单
 *  - 写（`ai:write`）：CRUD、授权 upsert/删除
 *  - 工作区选择器：`GET /api/v1/agent/bots`（B2-04，见 ai-api.ts 的 `aiListVisibleBots`）
 *
 * 响应契约：统一 `{code,message,data}` 包络（`http-client.ts` 只解包 `data`）——后端**禁止裸 JSON**；
 * 2026-09-27 修复：`bot_admin.go` 曾返回裸 JSON 导致本页与工作区选择器静默为空（证据
 * `docs/plan/evidence/bot-b3/S4-S7-business-bots-evidence.md` §5.2）。
 *
 * 回滚语义：`bot.enabled=false` 时 bootstrap 不注入 handler，整组路由**不注册** →
 * gin 404 且无 errorCode；本文件通过 `isBotFeatureDisabled` 识别，UI 降级为"功能未启用"引导。
 *
 * 枚举字面量对齐后端 `service/bot/admin.go`：
 *  - status：`draft` | `pilot` | `ga`（缺省 draft，draft 不下发、不可见）
 *  - riskLimit：`read` | `plan` | `act_low` | `act_medium` | `act_high`
 *  - audience：`internal` | `end_user` | `all`（空 = internal；end_user 角色仅可见 end_user/all）
 */

// ==================== 类型 ====================

/** 模板状态（与后端 StatusDraft/StatusPilot/StatusGA 同字面量）。 */
export type BotStatus = 'draft' | 'pilot' | 'ga';
export const BOT_STATUSES: readonly BotStatus[] = ['draft', 'pilot', 'ga'] as const;

/** 工具风险级别（与 service.ToolRisk* 同字面量）。 */
export type BotRisk = 'read' | 'plan' | 'act_low' | 'act_medium' | 'act_high';
export const BOT_RISKS: readonly BotRisk[] = ['read', 'plan', 'act_low', 'act_medium', 'act_high'] as const;

/** 风险级别序号（越大约危险；用于"授权上限不得超过模板上限"的前端预检）。 */
export const BOT_RISK_RANK: Record<BotRisk, number> = {
  read: 0,
  plan: 1,
  act_low: 2,
  act_medium: 3,
  act_high: 4,
};

/** Bot 受众（决定工作区可见性与角色过滤）。 */
export type BotAudience = 'internal' | 'end_user' | 'all';
export const BOT_AUDIENCES: readonly BotAudience[] = ['internal', 'end_user', 'all'] as const;

/**
 * 入口建议值。
 *
 * 后端对 entrypoints 只做**非空**校验（`service/bot/admin.go` validateEntrypoints），取值开放；
 * 已冻结常量仅 `chat`（`service/bot/policy.go:29` `EntrypointChat`）。其余入口名待 B3-01
 * 枚举落地，先以自由录入（tags）承载，避免前端伪枚举与后端发散。
 */
export const BOT_ENTRYPOINT_SUGGESTIONS = ['chat'] as const;

/** 管理面模板视图（对齐 `botTemplateView`）。 */
export interface BotTemplate {
  id: number;
  slug: string;
  name: string;
  audience: string;
  riskLimit: string;
  /** 后端原样返回 JSON 字符串（如 `["chat"]`）。 */
  entrypointsJson: string;
  systemPromptRef: string;
  status: string;
  createdAt?: string;
  updatedAt?: string;
}

/** 工具授权视图（对齐 `botGrantView`）。 */
export interface BotGrant {
  id: number;
  botId: number;
  toolName: string;
  riskLimit: string;
  /** 参数策略 JSON 字符串；空 = 不限制。 */
  argsPolicyJson: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface BotTemplateInput {
  slug?: string;
  name: string;
  audience: string;
  riskLimit: string;
  entrypoints: string[];
  systemPromptRef?: string;
  status: string;
}

export interface BotGrantInput {
  toolName: string;
  riskLimit: string;
  argsPolicyJson?: string;
}

export interface BotTemplateListResult {
  items: BotTemplate[];
  total: number;
}

export interface BotGrantListResult {
  items: BotGrant[];
  total: number;
}

// ==================== 错误与开关判定 ====================

/** 提取错误上的字符串错误码（类型安全窄化）。 */
export const botErrorCode = (err: unknown): string | undefined => {
  const candidate = err as HttpClientError | undefined;
  return typeof candidate?.errorCode === 'string' && candidate.errorCode
    ? candidate.errorCode
    : undefined;
};

/** 可读错误提示：后端 message 优先（管理面校验信息含具体字段原因），fallback 兜底。 */
export const describeBotError = (err: unknown, fallback = '操作失败，请稍后重试'): string => {
  if (err instanceof Error && err.message) return err.message;
  return fallback;
};

/** 灰度开关关闭判定（整组路由未注册 → 404 且无 errorCode；403 属权限不足，不在此列）。 */
export const isBotFeatureDisabled = (err: unknown): boolean => {
  const candidate = err as HttpClientError | undefined;
  if (!candidate || typeof candidate !== 'object') return false;
  if (candidate.errorCode) return false;
  return candidate.httpStatus === 404;
};

/** 权限不足判定（403：无 ai:write — 只读用户仍可浏览列表）。 */
export const isBotPermissionDenied = (err: unknown): boolean => {
  const candidate = err as HttpClientError | undefined;
  return candidate?.httpStatus === 403;
};

// ==================== 纯函数（供 UI 与测试共用） ====================

/** 解析 `entrypointsJson`（损坏/空 → []，绝不抛错）。 */
export const parseEntrypoints = (raw: unknown): string[] => {
  if (Array.isArray(raw)) {
    return raw.filter((item): item is string => typeof item === 'string' && item.trim() !== '');
  }
  if (typeof raw !== 'string' || raw.trim() === '') return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((item): item is string => typeof item === 'string' && item.trim() !== '');
  } catch {
    return [];
  }
};

/** 序列化入口数组（去重 + trim + 排序稳定）。 */
export const serializeEntrypoints = (values: string[]): string =>
  JSON.stringify(Array.from(new Set(values.map(v => v.trim()).filter(Boolean))));

/**
 * slug **建议**格式（非硬校验）。
 *
 * 后端只强制「非空 + 租户内唯一」（`service/bot/admin.go` CreateTemplate），未做正则与长度
 * 限制；这里仅用于表单**软提示**，绝不阻塞提交（避免前端比后端更严造成合法输入被拒）。
 */
export const BOT_SLUG_HINT_PATTERN = /^[a-z0-9_-]+$/;

/** 授权风险上限是否超过模板上限（前端预检；后端 `TemplateAdmin` 亦强校验）。 */
export const grantRiskExceeds = (templateRisk: string, grantRisk: string): boolean => {
  const templateRank = BOT_RISK_RANK[templateRisk as BotRisk];
  const grantRank = BOT_RISK_RANK[grantRisk as BotRisk];
  if (templateRank === undefined || grantRank === undefined) return false;
  return grantRank > templateRank;
};

/** 影响面预览的告警码（文案由 UI 经 i18n 渲染，避免纯函数里硬编码中文）。 */
export type ImpactWarning = 'no_entrypoints' | 'draft' | 'no_grants';

/** 影响面预览：给定 audience + entrypoints，说明谁将看到/可调用（纯函数，UI 与测试共用）。 */
export interface ImpactPreview {
  /** 命中的受众（机器值：internal / end_user）。 */
  audiences: Array<'internal' | 'end_user'>;
  entrypoints: string[];
  /** 生效前置条件（未满足则不会生效）。 */
  warnings: ImpactWarning[];
}

export const buildImpactPreview = (
  template: Pick<BotTemplateInput, 'audience' | 'entrypoints' | 'status'>,
  activeGrantCount: number
): ImpactPreview => {
  const audience = (template.audience || 'internal').trim() || 'internal';
  const audiences: Array<'internal' | 'end_user'> =
    audience === 'all' ? ['internal', 'end_user'] : audience === 'end_user' ? ['end_user'] : ['internal'];
  const entrypoints = Array.from(new Set((template.entrypoints ?? []).map(v => v.trim()).filter(Boolean)));
  const warnings: ImpactWarning[] = [];
  if (entrypoints.length === 0) {
    warnings.push('no_entrypoints');
  }
  if ((template.status || 'draft') === 'draft') {
    warnings.push('draft');
  }
  if (activeGrantCount === 0) {
    warnings.push('no_grants');
  }
  return { audiences, entrypoints, warnings };
};

// ==================== API ====================

const BASE = '/api/v1/admin/bots';

class BotApi {
  /** 模板列表（按租户；首次调用后端会幂等种入内置默认助手）。 */
  async listTemplates(): Promise<BotTemplateListResult> {
    return httpClient.get(BASE) as Promise<BotTemplateListResult>;
  }

  async getTemplate(id: number): Promise<BotTemplate> {
    return httpClient.get(`${BASE}/${id}`) as Promise<BotTemplate>;
  }

  async createTemplate(input: BotTemplateInput): Promise<BotTemplate> {
    return httpClient.post(BASE, input) as Promise<BotTemplate>;
  }

  /** 更新模板（slug 不可修改：传空即可，后端对非空 slug 直接 400）。 */
  async updateTemplate(id: number, input: Omit<BotTemplateInput, 'slug'>): Promise<BotTemplate> {
    return httpClient.put(`${BASE}/${id}`, input) as Promise<BotTemplate>;
  }

  async deleteTemplate(id: number): Promise<{ deleted: boolean }> {
    return httpClient.delete(`${BASE}/${id}`) as Promise<{ deleted: boolean }>;
  }

  async listGrants(botId: number): Promise<BotGrantListResult> {
    return httpClient.get(`${BASE}/${botId}/grants`) as Promise<BotGrantListResult>;
  }

  /** 新增或更新授权（同工具名 upsert；风险上限不得超过模板上限，后端强校验）。 */
  async upsertGrant(botId: number, input: BotGrantInput): Promise<BotGrant> {
    return httpClient.put(`${BASE}/${botId}/grants`, input) as Promise<BotGrant>;
  }

  async deleteGrant(botId: number, grantId: number): Promise<{ deleted: boolean }> {
    return httpClient.delete(`${BASE}/${botId}/grants/${grantId}`) as Promise<{ deleted: boolean }>;
  }
}

export const botApi = new BotApi();
export default botApi;
