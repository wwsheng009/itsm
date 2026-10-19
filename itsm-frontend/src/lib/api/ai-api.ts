import { httpClient } from './http-client';
import { security } from '@/lib/security';

export interface TriageResult {
  category: string;
  priority: string;
  assigneeId?: number;
  confidence: number;
  explanation: string;
  urgency?: string;
}

export interface RagAnswer {
  objectType: string;
  id: number;
  title?: string;
  category?: string;
  snippet: string;
  source?: string;
  score?: number;
  // 可信 RAG 可观测字段（后端 L0/L1/L2 已生效，前端透出标签）
  authorityLevel?: number; // 0 普通 / 10 部门推荐 / 20 官方标准 / 30 唯一真相源
  validFrom?: string; // 生效时间 RFC3339，空=立即生效
  validUntil?: string; // 失效时间 RFC3339，空=长期有效
  lastReviewedAt?: string; // 最近复核时间 RFC3339
  reviewIntervalDays?: number; // 复核周期（天）
  isRestricted?: boolean; // 所属分类是否处于受限可见性（分类级守卫 L0）
}

export interface AIFeedbackRequest {
  kind: string;
  query?: string;
  itemType?: string;
  itemId?: number;
  useful: boolean;
  score?: number;
  notes?: string;
}

export interface AIMetrics {
  totalRequests: number;
  totalFeedback: number;
  usefulFeedback: number;
  usefulRate: number;
  byKind: Record<string, number>;
  avgResponseTimeSeconds: number;
  llmCallCount?: number;
  responseTimeAvailable?: boolean;
}

// —— B4-02：Bot 运行维度指标（GET /api/v1/ai/bot-metrics）——

export interface BotMetricsRunStats {
  total: number;
  completed: number;
  failed: number;
  running: number;
  cancelled: number;
  successRate: number;
  avgDurationMs: number;
}

export interface BotMetricsStepStats {
  total: number;
  llmSteps: number;
  toolSteps: number;
  confirmSteps: number;
  avgPerRun: number;
  avgDurationMs: number;
}

export interface BotMetricsToolStats {
  total: number;
  errors: number;
  errorRate: number;
  avgDurationMs: number;
  topTools?: Array<{ key: string; count: number }>;
}

export interface BotMetricsVerifyStats {
  verified: number;
  failed: number;
  skipped: number;
  pending: number;
  failRate: number;
}

export interface BotMetricsConfirmStats {
  approved: number;
  rejected: number;
  expired: number;
  pending: number;
  approvalRate: number;
  rejectRate: number;
  expireRate: number;
  avgDecisionMs?: number;
}

export interface BotMetricsCostStats {
  llmCalls: number;
  toolCalls: number;
  steps: number;
  avgStepsPerRun: number;
  avgToolCallsPerRun: number;
}

export interface BotMetricsBreakdown {
  key: string;
  runs: number;
  failed: number;
  successRate: number;
  avgDurationMs: number;
  toolCalls: number;
  toolErrors: number;
}

export interface BotMetrics {
  windowDays: number;
  since: string;
  generatedAt: string;
  runs: BotMetricsRunStats;
  steps: BotMetricsStepStats;
  tools: BotMetricsToolStats;
  verify: BotMetricsVerifyStats;
  confirmations: BotMetricsConfirmStats;
  cost: BotMetricsCostStats;
  byEntrypoint: BotMetricsBreakdown[];
  byBot: BotMetricsBreakdown[];
  /** token 计量未接线（B1-02 遗留）：成本维度为代理口径。 */
  tokensRecorded: boolean;
  notes?: string[];
}

export interface BotMetricsQuery {
  days?: number;
  botId?: number;
  entrypoint?: string;
}

export interface AIAuditEntry {
  id: number;
  tenantId: number;
  scenario: string;
  prompt: string;
  model: string;
  latencyMs: number;
  totalTokens: number;
  totalCost: number;
  score: number;
  feedback: string;
  createdAt: string;
}

export interface ConversationSummary {
  id: number;
  title: string;
  userId: number;
  tenantId: number;
  createdAt: string;
}

export interface AIMessage {
  id: number;
  conversationId: number;
  role: string;
  content: string;
  requestId?: string;
  createdAt: string;
}

export async function aiTriage(title: string, description: string): Promise<TriageResult> {
  const res = await httpClient.post<{
    title: string;
    description: string;
    suggestions?: {
      category?: string;
      priority?: string;
      confidence?: number;
      reasoning?: string;
      urgency?: string;
    };
  }>(`/api/v1/ai/triage`, { title, description });

  const suggestions = res?.suggestions || {};
  return {
    category: suggestions.category || 'general',
    priority: suggestions.priority || 'medium',
    confidence: typeof suggestions.confidence === 'number' ? suggestions.confidence : 0,
    explanation: suggestions.reasoning || '',
    urgency: suggestions.urgency,
    assigneeId: 0,
  };
}

export interface AIAuditRequest {
  scenario: string;
  inputRef: string;
  promptVersion?: string;
  model?: string;
  confidence?: number;
  suggestion: Record<string, unknown>;
  accepted: boolean;
  notes?: string;
}

/**
 * 上报 AI 建议采纳/拒绝审计（GA AI trace 契约）。
 *
 * 背景（2026-09-08 断链根因）：后端 POST /api/v1/ai/audit 端点已就位，
 * /ai/audit-logs 读 ai_feedbacks 中 item_type='ai_audit' 的记录；但前端
 * 从未调用上报端点，导致审计页恒为空（0 条 vs 44 次 LLM 调用）。
 *
 * 调用约定：fire-and-forget——审计上报失败不得阻塞主交互，
 * 由调用方 catch 后仅记 console（遥测是 best-effort 副作用）。
 */
export async function recordAIAudit(req: AIAuditRequest): Promise<void> {
  await httpClient.post<{ message: string }>(`/api/v1/ai/audit`, req);
}

export async function aiSearchKB(query: string, limit = 5): Promise<{ answers: RagAnswer[] }> {
  // 后端契约：POST /api/v1/ai/rag/search 返回 { results, degraded }（见 handlers/ai KnowledgeSearch）
  const res = await httpClient.post<{ results: RagAnswer[]; degraded?: boolean }>(
    `/api/v1/ai/rag/search`,
    {
      query,
      limit,
      type: 'kb',
    }
  );
  return { answers: Array.isArray(res?.results) ? res.results : [] };
}

export async function aiSimilarIncidents(
  query: string,
  limit = 5
): Promise<{ incidents: RagAnswer[] }> {
  const res = await httpClient.post<{ results: RagAnswer[]; degraded?: boolean }>(
    `/api/v1/ai/rag/search`,
    {
      query,
      limit,
      type: 'incident',
    }
  );
  return { incidents: Array.isArray(res?.results) ? res.results : [] };
}

// ==================== V0-01 工单答复草稿（Inline 优先） ====================
// 后端契约：POST /ai/tickets/:id/reply-draft（只读生成 + KB 引用；采纳/提交由页面完成）。

export interface ReplyDraftCitation {
  title: string;
  source: string;
  snippet?: string;
  score?: number;
}

export interface ReplyDraftResult {
  ticketId: number;
  draft: string;
  citations: ReplyDraftCitation[];
  latencyMs?: number;
  degraded: boolean;
}

/** 生成工单答复草稿（**只读**；不落任何业务字段，绝不自动提交）。 */
export async function aiCreateReplyDraft(
  ticketId: number,
  tone?: string
): Promise<ReplyDraftResult> {
  return httpClient.post<ReplyDraftResult>(
    `/api/v1/ai/tickets/${ticketId}/reply-draft`,
    tone ? { tone } : {}
  );
}

// ==================== V1-01 列表页 AI 批量（只读生成） ====================
// 后端契约：POST /ai/tickets/batch-triage、POST /ai/tickets/batch-reply-drafts。
// 两个端点均只返回建议/草稿文本；采纳与分派由列表页逐行走既有写端点，绝不自动提交。

/** 批量分诊上限（后端 fail-visible：超限 400）。 */
export const BATCH_TRIAGE_MAX_TICKETS = 20;
/** 批量草稿上限（LLM 逐条调用；超限 400）。 */
export const BATCH_REPLY_DRAFT_MAX_TICKETS = 10;

export interface BatchTriageItem {
  ticketId: number;
  title?: string;
  status: 'ok' | 'not_found';
  category?: string;
  priority?: string;
  urgency?: string;
  assigneeId?: number;
  confidence?: number;
  explanation?: string;
}

export interface BatchTriageResult {
  items: BatchTriageItem[];
  processed: number;
  degraded: boolean;
  latencyMs?: number;
}

/** 批量分诊建议（只读；分派/写回仍由用户逐行确认）。 */
export async function aiBatchTriage(ticketIds: number[]): Promise<BatchTriageResult> {
  return httpClient.post<BatchTriageResult>('/api/v1/ai/tickets/batch-triage', { ticketIds });
}

export interface BatchReplyDraftItem {
  ticketId: number;
  status: 'ok' | 'failed' | 'not_found';
  draft?: string;
  citations?: ReplyDraftCitation[];
  latencyMs?: number;
  reason?: string;
}

export interface BatchReplyDraftResult {
  items: BatchReplyDraftItem[];
  succeeded: number;
  degraded: boolean;
  latencyMs?: number;
}

/** 批量答复草稿（只读；草稿不落任何业务字段、不自动评论）。 */
export async function aiBatchReplyDrafts(ticketIds: number[]): Promise<BatchReplyDraftResult> {
  return httpClient.post<BatchReplyDraftResult>('/api/v1/ai/tickets/batch-reply-drafts', {
    ticketIds,
  });
}

// ==================== V1-03 口语↔术语确认（只读理解） ====================
// 后端契约：POST /api/v1/ai/tickets/interpret（handlers/ai/interpret.go）。
// 只把口语映射为可核对的术语（分类/优先级）+ 证据片段 + 置信度；
// 不生成表单、不写业务字段；前端确认/修改后才进入 A2UI 建单，绝不自动提交。

/** 一次术语命中：surface 是命中的口语片段（证据），term 是术语标签（网络故障/高）。 */
export interface TicketIntentTerm {
  field: string;
  surface: string;
  term: string;
  value: string;
  confidence: number;
}

/** 可直接带进建单表单的建议字段（后端已给出 title/description 兜底）。 */
export interface TicketIntentFields {
  title: string;
  type: string;
  priority: string;
  description: string;
}

/** 口语理解结果（matched=false 表示未命中术语，已按 other/medium 默认值降级）。 */
export interface TicketIntentInterpretation {
  summary: string;
  matched: boolean;
  needsConfirmation: boolean;
  confidence: number;
  terms: TicketIntentTerm[];
  fields: TicketIntentFields;
}

/** 口语 → 术语理解（只读；确认/修改交互由「理解确认卡」承载）。 */
export async function aiInterpretTicketIntent(
  utterance: string
): Promise<TicketIntentInterpretation> {
  return httpClient.post<TicketIntentInterpretation>('/api/v1/ai/tickets/interpret', {
    utterance,
  });
}

