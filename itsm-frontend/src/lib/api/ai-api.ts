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

/** SSE `done` 事件回带的生效实例信息（BE-7；开关关闭时两端都缺省 → 与现状一致）。 */
export interface AIChatDoneInfo {
  provider?: string;
  providerSource?: string;
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
  | { type: 'done'; conversationId: number; provider?: string; providerSource?: string }
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
  try {
    const res = (await httpClient.get('/api/v1/agent/bots')) as { items?: BotOption[] } | undefined;
    const items = Array.isArray(res?.items) ? res.items : [];
    return items
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
      }));
  } catch {
    return [];
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
  signal?: AbortSignal;
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
            // 调用方与既有行为完全一致（QA-3 回归门禁）。
            const info =
              payload?.provider || payload?.providerSource
                ? { provider: payload?.provider, providerSource: payload?.providerSource }
                : undefined;
            if (info) {
              callbacks.onDone?.(finalConversationId, info);
            } else {
              // 单参数调用，保持与改动前的回调签名逐字一致（既有调用方零感知）。
              callbacks.onDone?.(finalConversationId);
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
}