export async function aiSummarize(text: string, maxLen = 200): Promise<{ summary: string }> {
  const res = await httpClient.post<{ answers: unknown[] }>(`/api/v1/ai/chat`, {
    query: `请在${maxLen}字以内总结以下内容：\n\n${text}`,
    limit: 1,
  });
  const answers = Array.isArray(res?.answers) ? res.answers : [];
  const summary = answers
    .map(a => (typeof a === 'string' ? a : JSON.stringify(a)))
    .join('\n')
    .trim();
  return { summary };
}

export async function aiSaveFeedback(feedback: AIFeedbackRequest): Promise<{ message: string }> {
  return httpClient.post<{ message: string }>(`/api/v1/ai/feedback`, feedback);
}

export async function aiGetMetrics(days = 7): Promise<AIMetrics> {
  return httpClient.get<AIMetrics>(`/api/v1/ai/metrics?days=${days}`);
}

/**
 * Bot 运行维度指标（B4-02）。
 *
 * 后端契约：`GET /api/v1/ai/bot-metrics?days=&botId=&entrypoint=`；
 * `bot.enabled=false` 时后端返回 503（调用方据此隐藏/置灰看板）。
 */
export async function aiGetBotMetrics(params: BotMetricsQuery = {}): Promise<BotMetrics> {
  const search = new URLSearchParams();
  if (params.days && params.days > 0) search.set('days', String(params.days));
  if (params.botId && params.botId > 0) search.set('botId', String(params.botId));
  if (params.entrypoint) search.set('entrypoint', params.entrypoint);
  const qs = search.toString();
  // 注意：不要在此处写嵌套模板字面量（`...${qs ? `?${qs}` : ''}`）——api-contract 扫描器
  // 会把内层反引号当作字符串终点，将路径误报为「真实契约漂移」（见 api-contract.test.ts 提取规则）。
  if (qs) {
    return httpClient.get<BotMetrics>(`/api/v1/ai/bot-metrics?${qs}`);
  }
  return httpClient.get<BotMetrics>('/api/v1/ai/bot-metrics');
}

// ==================== H0-03/H0-04 帮助文档数据面 ====================
// 后端契约：GET /api/v1/ai/help/search?q=&pageKey=&limit=、GET /api/v1/ai/help/page?pageKey=
// （handlers/ai/help.go；内嵌索引来自 docs/help 真源）。

export interface HelpDoc {
  path: string;
  title: string;
  pageKeys: string[];
  text: string;
}

export interface HelpSearchResult {
  path: string;
  title: string;
  pageKeys: string[];
  snippet: string;
  score: number;
}

export type HelpPageStatus = 'ok' | 'missing' | 'stale' | 'unknown';

/**
 * P2：服务商（MSP）分发的帮助文档（copy-in 快照）。
 * status=active 跟随 provider 更新；unbound=已解绑，转客户原生副本（内容保留）。
 */
export interface HelpDistributedDoc {
  title: string;
  pageKey: string;
  text: string;
  source: 'msp_provider';
  providerTenantId: number;
  sourceSlug: string;
  sourceVersion: number;
  status: 'active' | 'unbound';
}

/** P2：服务商分发帮助的检索结果（字段集与内嵌结果不同：无 path/score，带来源追踪）。 */
export interface HelpDistributedSearchResult {
  title: string;
  pageKey: string;
  snippet: string;
  text?: string;
  source: 'msp_provider';
  providerTenantId: number;
  sourceSlug: string;
  sourceVersion: number;
  status: 'active' | 'unbound';
}

export interface HelpPageResult {
  pageKey: string;
  status: HelpPageStatus;
  title: string;
  pattern: string;
  entrypoint: string;
  docs: HelpDoc[];
  /** P2：服务商分发帮助（可能为空；面板按来源标注渲染）。 */
  distributedDocs?: HelpDistributedDoc[];
  coverage: { coverage: number; totals: Record<string, number> };
}

export async function aiGetPageHelp(pageKey: string): Promise<HelpPageResult> {
  const qs = `?pageKey=${encodeURIComponent(pageKey)}`;
  return httpClient.get<HelpPageResult>(`/api/v1/ai/help/page${qs}`);
}

export async function aiSearchHelp(
  query: string,
  pageKey?: string,
  limit = 5
): Promise<{ results: HelpSearchResult[]; distributedResults?: HelpDistributedSearchResult[] }> {
  const search = new URLSearchParams();
  search.set('q', query);
  if (pageKey) search.set('pageKey', pageKey);
  if (limit > 0) search.set('limit', String(limit));
  const qs = search.toString();
  return httpClient.get<{
    results: HelpSearchResult[];
    distributedResults?: HelpDistributedSearchResult[];
  }>(`/api/v1/ai/help/search?${qs}`);
}

// ==================== H1-01/H1-04 帮助闭环数据面 ====================
// 后端契约：GET /ai/help/todos（遥测缺口待办队列，H1-01）；GET /ai/help/coverage（覆盖率看板，H1-04）。

// H1-01/H1-03/H1-04 条件项：待办运营状态（open=待处理缺省；claimed=已认领；ignored=已忽略）。
export type HelpTodoState = 'open' | 'claimed' | 'ignored';

export interface HelpTodoItem {
  pageKey: string;
  title?: string;
  docStatus: 'missing' | 'stale' | 'ok' | 'unknown';
  gapCount: number;
  draftCount: number;
  lastSeen?: string;
  reason: 'doc_missing' | 'doc_stale' | 'page_unregistered' | 'user_help_gaps';
  state?: HelpTodoState;
  stateUpdatedBy?: number;
  stateUpdatedAt?: string;
}

export interface HelpTodosResult {
  days: number;
  items: HelpTodoItem[];
  totals: { items: number; missing: number; stale: number; claimed?: number; ignored?: number };
  /** H1-04 条件项：false = 状态服务未装配/降级（状态按 open 缺省渲染，写面不可用）。 */
  states?: { available: boolean };
  telemetry: { available: boolean };
}

export async function aiGetHelpTodos(days = 30, limit = 50): Promise<HelpTodosResult> {
  const search = new URLSearchParams();
  search.set('days', String(days));
  search.set('limit', String(limit));
  return httpClient.get<HelpTodosResult>(`/api/v1/ai/help/todos?${search.toString()}`);
}

export interface HelpTodoStateResult {
  pageKey: string;
  state: HelpTodoState;
  updatedBy?: number;
  updatedAt?: string;
}

// 待办认领/忽略/恢复（claimed|ignored|open；幂等；ai:write 门控由后端执行）。
export async function aiSetHelpTodoState(
  pageKey: string,
  state: HelpTodoState
): Promise<HelpTodoStateResult> {
  return httpClient.put<HelpTodoStateResult>('/api/v1/ai/help/todos/state', { pageKey, state });
}

export interface HelpCoveragePage {
  pageKey: string;
  title: string;
  pattern: string;
  entrypoint: string;
  status: 'ok' | 'missing' | 'stale';
  docs: string[];
}

export interface HelpCoverageResult {
  generatedAt: string;
  coverage: number;
  totals: { pages: number; ok: number; missing: number; stale: number; orphan: number };
  pages: HelpCoveragePage[];
  orphanDocs: Array<{ doc: string; unknownKeys: string[] }>;
}

export async function aiGetHelpCoverage(): Promise<HelpCoverageResult> {
  return httpClient.get<HelpCoverageResult>('/api/v1/ai/help/coverage');
}

// ==================== D1-01 页面助手上下文（服务端建议过滤） ====================
// 后端契约：GET /api/v1/ai/assistant/context?pageKey=&route=（handlers/ai/assistant_context.go）。
// 前端策略：建议卡优先消费服务端过滤结果（可见 Bot ∩ 工具授权 ∩ RBAC）；
// 请求失败 / known=false → 回退 D0-03/D0-05 静态注册表基线（零延迟基线先行渲染，不阻塞交互）。

export interface AssistantContextSuggestion {
  id: string;
  kind: 'task' | 'question';
  label: string;
  prompt: string;
  readOnly: boolean;
  /** 注册表序号（服务端过滤后仍保留原始顺序）。 */
  rank: number;
  /** O2-01：规则打分（ranking=weighted 时携带）。 */
  score?: number;
  /** O2-01：反馈（原始计数 + 半衰期衰减有效计数）。 */
  feedback?: AssistantContextFeedback;
}

/** 建议反馈计数（O2-01：decayed* 为半衰期衰减后的排序口径，旧后端可能缺省）。 */
export interface AssistantContextFeedback {
  shown: number;
  clicked: number;
  ignored: number;
  decayedShown?: number;
  decayedClicked?: number;
  decayedIgnored?: number;
}

export interface AssistantContextScene {
  slug?: string;
  botId?: number;
  status?: string;
  /** 场景建议是否放行；false = 该页场景建议全部被过滤。 */
  eligible?: boolean;
  /** 判定原因码（template_missing / status_draft / audience_denied / entrypoint_denied / policy_disabled …）。 */
  reason?: string;
}

export interface AssistantContextResult {
  pageKey: string;
  route?: string;
  known: boolean;
  title?: string;
  entrypoint?: string;
  suggestions: AssistantContextSuggestion[];
  filtered?: Array<{ id: string; reason: string }>;
  /** enabled=服务端过滤生效；disabled=策略层未装配（兼容态）；none=无场景声明。 */
  filtering?: 'enabled' | 'disabled' | 'none';
  scene?: AssistantContextScene | null;
  /** O2-01：weighted=按反馈重排；registry=注册表顺序（埋点未装配/无反馈）。 */
  ranking?: 'weighted' | 'registry';
}

export async function aiGetAssistantContext(
  pageKey: string,
  route?: string
): Promise<AssistantContextResult> {
  const search = new URLSearchParams();
  search.set('pageKey', pageKey);
  if (route) search.set('route', route);
  return httpClient.get<AssistantContextResult>(`/api/v1/ai/assistant/context?${search.toString()}`);
}

// ==================== AI Copilot 观测看板（O0-03） ====================
// 后端契约：GET /api/v1/ai/telemetry/summary?days=&pageKey=&role=（handlers/ai/telemetry.go）。
// 口径：采用 / 效果 / 质量 / 成本与时延四层；过滤字段留空即不参与条件（零行为变化）。

export interface AssistantTelemetryBucket {
  key: string;
  count: number;
}

export interface AssistantTelemetryLatency {
  avg: number;
  p95: number;
  max: number;
}

export interface AssistantTelemetrySummary {
  days: number;
  totalEvents: number;
  distinctSessions: number;
  errorEvents: number;
  byName: AssistantTelemetryBucket[];
  byPage: AssistantTelemetryBucket[];
  latencyMs?: AssistantTelemetryLatency;
  generatedAt: string;
}

export interface AssistantTelemetryQuery {
  days?: number;
  pageKey?: string;
  role?: string;
}

export async function aiGetAssistantTelemetrySummary(
  params: AssistantTelemetryQuery = {}
): Promise<AssistantTelemetrySummary> {
  const search = new URLSearchParams();
  if (params.days && params.days > 0) search.set('days', String(params.days));
  if (params.pageKey) search.set('pageKey', params.pageKey);
  if (params.role) search.set('role', params.role);
  const qs = search.toString();
  // 同 aiGetBotMetrics：避免嵌套模板字面量（api-contract 扫描器提取规则）。
  if (qs) {
    return httpClient.get<AssistantTelemetrySummary>(`/api/v1/ai/telemetry/summary?${qs}`);
  }
  return httpClient.get<AssistantTelemetrySummary>('/api/v1/ai/telemetry/summary');
}

// ==================== AI 评估与审计（AI-Native：可观测、可回测） ====================
// 后端契约：GET /api/v1/ai/evaluation、GET /api/v1/ai/audit-logs（见 handlers/ai GetEvaluation/GetAuditLogs）

export interface AICalibrationBucket {
  bucket: string;
  midpoint: number;
  count: number;
  usefulRate: number;
  calibrationError: number;
}

export interface AIScenarioEval {
  kind: string;
  count: number;
  usefulRate: number;
  acceptedRate: number;
  avgConfidence: number;
}

export interface AIEvaluationReport {
  generatedAt: string;
  lookbackDays: number;
  totalFeedback: number;
  usefulRate: number;
  acceptedRate: number;
  avgConfidence: number;
  healthScore: number;
  hasData: boolean;
  byScenario: AIScenarioEval[];
  confidenceCalibration: AICalibrationBucket[];
  platform: {
    llmCallCount: number;
    successRate: number;
    avgLatencyMs: number;
  };
}

export interface AIAuditEntry {
  id: number;
  createdAt: string;
  tenantId: number;
  userId: number;
  requestId: string;
  scenario: string;
  inputRef: string;
  promptVersion: string;
  model: string;
  confidence: number;
  accepted: boolean;
  suggestion: Record<string, unknown> | null;
  notes: string;
}

export interface AIAuditLogsResponse {
  items: AIAuditEntry[];
  total: number;
  page: number;
  pageSize: number;
}

export async function aiGetEvaluation(days = 30): Promise<AIEvaluationReport> {
  return httpClient.get<AIEvaluationReport>(`/api/v1/ai/evaluation?days=${days}`);
}

export async function aiGetAuditLogs(params: {
  page?: number;
  pageSize?: number;
  kind?: string;
  days?: number;
} = {}): Promise<AIAuditLogsResponse> {
  const query = new URLSearchParams();
  if (params.page) query.set('page', String(params.page));
  if (params.pageSize) query.set('pageSize', String(params.pageSize));
  if (params.kind) query.set('kind', params.kind);
  if (params.days) query.set('days', String(params.days));
  const qs = query.toString();
  // 字面量路径 + 拼接，避免模板内三元表达式，保证 api-contract 测试可静态解析路径
  const url = '/api/v1/ai/audit-logs' + (qs ? `?${qs}` : '');
  return httpClient.get<AIAuditLogsResponse>(url);
}

// ==================== AI 趋势预测（对应后端 POST /api/v1/ai/predictions） ====================

/** 后端 DTO：itsm-backend/dto/ticket_prediction_dto.go */
export type TrendPredictionType = 'volume' | 'type' | 'priority' | 'resource';

export interface TrendPredictionRequest {
  timeRange: [string, string]; // YYYY-MM-DD ×2
  predictionType: TrendPredictionType;
  filters?: Record<string, unknown>;
  model?: string;
}

export interface PredictionDataPoint {
  date: string;
  predictedValue: number;
  lowerBound: number;
  upperBound: number;
  confidence: number;
  category?: string;
  priority?: string;
}

export interface TrendPredictionResponse {
  predictions: PredictionDataPoint[];
  confidence: number;
  model: string;
  generatedAt: string;
}

export async function aiPredict(req: TrendPredictionRequest): Promise<TrendPredictionResponse> {
  return httpClient.post<TrendPredictionResponse>('/api/v1/ai/predictions', req);
}

// ==================== AI 工具调用审批队列（P0：打通人工审批闭环） ====================

export interface ToolApproval {
  id: number;
  toolName: string;
  /**
   * 脱敏参数（JSON 字符串）。
   *
   * M1-02 起列表接口**不再返回原始 `arguments`**（原始参数只作为执行真源留在后端），
   * 展示与审批一律以本字段为准，避免口令/token 出现在页面与浏览器网络面板。
   */
  argsRedacted: string;
  status: string;
  needsApproval: boolean;
  approvalState: string; // pending | approved | rejected | auto
  approvalReason?: string;
  permissionCheck?: string;
  permissionReason?: string;
  createdAt: string;
  conversationId: number;
  userId: number;
  /**
   * B1-01 运行归属（后端 `runId` 原样返回；0/缺省 = 纯问答无运行）。
   * 审计页据此提供「查看轨迹」行内跳转（有 run 优先 run 维度，否则会话维度）。
   */
  runId?: number;
  /**
   * 来源与风险（M1-02 后端契约）：
   * `provider` = builtin | mcp；`serverName/rawToolName/callableName` 为 MCP 三元组
   * （内置工具为空）；`risk` 由工具面实时解析（服务器不可达时为空串）。
   */
  provider?: string;
  serverName?: string;
  rawToolName?: string;
  callableName?: string;
  risk?: string;
  roleSnapshot?: string;
  approvedBy?: number;
  approvedAt?: string | null;
  durationMs?: number;
  errorCode?: string;
  outputSummary?: string;
  /**
   * B1-05 后端附加字段：
   * `confirmationState` = pending|confirmed|rejected|expired|cancelled|unknown（规范化状态）；
   * `expiresAt` = 确认单有效期（未配置 TTL 时为 null）。
   * 旧后端不返回时二者缺省，前端按 `approvalState` 降级推导。
   */
  confirmationState?: string;
  expiresAt?: string | null;
  /**
   * 目标对象与支撑引用（M0-03/B0-02 联合字段）：由后端详情/列表原样返回。
   * SSE 过程事件不带这些字段，前端只在拿到详情后展示（不臆造）。
   */
  targetType?: string;
  targetId?: string;
  supportRef?: string;
  /** B1-06/B0-04 队列与预览字段（后端 `handlers/ai/entity.go` 原样返回）。 */
  dryRun?: boolean;
  verifyState?: string;
  verifyNote?: string;
  attemptCount?: number;
  lastErrorCode?: string;
}

export interface ToolApprovalListResponse {
  items: ToolApproval[];
  state: string;
  /** 生效的来源过滤（M1-06；空串=未过滤），用于筛选回显。 */
  provider?: string;
  /** 生效的服务器过滤（M1-06；空串=未过滤）。 */
  server?: string;
}

/** 审批/审计列表的来源维度过滤（M1-06）：过滤在后端数据库层完成。 */
export interface ToolApprovalListFilter {
  /** builtin | mcp；空/缺省=全部。 */
  provider?: string;
  /** MCP 服务器标识；空/缺省=全部。 */
  server?: string;
  /** I2-01 监督面：按运行收敛（只看某次运行的待审批）；0/缺省=不过滤。 */
  runId?: number;
}

export interface ToolApproveRequest {
  approve: boolean;
  reason?: string;
}

// 列出 AI 工具调用审批记录（默认待审批）
// 对应后端 GET /api/v1/agent/tools/invocations?state=pending
export async function aiGetToolApprovals(
  state = 'pending',
  filter: ToolApprovalListFilter = {}
): Promise<ToolApprovalListResponse> {
  // 字面量路径 + 拼接，避免模板内三元表达式，保证 api-contract 测试可静态解析路径
  const parts = ['state=' + encodeURIComponent(state)];
  if (filter.provider) parts.push('provider=' + encodeURIComponent(filter.provider));
  if (filter.server) parts.push('server=' + encodeURIComponent(filter.server));
  if (filter.runId && filter.runId > 0) parts.push('runId=' + encodeURIComponent(String(filter.runId)));
  const url = '/api/v1/agent/tools/invocations?' + parts.join('&');
  return httpClient.get<ToolApprovalListResponse>(url);
}

/**
 * 工具调用记录详情（M1-05：对话内待审批卡片按需拉取一次，不轮询）。
 *
 * 字段与审批列表同口径（后端 `toolInvocationItem` 唯一装配）：
 * `provider/serverName/rawToolName/callableName/risk/argsRedacted/...`；**不含原始参数**。
 */
/** 详情接口在列表字段之上多返回执行结果与错误（同样不含原始参数）。 */
export interface ToolInvocationDetail extends ToolApproval {
  result?: string | null;
  error?: string | null;
  requestId?: string;
}

// 查询单条工具调用记录
// 对应后端 GET /api/v1/agent/tools/:id
export async function aiGetToolInvocation(id: number): Promise<ToolInvocationDetail> {
  // 与相邻包装同一写法（字面量拼接），便于 api-contract 测试静态解析路径。
  const url = '/api/v1/agent/tools/' + encodeURIComponent(String(id));
  return httpClient.get<ToolInvocationDetail>(url);
}

// 通过 / 驳回某条工具调用审批
// 对应后端 POST /api/v1/agent/tools/:id/approve
export async function aiApproveTool(
  id: number,
  req: ToolApproveRequest
): Promise<{ invocationId: number; approvalState: string }> {
  return httpClient.post(`/api/v1/agent/tools/${id}/approve`, req);
}

// ==================== V0-03 工具直执行（Inline 任务卡；plan/只读） ====================
// 后端契约：POST /api/v1/agent/tools/execute（handlers/ai ExecuteTool；写工具会返回 pending 审批，
// 本入口不改变该语义——Inline 面板只用于 plan/只读工具，如 analyze_ci_impact_plan）。

export interface ToolExecuteResult {
  status: string;
  summary?: string;
  nextActions?: string[];
  artifacts?: string[];
  /** 工具输出（plan/analysis 类含 artifactId/kind/title/content/evidence）。 */
  data?: Record<string, unknown> | null;
}

export async function aiExecuteTool(
  name: string,
  args: Record<string, unknown>,
  opts?: { conversationId?: number; dryRun?: boolean }
): Promise<ToolExecuteResult> {
  return httpClient.post<ToolExecuteResult>('/api/v1/agent/tools/execute', {
    name,
    args,
    ...(opts?.conversationId ? { conversationId: opts.conversationId } : {}),
    ...(opts?.dryRun ? { dryRun: true } : {}),
  });
}

// ==================== 合并自 legacy AIService（src/lib/services/ai-service.ts） ====================
// 语义与旧实现保持一致：aiClassifyTicket → /ai/triage；aiSuggestSolutions → /ai/rag/search(kb)；
// aiIntelligentSearch → /global-search。

export interface TicketAnalysisRequest {
  title: string;
  description: string;
  attachments?: string[];
  userContext?: {
    department: string;
    role: string;
    location: string;
  };
}

export interface TicketClassificationResult {
  category: string;
  subcategory?: string;
  priority: 'low' | 'medium' | 'high' | 'critical';
  urgency: 'low' | 'medium' | 'high' | 'critical' | string;
  confidence: number;
  reasoning: string;
  suggestions?: string[];
}

export interface SolutionSuggestion {
  solutionId: string;
  title: string;
  description: string;
  steps?: string[];
  estimatedTime?: number; // 预计解决时间（分钟）
  successRate?: number; // 历史成功率
  relatedKnowledge?: string[];
  confidence: number;
  reasoning: string;
}

export interface SolutionSearchRequest {
  query: string;
  category?: string;
  priority?: string;
  context?: string;
  limit?: number;
}

export interface SearchResult {
  id: number;
  title: string;
  description: string;
  type: string;
  status?: string;
  number?: string;
}

export interface IntelligentSearchFilters {
  type?: 'tickets' | 'knowledge' | 'incidents' | 'all';
  dateRange?: { start: string; end: string };
  category?: string;
}

export async function aiClassifyTicket(
  request: TicketAnalysisRequest
): Promise<TicketClassificationResult> {
  const res = await httpClient.post<{
    title: string;
    description: string;
    suggestions?: {
      category?: string;
      priority?: string;
      confidence?: number;
      reasoning?: string;
      urgency?: string;
    };
  }>(`/api/v1/ai/triage`, {
    title: request.title,
    description: request.description,
  });

  const s = res?.suggestions || {};
  const priority = (s.priority || 'medium') as TicketClassificationResult['priority'];
  const urgency = (s.urgency || priority) as TicketClassificationResult['urgency'];
  return {
    category: s.category || 'general',
    priority,
    urgency,
    confidence: typeof s.confidence === 'number' ? s.confidence : 0,
    reasoning: s.reasoning || '',
    suggestions: [],
  };
}

export async function aiSuggestSolutions(
  request: SolutionSearchRequest
): Promise<SolutionSuggestion[]> {
  const limit = request.limit && request.limit > 0 ? request.limit : 5;
  const res = await httpClient.post<{ results: any[]; degraded?: boolean }>(
    `/api/v1/ai/rag/search`,
    {
      query: request.query,
      limit,
      type: 'kb',
    }
  );

  const list = Array.isArray(res?.results) ? res.results : [];
  return list.map((item, idx) => {
    const title = String(item?.title || item?.source || `知识项 ${idx + 1}`);
    const snippet = String(item?.snippet || item?.content || item?.text || '');
    const score = typeof item?.score === 'number' ? item.score : undefined;
    return {
      solutionId: String(item?.id ?? `${Date.now()}_${idx}`),
      title,
      description: snippet,
      steps: [],
      relatedKnowledge: item?.source ? [String(item.source)] : [],
      confidence: typeof score === 'number' ? score : 0,
      reasoning: '来自知识库检索结果',
    };
  });
}

export async function aiIntelligentSearch(
  query: string,
  _filters?: IntelligentSearchFilters
): Promise<{
  tickets: SearchResult[];
  knowledge: SearchResult[];
  incidents: SearchResult[];
  suggestions: string[];
}> {
  const res = await httpClient.get<{ results: Array<any>; total: number }>(
    `/api/v1/global-search?q=${encodeURIComponent(query)}`
  );
  const results = Array.isArray(res?.results) ? res.results : [];

  const normalize = (r: any): SearchResult => ({
    id: Number(r?.id || 0),
    type: String(r?.type || ''),
    title: String(r?.title || ''),
    description: String(r?.description || ''),
    status: r?.status ? String(r.status) : undefined,
    number: r?.ticketNumber ? String(r.ticketNumber) : r?.number ? String(r.number) : undefined,
  });

  return {
    tickets: results.filter(r => r?.type === 'ticket').map(normalize),
    knowledge: results.filter(r => r?.type === 'knowledge').map(normalize),
    incidents: results.filter(r => r?.type === 'incident').map(normalize),
    suggestions: [],
  };
}

// ==================== SSE Streaming Chat ====================

/**
 * O2-02/§13.4 + B4-02：一次对话回合的 token 用量（后端 v2 `usage` 事件）。
 *
 * 口径（按 estimated 区分）：
 * - true/缺省：字符数 ÷ 4，与后端限流门禁同一算法——展示必须保留「估算」字样；
 * - false：provider 回包实测（B4-02；协议 usage 末帧/非流式响应），展示标注「provider 实测」；
 * 两者均**不是**账单口径（缓存命中/推理 token 等细分与计价映射不在范围内）。
 */
export interface AIUsageEstimate {
  /** 本回合输入估算（含系统提示与历史上下文；工具循环多次调用累加）。 */
  tokenIn: number;
  /** 本回合输出估算（流式正文累计）。 */
  tokenOut: number;
  /** 口径标记：true/缺省=字符/4 估算；false=provider 回包实测（B4-02）。 */
  estimated?: boolean;
  /** 计量范围：turn = 本回合。 */
  scope?: string;
}

/** SSE `done` 事件回带的生效实例信息（BE-7；开关关闭时两端都缺省 → 与现状一致）。 */
export interface AIChatDoneInfo {
  provider?: string;
  providerSource?: string;
  /** v2 `usage` 事件（恒在 done 前到达；旧后端缺省 → undefined，调用方按无数据处理）。 */
  usage?: AIUsageEstimate;
}

/** Server-Sent Event payload types emitted by /ai/chat/stream. */
export type AIChatStreamEvent =
  | { type: 'sources'; sources: RagAnswer[] }
  | { type: 'delta'; content: string }
  | { type: 'tool_call_started'; event: AIToolStreamEvent }
  | { type: 'tool_call_finished'; event: AIToolStreamEvent }
  | { type: 'tool_call_failed'; event: AIToolStreamEvent }
  | { type: 'approval_pending'; event: AIToolStreamEvent }
  | { type: 'confirmation_required'; event: AIToolStreamEvent }
  | { type: 'run_started'; run: AIRunStartedEvent }
  | { type: 'step'; step: AIRunStepEvent }
  | { type: 'usage'; usage: AIUsageEstimate }
  | { type: 'done'; conversationId: number; provider?: string; providerSource?: string; usage?: AIUsageEstimate }
  | { type: 'error'; message: string };

/**
 * v2 运行事件（B1-03 注册表；对象载荷带 `v:2`）。
 *
 * 语义：一次对话回合（run）开始。旧后端/开关关闭时不出现——调用方不得依赖其存在。
 */
export interface AIRunStartedEvent {
  v?: number;
  runId: number;
  entrypoint?: string;
  conversationId?: number;
}

/**
 * v2 步骤事件（B1-03 注册表）：llm / tool 等步骤**落库成功后**广播。
 *
 * 只承载索引与元数据，业务载荷经 `payloadRef` 引用（不在此展开）。
 */
export interface AIRunStepEvent {
  v?: number;
  runId: number;
  stepIndex: number;
  type: string;
  payloadRef?: string;
  durationMs?: number;
}

/**
 * 对话内工具事件（M1-03 后端契约）。
 *
 * 语义：**过程可见**，不是消息本身——最终答案仍由 `delta/done` 承载；
 * 事件缺失时调用方按最终消息降级渲染，不应出现空白块。
 */
export interface AIToolStreamEvent {
  /** tool_invocation 主键（仅写路径/已提交审批时有值）。 */
  id?: number;
  /** 投影后的可调用名（MCP 为 mcp__<server>__<tool>）。 */
  tool: string;
  /** 来源：builtin | mcp（未知来源按 builtin 兜底渲染）。 */
  provider: string;
  /** MCP 服务器标识（内置工具缺省）。 */
  server?: string;
  /** 调用性质：read | write。 */
  phase: string;
  /** 状态：started | done | failed | pending。 */
  status: string;
  /** 脱敏 + 截断的结果摘要（不含原始参数）。 */
  summary?: string;
  /** 读路径执行耗时（毫秒；写路径提交阶段缺省）。 */
  durationMs?: number;
  /** 失败时的稳定错误码（tool_permission_denied / unknown_tool / ...）。 */
  errorCode?: string;
  /**
   * I0-04 Outcome Contract：产物类工具（plan/analysis/draft）成功时回带的产物 id（可选）。
   * 旧后端不产生该字段 → 前端回执卡按「无产物落点」降级渲染。
   */
  artifactId?: number;
}

/** 工具事件回调（四种事件共用；status 区分阶段）。 */
export type AIToolStreamCallback = (event: AIToolStreamEvent) => void;

export interface AIChatStreamCallbacks {
  onSources?: (sources: RagAnswer[]) => void;
  onDelta?: (delta: string) => void;
  /**
   * 工具调用过程事件（M1-03）。旧调用方不传即可——新增事件对既有渲染零影响。
   */
  onToolEvent?: AIToolStreamCallback;
  /** v2 运行开始（B1-03）；不传即忽略。 */
  onRunStarted?: (run: AIRunStartedEvent) => void;
  /** v2 步骤进度（B1-03）；不传即忽略。 */
  onStep?: (step: AIRunStepEvent) => void;
  /** v2 回合用量估算（O2-02/§13.4）；不传即忽略（仅用于展示/埋点，不影响渲染）。 */
  onUsage?: (usage: AIUsageEstimate) => void;
  /**
   * 未知事件上报（可选）：默认静默忽略以保证前向兼容；
   * 传入时仅用于日志/埋点，**不得**据此中断流或渲染。
   */
  onUnknownEvent?: (event: string, data: unknown) => void;
  onDone?: (conversationId: number, info?: AIChatDoneInfo) => void;
  onError?: (message: string) => void;
}

// —— B2-04 工作区 Bot 选择器 ——

/** 选择器可见的 Bot（后端 GET /api/v1/agent/bots 已按角色 audience 过滤，仅最小字段）。 */
export interface BotOption {
  id: number;
  slug: string;
  name: string;
  audience: string;
}

/**
 * 拉取当前用户可见的 Bot 列表（工作区选择器）。
 *
 * 兼容默认：端点未注册（bot.enabled=false → 404）或请求失败时返回空数组——
 * 选择器不渲染，聊天链路与引入该能力前完全一致（不因治理面缺失而阻断对话）。
 */
export async function aiListVisibleBots(): Promise<BotOption[]> {
  const res = await aiListVisibleBotsWithCapabilities();
  return res.items;
}

/**
 * 展示用能力块（`GET /api/v1/agent/bots` 随列表下发，camelCase）。
 *
 * `botEnabled=false` 时后端返回空列表 + 该块；对话页据此禁用/隐藏选择器并提示，
 * **不新增任何请求**。
 */
export interface AgentBotsCapabilities {
  botEnabled: boolean;
  mcpWriteEnabled: boolean;
}

export interface AgentBotsResult {
  items: BotOption[];
  /** 旧后端未下发时为 null（页面按"未管控"降级）。 */
  capabilities: AgentBotsCapabilities | null;
}

/**
 * 同 `aiListVisibleBots`，但额外返回展示用 capabilities 块（一次请求，供对话页复用）。
 *
 * 失败兜底：任何异常都返回空列表 + null capabilities，绝不阻断聊天链路。
 */
export async function aiListVisibleBotsWithCapabilities(): Promise<AgentBotsResult> {
  try {
    const res = (await httpClient.get('/api/v1/agent/bots')) as
      | { items?: BotOption[]; capabilities?: AgentBotsCapabilities }
      | undefined;
    const items = Array.isArray(res?.items) ? res.items : [];
    const capabilities =
      res?.capabilities && typeof res.capabilities.botEnabled === 'boolean'
        ? {
            botEnabled: res.capabilities.botEnabled,
            mcpWriteEnabled: Boolean(res.capabilities.mcpWriteEnabled),
          }
        : null;
    return {
      items: items
        .filter(
          (item: unknown): item is BotOption =>
            Boolean(item) &&
            typeof (item as BotOption).id === 'number' &&
            typeof (item as BotOption).name === 'string'
        )
        .map((item: BotOption) => ({
          id: item.id,
          slug: item.slug ?? '',
          name: item.name,
          audience: item.audience ?? '',
        })),
      capabilities,
    };
  } catch {
    return { items: [], capabilities: null };
  }
}

export interface AIChatStreamRequest {
  query: string;
  conversationId?: number;
  limit?: number;
  /** 多 Provider 灰度（BE-7）：显式覆盖实例 key；缺省 = 与现状完全一致（不落该字段）。 */
  provider?: string;
  /**
   * B2-04 工作区选择器：新建会话归属的 Bot 模板 id；缺省/0 = 内置默认助手。
   * 仅对**新建会话**生效（已有 conversationId 时后端按会话绑定归属，忽略该字段）。
   */
  botId?: number;
  /**
   * B3-01/B3-02 入口上下文（页面 launcher 携带；缺省 = chat/无目标，请求体与现状一致）。
   * 服务端会再次解析并做目标对象预检（前端值只是提示）。
   */
  entrypoint?: string;
  targetType?: string;
  targetId?: number;
  summary?: string;
  /**
   * D0-04 页面上下文提示（pageKey/route；仅提示/审计，不参与授权）。
   * 缺省不落字段——请求体与改动前逐字节一致（QA-3 门禁）。
   */
  pageContext?: AIPageContextHint;
  signal?: AbortSignal;
}

/** D0-04 页面上下文提示（与后端 handlers/ai/page_context.go 的 PageContextHint 对齐）。 */
export interface AIPageContextHint {
  pageKey: string;
  route?: string;
}

/**
 * Stream a RAG-backed answer over SSE. Falls back to the plain /ai/chat
 * endpoint if streaming is unsupported by the environment (e.g. legacy
 * browsers without ReadableStream). Returns the final conversationId once the
 * stream finishes.
 */
export async function aiChatStream(
  req: AIChatStreamRequest,
  callbacks: AIChatStreamCallbacks = {}
): Promise<number> {
  const baseUrl = httpClient.getBaseURL();
  const token = httpClient.getAuthToken();
  const tenantId = httpClient.getTenantId();

  // 优先走前端同源代理，避开直连后端的 CORS/CSRF 问题。
  const candidates: Array<{ url: string; useCredentials: boolean; appendCSRF: boolean }> = [
    { url: '/api/v1/ai/chat/stream', useCredentials: true, appendCSRF: true },
  ];
  if (baseUrl && !baseUrl.startsWith('/')) {
    candidates.push({
      url: `${baseUrl}/api/v1/ai/chat/stream`,
      useCredentials: true,
      appendCSRF: true,
    });
  }

  // 获取 CSRF token。后端的 csrf cookie 是 HttpOnly，document.cookie 读不到，
  // 必须与 httpClient 一致走 security 抽象（GET /api/v1/csrf-token）。
  const getCsrfToken = async (): Promise<string | null> => {
    try {
      return await security.csrf.getToken();
    } catch {
      return null;
    }
  };
  // 后端会在每次写请求成功后轮换 CSRF token，缓存值可能已过期；
  // 403 时丢弃缓存重新获取。
  const refreshCsrfToken = async (): Promise<string | null> => {
    security.csrf.clearToken();
    return getCsrfToken();
  };

  const csrfToken = await getCsrfToken();

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    Accept: 'text/event-stream',
  };
  if (token) headers.Authorization = `Bearer ${token}`;
  if (tenantId) headers['X-Tenant-ID'] = String(tenantId);
  if (csrfToken) headers['X-CSRF-Token'] = csrfToken;

  const body = JSON.stringify({
    query: req.query,
    limit: req.limit,
    conversationId: req.conversationId,
    // 未显式选择时不写 provider：请求体与现状逐字节一致（QA-3 门禁）。
    ...(req.provider ? { provider: req.provider } : {}),
    // 同上：未选择 Bot 时不落 botId（默认助手，请求体与现状一致）。
    ...(req.botId ? { botId: req.botId } : {}),
    // B3-02：仅携带入口上下文时落字段（缺省请求体与现状一致）。
    ...(req.entrypoint ? { entrypoint: req.entrypoint } : {}),
    ...(req.targetType && req.targetId ? { targetType: req.targetType, targetId: req.targetId } : {}),
    ...(req.summary ? { summary: req.summary } : {}),
    // D0-04：仅携带页面上下文提示时落字段（缺省请求体与现状逐字节一致）。
    ...(req.pageContext ? { pageContext: req.pageContext } : {}),
  });

  let lastError: Error | null = null;
  for (const candidate of candidates) {
    const controller = new AbortController();
    if (req.signal) {
      if (req.signal.aborted) {
        controller.abort();
      } else {
        req.signal.addEventListener('abort', () => controller.abort(), { once: true });
      }
    }

    try {
      const doFetch = () =>
        fetch(candidate.url, {
          method: 'POST',
          headers,
          credentials: candidate.useCredentials ? 'include' : 'omit',
          signal: controller.signal,
          body,
        });

      let response = await doFetch();

      // CSRF token 可能已被后端轮换导致 403，取新 token 原地重试一次。
      if (response.status === 403) {
        const freshToken = await refreshCsrfToken();
        if (freshToken) {
          headers['X-CSRF-Token'] = freshToken;
          response = await doFetch();
        }
      }

      if (!response.ok || !response.body) {
        // 4xx/5xx 走 fallback 而非抛错
        lastError = new Error(`AI chat stream failed: HTTP ${response.status}`);
        continue;
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let buffer = '';
      let finalConversationId = 0;
      // O2-02/§13.4：`usage` 事件恒在 done 前到达 → 先缓存，随 done 一并交给调用方。
      let finalUsage: AIUsageEstimate | undefined;
      // B1-03 双发去重：同一 invocation id 的待审批事件（v1 + v2）只回调一次。
      const seenPendingIds = new Set<number>();

      const dispatch = (event: string, dataRaw: string) => {
        let data: unknown;
        try {
          data = JSON.parse(dataRaw);
        } catch {
          return;
        }
        switch (event) {
          case 'sources': {
            const sources = Array.isArray(data) ? (data as RagAnswer[]) : [];
            callbacks.onSources?.(sources);
            break;
          }
          case 'delta': {
            const payload = data as { content?: string };
            if (payload && typeof payload.content === 'string') {
              callbacks.onDelta?.(payload.content);
            }
            break;
          }
          case 'done': {
            const payload = data as {
              conversationId?: number;
              provider?: string;
              providerSource?: string;
            };
            finalConversationId = payload?.conversationId ?? finalConversationId;
            // 开关关闭/静态回退时 done 不含 provider 字段 → 第二参数缺省，
            // 调用方与既有行为完全一致（QA-3 回归门禁）；usage 存在时才补第二参数。
            const info =
              payload?.provider || payload?.providerSource || finalUsage
                ? {
                    provider: payload?.provider,
                    providerSource: payload?.providerSource,
                    usage: finalUsage,
                  }
                : undefined;
            if (info) {
              callbacks.onDone?.(finalConversationId, info);
            } else {
              // 单参数调用，保持与改动前的回调签名逐字一致（既有调用方零感知）。
              callbacks.onDone?.(finalConversationId);
            }
            break;
          }
          case 'usage': {
            // v2（O2-02/§13.4）：回合 token 用量（估算或 provider 实测），恒在 done 前到达；旧后端不发送。
            const payload = data as Partial<AIUsageEstimate> | null;
            if (
              payload &&
              typeof payload.tokenIn === 'number' &&
              typeof payload.tokenOut === 'number'
            ) {
              finalUsage = {
                tokenIn: payload.tokenIn,
                tokenOut: payload.tokenOut,
                // 旧/异常载荷未带 estimated 时按估算处理（宁可标注，也不冒充真实值）。
                estimated: payload.estimated !== false,
                scope: payload.scope,
              };
              callbacks.onUsage?.(finalUsage);
            }
            break;
          }
          case 'error': {
            const payload = data as { message?: string };
            callbacks.onError?.(payload?.message ?? 'unknown stream error');
            break;
          }
          // M1-03 工具事件：四种事件共用同一回调，status 已由后端给出；
          // 缺 status 时按事件名补偿，字段缺失（旧后端/半截载荷）不触发回调。
          case 'tool_call_started':
          case 'tool_call_finished':
          case 'tool_call_failed':
          case 'approval_pending':
          case 'confirmation_required': {
            const payload = data as Partial<AIToolStreamEvent> | null;
            if (!payload || typeof payload.tool !== 'string' || payload.tool.length === 0) {
              break;
            }
            // B1-03 兼容双发：待审批会先发 v1 `approval_pending` 再发 v2
            // `confirmation_required`（同一 invocation id）。按 id 去重，
            // 保证前端只渲染一张待审批卡片；v2-only 后端也能正常接住。
            const pendingId = typeof payload.id === 'number' ? payload.id : undefined;
            if (event === 'confirmation_required' && pendingId !== undefined) {
              if (seenPendingIds.has(pendingId)) {
                break;
              }
              seenPendingIds.add(pendingId);
            } else if (event === 'approval_pending' && pendingId !== undefined) {
              seenPendingIds.add(pendingId);
            }
            const fallbackStatus: Record<string, string> = {
              tool_call_started: 'started',
              tool_call_finished: 'done',
              tool_call_failed: 'failed',
              approval_pending: 'pending',
              confirmation_required: 'pending',
            };
            callbacks.onToolEvent?.({
              ...payload,
              tool: payload.tool,
              provider: typeof payload.provider === 'string' ? payload.provider : 'builtin',
              phase: typeof payload.phase === 'string' ? payload.phase : 'read',
              status: typeof payload.status === 'string' ? payload.status : fallbackStatus[event],
            });
            break;
          }
          // B1-03 v2 运行事件：只做解析与回调，不改变既有渲染路径。
          case 'run_started': {
            const payload = data as Partial<AIRunStartedEvent> | null;
            if (!payload || typeof payload.runId !== 'number') {
              break;
            }
            callbacks.onRunStarted?.({
              v: payload.v,
              runId: payload.runId,
              entrypoint: payload.entrypoint,
              conversationId: payload.conversationId,
            });
            break;
          }
          case 'step': {
            const payload = data as Partial<AIRunStepEvent> | null;
            if (
              !payload ||
              typeof payload.runId !== 'number' ||
              typeof payload.stepIndex !== 'number' ||
              typeof payload.type !== 'string' ||
              payload.type.length === 0
            ) {
              break;
            }
            callbacks.onStep?.({
              v: payload.v,
              runId: payload.runId,
              stepIndex: payload.stepIndex,
              type: payload.type,
              payloadRef: payload.payloadRef,
              durationMs: payload.durationMs,
            });
            break;
          }
          default:
            // 未知事件一律忽略：新后端叠加的事件不会破坏旧前端的渲染（M1-03 兼容口径）。
            // 需要观测时由调用方经 onUnknownEvent 自行埋点（不得据此渲染或中断流）。
            callbacks.onUnknownEvent?.(event, data);
            break;
        }
      };

      const flushBlock = (block: string) => {
        let event = 'message';
        const dataLines: string[] = [];
        for (const line of block.split('\n')) {
          if (line.startsWith('event:')) {
            event = line.slice(6).trim();
          } else if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).trim());
          }
        }
        if (dataLines.length > 0) {
          dispatch(event, dataLines.join('\n'));
        }
      };

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        let boundary = buffer.indexOf('\n\n');
        while (boundary !== -1) {
          const block = buffer.slice(0, boundary);
          buffer = buffer.slice(boundary + 2);
          if (block.trim().length > 0) {
            flushBlock(block);
          }
          boundary = buffer.indexOf('\n\n');
        }
      }
      if (buffer.trim().length > 0) {
        flushBlock(buffer);
      }

      return finalConversationId;
    } catch (err) {
      const aborted = (err as Error)?.name === 'AbortError';
      if (aborted) throw err;
      lastError = err instanceof Error ? err : new Error('stream request failed');
      // 继续尝试下一个 candidate
    }
  }

  // 走到这里表示所有 candidate 都失败，抛给调用方做 fallback
  const message = lastError?.message || 'AI chat stream failed: no candidate succeeded';
  callbacks.onError?.(message);
  throw lastError || new Error(message);
}

/** 列出当前用户的 AI 会话历史 */
export async function listConversations(): Promise<ConversationSummary[]> {
  const res = await httpClient.get<{ conversations: ConversationSummary[] }>('/api/v1/ai/conversations');
  return Array.isArray(res?.conversations) ? res.conversations : [];
}

/** 获取指定会话的所有消息 */
export async function getConversationMessages(conversationId: number): Promise<AIMessage[]> {
  const res = await httpClient.get<{ messages: AIMessage[] }>(`/api/v1/ai/conversations/${conversationId}`);
  return Array.isArray(res?.messages) ? res.messages : [];
}

/** 删除指定会话 */
export async function deleteConversation(conversationId: number): Promise<void> {
  await httpClient.delete(`/api/v1/ai/conversations/${conversationId}`);
}

// ==================== AI 分析结果持久化 ====================

export interface AIAnalysisResultDTO {
  id: number;
  tenantId: number;
  userId: number;
  analysisType: string; // triage | summary | rca | deep_analytics | trend_prediction | incident_impact
  ticketId?: number;
  incidentId?: number;
  ticketNumber?: string;
  ticketTitle?: string;
  requestPrompt: string;
  resultJson: string;
  model?: string;
  latencyMs?: number;
  totalTokens?: number;
  costUsd?: number;
  confidenceScore?: number;
  degraded: boolean;
  createdAt: string;
}

export interface AIAnalysisResultListResponse {
  results: AIAnalysisResultDTO[];
}

/** 列出分析历史（可按 type 过滤） */
export async function listAnalysisResults(params?: {
  type?: string;
  limit?: number;
}): Promise<AIAnalysisResultDTO[]> {
  const res = await httpClient.get<AIAnalysisResultListResponse>('/api/v1/ai/analysis-results', { params });
  return res?.results ?? [];
}

/** 获取单条分析结果 */
export async function getAnalysisResult(id: number): Promise<AIAnalysisResultDTO> {
  return httpClient.get<AIAnalysisResultDTO>(`/api/v1/ai/analysis-results/${id}`);
}

/** 删除分析结果记录 */
export async function deleteAnalysisResult(id: number): Promise<void> {
  await httpClient.delete(`/api/v1/ai/analysis-results/${id}`);
}

// ==================== 兼容类包装器 ====================

export class AIApi {
  static async triage(title: string, description: string): Promise<TriageResult> {
    return aiTriage(title, description);
  }

  static async chat(params: {
    query: string;
    conversationId?: number;
    limit?: number;
    provider?: string;
    /** B2-04：新建会话归属的 Bot（缺省 = 默认助手；已有会话忽略）。 */
    botId?: number;
    /** B3-01/B3-02：入口上下文（缺省 = chat/无目标）。 */
    entrypoint?: string;
    targetType?: string;
    targetId?: number;
    summary?: string;
    /** D0-04：页面上下文提示（缺省不落字段）。 */
    pageContext?: AIPageContextHint;
  }): Promise<any> {
    return httpClient.post(`/api/v1/ai/chat`, {
      query: params.query,
      limit: params.limit,
      conversationId: params.conversationId,
      ...(params.provider ? { provider: params.provider } : {}),
      ...(params.botId ? { botId: params.botId } : {}),
      ...(params.entrypoint ? { entrypoint: params.entrypoint } : {}),
      ...(params.targetType && params.targetId ? { targetType: params.targetType, targetId: params.targetId } : {}),
      ...(params.summary ? { summary: params.summary } : {}),
      ...(params.pageContext ? { pageContext: params.pageContext } : {}),
    });
  }

  static async searchKB(query: string, limit = 5): Promise<{ answers: RagAnswer[] }> {
    return aiSearchKB(query, limit);
  }

  static async similarIncidents(query: string, limit = 5): Promise<{ incidents: RagAnswer[] }> {
    return aiSimilarIncidents(query, limit);
  }

  static async summarize(text: string, maxLen = 200): Promise<{ summary: string }> {
    return aiSummarize(text, maxLen);
  }

  /**
   * 工单/事件 AI 摘要（基于持久化数据，走 LLM Gateway，失败有降级）
   * 对应后端 GET /api/v1/ai/tickets/:id/summary
   */
  static async summarizeTicket(id: number | string): Promise<{
    degraded?: boolean;
    message?: string;
    summary?: string;
  }> {
    return httpClient.get(`/api/v1/ai/tickets/${id}/summary`);
  }

  /**
   * 工单/事件 AI 分析（影响分析、关联推荐等）
   * 对应后端 POST /api/v1/ai/tickets/:id/analyze
   */
  static async analyzeTicket(id: number | string): Promise<unknown> {
    return httpClient.post(`/api/v1/ai/tickets/${id}/analyze`, {});
  }

  /** AI-assisted analysis for an Incident aggregate. */
  static async analyzeIncident(id: number | string): Promise<unknown> {
    return httpClient.post(`/api/v1/ai/incidents/${id}/analyze`, {});
  }

  static async saveFeedback(feedback: AIFeedbackRequest): Promise<{ message: string }> {
    return aiSaveFeedback(feedback);
  }

  static async getMetrics(days = 7): Promise<AIMetrics> {
    return aiGetMetrics(days);
  }

  /** 列出 AI 工具调用审批记录（默认待审批）。对应 GET /api/v1/agent/tools/invocations */
  static async getToolApprovals(state = 'pending'): Promise<ToolApprovalListResponse> {
    return aiGetToolApprovals(state);
  }

  /** 通过 / 驳回工具调用审批。对应 POST /api/v1/agent/tools/:id/approve */
  static async approveTool(id: number, req: ToolApproveRequest): Promise<{ invocationId: number; approvalState: string }> {
    return aiApproveTool(id, req);
  }

  static chatStream(
    req: AIChatStreamRequest,
    callbacks: AIChatStreamCallbacks = {}
  ): Promise<number> {
    return aiChatStream(req, callbacks);
  }

  /** B2-04：工作区 Bot 选择器候选（失败/未开启返回空数组）。 */
  static async listVisibleBots(): Promise<BotOption[]> {
    return aiListVisibleBots();
  }

  /** 工作区 Bot 候选 + 展示用 capabilities（单次请求，供对话页能力降级复用）。 */
  static async listVisibleBotsWithCapabilities(): Promise<AgentBotsResult> {
    return aiListVisibleBotsWithCapabilities();
  }
}

// ==================== 工具目录（内置 + MCP 外部工具的统一查询视图） ====================

/**
 * 工具目录条目：内置工具与 MCP 外部工具统一投影。
 *
 * 后端契约：`GET /api/v1/agent/tools/catalog`（ai:read；按当前角色 RBAC 过滤）。
 */
export interface ToolCatalogItem {
  name: string;
  description?: string;
  readOnly: boolean;
  /** 治理标注风险：read|plan|act_low|act_medium|act_high（内置工具可能为空）。 */
  risk?: string;
  category?: string;
  /** builtin | mcp（内置工具由注册表统一填 builtin）。 */
  provider?: string;
  /** provider=mcp 时的服务器标识（callable 名主体）。 */
  serverName?: string;
  /** provider=mcp 时的服务器侧原始工具名。 */
  rawToolName?: string;
  resource?: string;
  action?: string;
  supportsDryRun?: boolean;
  idempotent?: boolean;
}

export interface ToolCatalogParams {
  q?: string;
  source?: 'builtin' | 'mcp';
  readOnly?: boolean;
  risk?: string;
  limit?: number;
}

/**
 * 工具目录展示用能力块（camelCase，随响应下发）。
 *
 * 工具页据此渲染"能力关闭"Alert 与 MCP 写工具行徽标；缺省 = 未管控，不渲染。
 */
export interface ToolCatalogCapabilities {
  mcpEnabled: boolean;
  mcpWriteEnabled: boolean;
  botEnabled: boolean;
}

export interface ToolCatalogResult {
  items: ToolCatalogItem[];
  total: number;
  capabilities?: ToolCatalogCapabilities;
}

/**
 * 查询工具目录（Bot 授权选择器 / 独立工具目录页共用）。
 *
 * - `q`：名称/描述/服务器 子串（大小写不敏感，服务端过滤）；
 * - `source`/`readOnly`/`risk`：可选过滤；`limit` 默认由后端取 200（上限 500）。
 */
export async function aiListToolCatalog(params: ToolCatalogParams = {}): Promise<ToolCatalogResult> {
  const query = new URLSearchParams();
  if (params.q) query.set('q', params.q);
  if (params.source) query.set('source', params.source);
  if (typeof params.readOnly === 'boolean') query.set('readOnly', String(params.readOnly));
  if (params.risk) query.set('risk', params.risk);
  if (params.limit && params.limit > 0) query.set('limit', String(params.limit));
  const qs = query.toString();
  // 字面量路径 + 拼接，避免模板内三元表达式，保证 api-contract 测试可静态解析路径
  const url = '/api/v1/agent/tools/catalog' + (qs ? `?${qs}` : '');
  const res = await httpClient.get<ToolCatalogResult>(url);
  return {
    items: Array.isArray(res?.items) ? res.items : [],
    total: res?.total ?? 0,
    capabilities: res?.capabilities,
  };
}
// ==================== O1-01 单任务全链路轨迹（run_id 回放） ====================

/**
 * 运行轨迹契约：`GET /api/v1/ai/runs/:id/trace`（ai:read）。
 *
 * 链路（方案 §13.4）：会话 → run → 步骤/事件 → 工具调用（确认/审批）→ 产物 → 业务对象。
 * 口径：原始参数不回显（仅 `argsRedacted`）；跨租户与不存在同为 404。
 */
export interface AIRunTraceRun {
  id: number;
  conversationId: number;
  botId: number;
  entrypoint: string;
  status: string;
  model?: string;
  targetType?: string;
  targetId?: number;
  errorCode?: string;
  budgetJson?: string;
  startedAt: string;
  finishedAt?: string;
}

export interface AIRunTraceStep {
  id: number;
  stepIndex: number;
  type: string;
  payloadRef?: string;
  durationMs: number;
  createdAt: string;
}

export interface AIRunTraceEvent {
  id: number;
  seq: number;
  type: string;
  payload?: Record<string, unknown>;
  createdAt: string;
}

export interface AIRunTraceTool {
  id: number;
  toolName: string;
  status: string;
  approvalState: string;
  /** 规范化确认状态：pending|confirmed|rejected|expired|cancelled|unknown。 */
  confirmationState: string;
  needsApproval: boolean;
  risk?: string;
  category?: string;
  provider?: string;
  serverName?: string;
  argsRedacted?: string;
  result?: string | null;
  outputSummary?: string;
  error?: string | null;
  errorCode?: string;
  verifyState?: string;
  verifyNote?: string;
  durationMs: number;
  targetType?: string;
  targetId?: string;
  stepId: number;
  userId: number;
  approvedBy?: number;
  approvedAt?: string;
  createdAt: string;
}

export interface AIRunTraceArtifact {
  id: number;
  kind: string;
  toolName?: string;
  title?: string;
  contentJson?: string;
  evidenceJson?: string;
  createdAt: string;
}

export interface AIRunTrace {
  run: AIRunTraceRun;
  steps: AIRunTraceStep[];
  events: AIRunTraceEvent[];
  toolInvocations: AIRunTraceTool[];
  artifacts: AIRunTraceArtifact[];
}

/** 读取单次运行的全链路轨迹（管理侧回放；404 = 不存在或跨租户）。 */
export async function aiGetRunTrace(runId: number): Promise<AIRunTrace> {
  // 字面量路径 + 拼接，避免模板内三元表达式，保证 api-contract 测试可静态解析路径
  const url = '/api/v1/ai/runs/' + runId + '/trace';
  const res = await httpClient.get<AIRunTrace>(url);
  return {
    run: res?.run,
    steps: Array.isArray(res?.steps) ? res.steps : [],
    events: Array.isArray(res?.events) ? res.events : [],
    toolInvocations: Array.isArray(res?.toolInvocations) ? res.toolInvocations : [],
    artifacts: Array.isArray(res?.artifacts) ? res.artifacts : [],
  };
}

// ==================== O1-01 条件项：会话维度轨迹（纯问答可回放） ====================

/**
 * 会话维度轨迹契约：`GET /api/v1/ai/conversations/:id/trace`（ai:read）。
 *
 * 链路：会话档案 → 消息回合 →（若有）运行列表 → 工具调用与确认 → 产物。
 * 口径：纯问答无 bot_runs 同样可回放；消息超长/超窗口由后端截断并显式标记
 * （`truncated` / `messagesTruncated`）；原始参数不回显（仅 `argsRedacted`）；
 * 跨租户与不存在同为 404。
 */
export interface AIConversationTraceMessage {
  id: number;
  role: string;
  content: string;
  truncated?: boolean;
  createdAt: string;
}

export interface AIConversationTraceRun {
  id: number;
  status: string;
  entrypoint?: string;
  model?: string;
  errorCode?: string;
  startedAt: string;
  finishedAt?: string;
}

export interface AIConversationTraceConversation {
  id: number;
  userId?: number;
  botId?: number;
  title?: string;
  createdAt: string;
}

export interface AIConversationTrace {
  conversation: AIConversationTraceConversation;
  messages: AIConversationTraceMessage[];
  messageCount: number;
  messagesTruncated: boolean;
  runs: AIConversationTraceRun[];
  toolInvocations: AIRunTraceTool[];
  artifacts: AIRunTraceArtifact[];
}

/** 读取会话维度全链路轨迹（纯问答无 run 同样可回放；404 = 不存在或跨租户）。 */
export async function aiGetConversationTrace(conversationId: number): Promise<AIConversationTrace> {
  // 字面量路径 + 拼接，避免模板内三元表达式，保证 api-contract 测试可静态解析路径
  const url = '/api/v1/ai/conversations/' + conversationId + '/trace';
  const res = await httpClient.get<AIConversationTrace>(url);
  return {
    conversation: res?.conversation,
    messages: Array.isArray(res?.messages) ? res.messages : [],
    messageCount: typeof res?.messageCount === 'number' ? res.messageCount : (res?.messages?.length ?? 0),
    messagesTruncated: res?.messagesTruncated === true,
    runs: Array.isArray(res?.runs) ? res.runs : [],
    toolInvocations: Array.isArray(res?.toolInvocations) ? res.toolInvocations : [],
    artifacts: Array.isArray(res?.artifacts) ? res.artifacts : [],
  };
}

// ==================== O1-02 实时健康与告警（SLO） ====================

/**
 * 健康快照契约：`GET /api/v1/ai/health`（ai:read）。
 * 告警派发：`POST /api/v1/ai/health/alerts/dispatch`（ai:write；违规项投递到通知中心，同标题 30 分钟去重）。
 */
export interface AIHealthRuns {
  total: number;
  completed: number;
  failed: number;
  running: number;
  successRate: number;
  avgDurationMs: number;
}

export interface AIHealthTools {
  total: number;
  errors: number;
  errorRate: number;
  avgDurationMs: number;
}

export interface AIHealthConfirmations {
  approved: number;
  rejected: number;
  expired: number;
  pending: number;
  expireRate: number;
}

export interface AIHealthQueue {
  available: boolean;
  length: number;
  capacity: number;
}

export interface AIHealthProviders {
  available: boolean;
  callCount: number;
  successRate: number;
  known: boolean;
  avgLatencyMs: number;
}

export interface AIHealthQuality {
  errorShown: number;
  errorRecovered: number;
  recoveryRate: number;
  latencyP95Ms: number;
  taskStarted: number;
  taskCompleted: number;
}

export interface AIHealthSnapshot {
  tenantId: number;
  windowDays: number;
  generatedAt: string;
  runs: AIHealthRuns;
  tools: AIHealthTools;
  confirmations: AIHealthConfirmations;
  queue: AIHealthQueue;
  providers: AIHealthProviders;
  quality: AIHealthQuality;
  notes?: string[];
}

export type AIHealthSLOStatus = 'ok' | 'breach' | 'insufficient_data';

export interface AIHealthSLO {
  rule: string;
  title: string;
  status: AIHealthSLOStatus;
  actual: number;
  threshold: number;
  comparator: 'gte' | 'lte';
  sample: number;
  message?: string;
}

export interface AIHealthResponse {
  snapshot: AIHealthSnapshot;
  slos: AIHealthSLO[];
}

export interface AIHealthAlert {
  rule: string;
  severity: 'warning' | 'critical';
  title: string;
  message: string;
  actual: number;
  threshold: number;
}

export interface AIHealthDispatchResult {
  snapshot: AIHealthSnapshot;
  slos: AIHealthSLO[];
  alerts: AIHealthAlert[];
  recipients: number;
  created: number;
  skipped: number;
  /** 因用户订阅偏好（ai_health_alert 关闭站内通知）被排除的候选收件人数（O1-02 条件项）。 */
  skippedByPreference?: number;
}

/** 采集健康快照 + SLO 评估（只读）。 */
export async function aiGetAIHealth(days = 7): Promise<AIHealthResponse> {
  const url = '/api/v1/ai/health?days=' + encodeURIComponent(String(days));
  const res = await httpClient.get<AIHealthResponse>(url);
  return {
    snapshot: res?.snapshot,
    slos: Array.isArray(res?.slos) ? res.slos : [],
  };
}

/** 评估并把违规告警投递到通知中心（写面；返回本次新建/去重计数）。 */
export async function aiDispatchAIHealthAlerts(days = 7): Promise<AIHealthDispatchResult> {
  const url = '/api/v1/ai/health/alerts/dispatch?days=' + encodeURIComponent(String(days));
  return httpClient.post<AIHealthDispatchResult>(url, {});
}// ==================== I2-01 Autopilot 监督面（运行列表 / 暂停 / 回滚） ====================

/**
 * Autopilot 运行监督契约：
 * - `GET /api/v1/ai/runs?days=&status=&limit=`（ai:read）：运行列表 + 步骤/工具/待审批/产物计数；
 * - `POST /api/v1/ai/runs/:id/cancel`（ai:write）：暂停/终止（仅 running 生效，幂等）；
 * - `POST /api/v1/ai/runs/:id/rollback`（ai:write）：终止 + 冻结该运行全部待审批（cancelled，永不执行）
 *   + 回滚结果投递通知中心（`type=ai_autopilot`，深链 `/ai/approval`）。
 */
export interface AutopilotRunSummary {
  id: number;
  conversationId: number;
  botId: number;
  entrypoint: string;
  status: string;
  model?: string;
  errorCode?: string;
  targetType?: string;
  targetId?: number;
  startedAt: string;
  finishedAt?: string;
  steps: number;
  toolCalls: number;
  pendingApprovals: number;
  artifacts: number;
}

export interface AutopilotRunListResponse {
  items: AutopilotRunSummary[];
  days: number;
  status: string;
}

export interface AutopilotActionResponse {
  runId: number;
  cancelled: boolean;
}

export interface AutopilotRollbackResult extends AutopilotActionResponse {
  rejectedInvocations: number;
  notifications: number;
  reason?: string;
}

/** 运行列表（监督面；status 为空串/缺省 = 全部状态）。 */
export async function aiListAutopilotRuns(
  params: { days?: number; status?: string; limit?: number } = {}
): Promise<AutopilotRunListResponse> {
  const query = new URLSearchParams();
  if (params.days) query.set('days', String(params.days));
  if (params.status) query.set('status', params.status);
  if (params.limit) query.set('limit', String(params.limit));
  const qs = query.toString();
  const url = '/api/v1/ai/runs' + (qs ? `?${qs}` : '');
  const res = await httpClient.get<AutopilotRunListResponse>(url);
  return {
    items: Array.isArray(res?.items) ? res.items : [],
    days: res?.days ?? 0,
    status: res?.status ?? '',
  };
}

/** 暂停/终止运行（仅 running 生效；返回 cancelled=false 表示已收口）。 */
export async function aiCancelAutopilotRun(
  runId: number,
  reason?: string
): Promise<AutopilotActionResponse> {
  const url = '/api/v1/ai/runs/' + runId + '/cancel';
  return httpClient.post<AutopilotActionResponse>(url, { reason });
}

/** 回滚运行：终止 + 冻结待审批 + 通知中心投递。 */
export async function aiRollbackAutopilotRun(
  runId: number,
  reason?: string
): Promise<AutopilotRollbackResult> {
  const url = '/api/v1/ai/runs/' + runId + '/rollback';
  return httpClient.post<AutopilotRollbackResult>(url, { reason });
}// ==================== I2-02 合规：会话导出（Markdown 快照） ====================

/**
 * 会话导出契约：`GET /api/v1/ai/conversations/:id/export`（ai:read）。
 * 返回 Markdown 快照（标题/导出时间/消息数 + 逐条角色与内容）；跨租户与不存在同口径 404。
 */
export interface AIConversationExport {
  conversationId: number;
  title: string;
  exportedAt: string;
  messageCount: number;
  markdown: string;
}

/** 导出会话快照（前端据此生成 `.md` 下载，不落服务端文件）。 */
export async function exportConversation(conversationId: number): Promise<AIConversationExport> {
  const url = '/api/v1/ai/conversations/' + conversationId + '/export';
  return httpClient.get<AIConversationExport>(url);
}// ==================== V0-04 试点对照报告（§11.5 放量门槛） ====================

/**
 * 契约：`GET /api/v1/ai/observability/pilot-report?currentDays=&baselineDays=`（ai:read）。
 * 对照口径：本期窗口 vs 基线窗口（前一窗口，同一埋点采集）；门槛：≥2 项试点相对基线改善 ≥30% 且负向指标不恶化。
 */
export interface PilotMetric {
  key: string;
  title: string;
  unit: 'rate' | 'ms';
  direction: 'up' | 'down';
  kind: 'primary' | 'secondary' | 'negative';
  baseline: number;
  current: number;
  delta: number;
  improvement: number;
  comparable: boolean;
  improved: boolean;
  sample: number;
  note?: string;
}

export interface PilotSection {
  key: string;
  title: string;
  pilot: string;
  metrics: PilotMetric[];
  met: boolean;
  reasons?: string[];
}

export interface PilotWindowInfo {
  days: number;
  since: string;
  until: string;
}

export interface PilotGate {
  passed: boolean;
  pilotsMet: number;
  required: number;
  reasons: string[];
  thresholds: string[];
}

export interface PilotReport {
  tenantId: number;
  current: PilotWindowInfo;
  baseline: PilotWindowInfo;
  generatedAt: string;
  sections: PilotSection[];
  negative: PilotMetric[];
  gate: PilotGate;
  notes?: string[];
}

/** 试点对照报告（窗口缺省 7/7，上限 30）。 */
export async function aiGetPilotReport(
  params: { currentDays?: number; baselineDays?: number } = {}
): Promise<PilotReport> {
  const query = new URLSearchParams();
  if (params.currentDays) query.set('currentDays', String(params.currentDays));
  if (params.baselineDays) query.set('baselineDays', String(params.baselineDays));
  const qs = query.toString();
  const url = '/api/v1/ai/observability/pilot-report' + (qs ? `?${qs}` : '');
  return httpClient.get<PilotReport>(url);
}// ==================== O2-01 分群对比（role | pageKey 维度） ====================

/**
 * 契约：`GET /api/v1/ai/observability/segments?dimension=role|pageKey&days=`（ai:read）。
 * 口径：租户隐式条件；按维度分组输出采用/建议/任务/Inline/错误子集（分母为 0 → 0）。
 */
export interface AssistantSegment {
  key: string;
  events: number;
  sessions: number;
  suggestionShown: number;
  suggestionClicked: number;
  taskStarted: number;
  taskCompleted: number;
  inlinePreview: number;
  inlineApplied: number;
  errors: number;
  suggestionCtr: number;
  taskCompletionRate: number;
  inlineAdoptionRate: number;
  errorRate: number;
}

export interface AssistantSegmentsReport {
  tenantId: number;
  days: number;
  dimension: 'role' | 'pageKey';
  segments: AssistantSegment[];
  generatedAt: string;
  notes?: string[];
}

/** 分群对比报告（维度白名单；窗口缺省 7，上限 90）。 */
export async function aiGetAssistantSegments(
  params: { dimension?: 'role' | 'pageKey'; days?: number } = {}
): Promise<AssistantSegmentsReport> {
  const query = new URLSearchParams();
  if (params.dimension) query.set('dimension', params.dimension);
  if (params.days) query.set('days', String(params.days));
  const qs = query.toString();
  const url = '/api/v1/ai/observability/segments' + (qs ? `?${qs}` : '');
  return httpClient.get<AssistantSegmentsReport>(url);
}

// ==================== P2 文档命中率/解决率运营报表 ====================

/** 遥测窗口内的原始计数与会话级分母（`GET /api/v1/ai/observability/doc-metrics`）。 */
export interface DocMetricsTotals {
  opened: number;
  gaps: number;
  drafts: number;
  /** AI 起草的草稿被实际保存成知识库文章的条数（doc_draft_published）。 */
  published: number;
  feedbackHelpful: number;
  feedbackUnhelpful: number;
  handoffs: number;
  openedSessions: number;
  gapSessions: number;
  handoffSessions: number;
  events: number;
}

/** 四口径比率（无有效分母时字段缺省/为 null，前端渲染「—」，不虚报 0%）。 */
export interface DocMetricsRates {
  hitRate?: number | null;
  resolveRate?: number | null;
  feedbackResolveRate?: number | null;
  /** 起草落地率 = published / drafts（无起草动作时缺省 = null）。 */
  draftPublishRate?: number | null;
}

export interface DocMetricsPage {
  pageKey: string;
  opened: number;
  gaps: number;
  drafts: number;
  published: number;
  helpful: number;
  unhelpful: number;
}

export interface DocMetricsTrendPoint {
  day: string;
  opened: number;
  gaps: number;
  drafts: number;
  published: number;
  helpful: number;
  unhelpful: number;
}

/** 账本覆盖率快照（与 `/ai/help/coverage` 同源；埋点未装配时报表的基础面）。 */
export interface DocMetricsCoverage {
  coverage: number;
  totals: Record<string, number>;
}

export interface DocMetricsReport {
  days: number;
  totals: DocMetricsTotals;
  rates: DocMetricsRates;
  byPage: DocMetricsPage[];
  trend: DocMetricsTrendPoint[];
  coverage: DocMetricsCoverage;
  telemetry: { available: boolean };
  generatedAt?: string;
}

/** 读取文档命中率/解决率运营报表（days 缺省 30，上限 90）。 */
export async function aiGetDocMetrics(params: { days?: number } = {}): Promise<DocMetricsReport> {
  const query = new URLSearchParams();
  if (params.days && params.days > 0) query.set('days', String(params.days));
  const qs = query.toString();
  const url = '/api/v1/ai/observability/doc-metrics' + (qs ? `?${qs}` : '');
  return httpClient.get<DocMetricsReport>(url);
}

// ==================== P2 LLM 动态建议（缓存 + 审计；失败回退静态基线） ====================

/**
 * 契约：`GET /api/v1/ai/assistant/suggestions/dynamic?pageKey=&route=`（ai:read）。
 * - `generated=false` / 请求失败 / 503 → 前端保持静态注册表基线（动态建议只是增强）；
 * - `cached=true` 表示服务端缓存命中（进程内 TTL）。
 */
export interface AIDynamicSuggestion {
  id: string;
  kind: 'task' | 'question';
  label: string;
  prompt: string;
  readOnly: boolean;
  source: string;
}

export interface AIDynamicSuggestionResult {
  pageKey: string;
  known: boolean;
  generated: boolean;
  cached: boolean;
  model?: string;
  suggestions: AIDynamicSuggestion[];
  reason?: string;
}

/** 读取页面动态建议（LLM 生成 + 服务端缓存；未知页面/未启用时按降级返回）。 */
export async function aiGetDynamicSuggestions(params: {
  pageKey: string;
  route?: string;
}): Promise<AIDynamicSuggestionResult> {
  const query = new URLSearchParams();
  query.set('pageKey', params.pageKey);
  if (params.route) query.set('route', params.route);
  const url = '/api/v1/ai/assistant/suggestions/dynamic?' + query.toString();
  return httpClient.get<AIDynamicSuggestionResult>(url);
}

/**
 * V2-02 异步精算（先给可用答案 + 后台精算完成后通知）。
 *
 * 契约：`POST /api/v1/ai/async/answers`（ai:read）→ 立即返回 `{taskId,status,fast}`；
 * 后台精算完成后 `GET /api/v1/ai/async/answers/:id` 返回 `refined`（并投递通知中心）。
 * 未装配 Provider 时 `status=degraded`（仅快速参考），不假装精算。
 */
export type AIAsyncAnswerStatus = 'running' | 'done' | 'degraded' | 'failed';

export interface AIAsyncAnswerFastItem {
  title: string;
  snippet: string;
  path?: string;
  pageKey?: string;
}

export interface AIAsyncAnswerFast {
  source: 'help_index' | 'none' | string;
  items: AIAsyncAnswerFastItem[];
}

export interface AIAsyncAnswerRefined {
  model?: string;
  answer: string;
}

export interface AIAsyncAnswerTask {
  taskId: string;
  status: AIAsyncAnswerStatus;
  prompt: string;
  pageKey?: string;
  createdAt: string;
  finishedAt?: string;
  fast: AIAsyncAnswerFast;
  refined?: AIAsyncAnswerRefined;
  reason?: string;
}

/** 发起异步精算：立即拿到快速参考（先给可用答案），精算在后台进行。 */
export async function aiStartAsyncAnswer(params: {
  prompt: string;
  pageKey?: string;
  route?: string;
}): Promise<AIAsyncAnswerTask> {
  return httpClient.post<AIAsyncAnswerTask>('/api/v1/ai/async/answers', params);
}

/** 读取异步精算任务快照（租户 + 用户隔离；未知/过期 → 404）。 */
export async function aiGetAsyncAnswer(taskId: string): Promise<AIAsyncAnswerTask> {
  return httpClient.get<AIAsyncAnswerTask>(`/api/v1/ai/async/answers/${encodeURIComponent(taskId)}`);
}
