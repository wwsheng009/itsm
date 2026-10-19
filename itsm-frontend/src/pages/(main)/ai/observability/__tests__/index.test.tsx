/**
 * AI 观测看板测试（O0-03）。
 *
 * 覆盖：四层指标渲染（采用/效果/质量/时延）、分解表、角色过滤触发重载、
 * 空窗口提示、503 → 服务未启用、其他错误提示与重试。
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import AssistantObservabilityPage from '..';
import {
  aiGetAssistantTelemetrySummary,
  aiGetConversationTrace,
  aiGetRunTrace,
  aiGetAIHealth,
  aiDispatchAIHealthAlerts,
  aiGetPilotReport,
  aiGetAssistantSegments,
  aiGetDocMetrics,
  type AIRunTrace,
  type AIConversationTrace,
  type AIHealthDispatchResult,
  type AIHealthResponse,
  type AssistantSegmentsReport,
  type DocMetricsReport,
  type PilotReport,
  type AssistantTelemetrySummary,
} from '@/lib/api/ai-api';
import { usePermissions } from '@/lib/hooks/use-permissions';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetAssistantTelemetrySummary: jest.fn(),
  aiGetConversationTrace: jest.fn(),
  aiGetRunTrace: jest.fn(),
  aiGetAIHealth: jest.fn(),
  aiDispatchAIHealthAlerts: jest.fn(),
  aiGetPilotReport: jest.fn(),
  aiGetAssistantSegments: jest.fn(),
  aiGetDocMetrics: jest.fn(),
}));
jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));

const mockedGet = aiGetAssistantTelemetrySummary as jest.MockedFunction<
  typeof aiGetAssistantTelemetrySummary
>;
const mockedTrace = aiGetRunTrace as jest.MockedFunction<typeof aiGetRunTrace>;
const mockedConversationTrace = aiGetConversationTrace as jest.MockedFunction<
  typeof aiGetConversationTrace
>;
const mockedHealth = aiGetAIHealth as jest.MockedFunction<typeof aiGetAIHealth>;
const mockedDispatch = aiDispatchAIHealthAlerts as jest.MockedFunction<
  typeof aiDispatchAIHealthAlerts
>;
const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;
const mockedPilot = aiGetPilotReport as jest.MockedFunction<typeof aiGetPilotReport>;
const mockedSegments = aiGetAssistantSegments as jest.MockedFunction<
  typeof aiGetAssistantSegments
>;
const mockedDocMetrics = aiGetDocMetrics as jest.MockedFunction<typeof aiGetDocMetrics>;

const segmentsFixture: AssistantSegmentsReport = {
  tenantId: 1,
  days: 7,
  dimension: 'role',
  generatedAt: '2026-10-18T12:00:00Z',
  segments: [
    {
      key: 'agent',
      events: 200,
      sessions: 45,
      suggestionShown: 40,
      suggestionClicked: 12,
      taskStarted: 10,
      taskCompleted: 6,
      inlinePreview: 20,
      inlineApplied: 9,
      errors: 4,
      suggestionCtr: 0.3,
      taskCompletionRate: 0.6,
      inlineAdoptionRate: 0.45,
      errorRate: 0.02,
    },
  ],
};

const docMetricsFixture: DocMetricsReport = {
  days: 7,
  totals: {
    opened: 8,
    gaps: 1,
    drafts: 2,
    published: 1,
    feedbackHelpful: 5,
    feedbackUnhelpful: 1,
    handoffs: 1,
    openedSessions: 8,
    gapSessions: 1,
    handoffSessions: 1,
    events: 17,
  },
  rates: { hitRate: 0.875, resolveRate: 0.875, feedbackResolveRate: 5 / 6, draftPublishRate: 0.5 },
  byPage: [{ pageKey: 'dashboard', opened: 4, gaps: 1, drafts: 1, published: 1, helpful: 3, unhelpful: 1 }],
  trend: [{ day: '2026-10-18', opened: 8, gaps: 1, drafts: 2, published: 1, helpful: 5, unhelpful: 1 }],
  coverage: { coverage: 0.81, totals: { pages: 21, ok: 17, missing: 3, stale: 1, orphan: 0 } },
  telemetry: { available: true },
  generatedAt: '2026-10-18T12:00:00Z',
};

const pilotFixture: PilotReport = {
  tenantId: 1,
  current: { days: 7, since: '2026-10-11T12:00:00Z', until: '2026-10-18T12:00:00Z' },
  baseline: { days: 7, since: '2026-10-04T12:00:00Z', until: '2026-10-11T12:00:00Z' },
  generatedAt: '2026-10-18T12:00:00Z',
  sections: [
    {
      key: 'ticket_inline',
      pilot: 'V0-01',
      title: '工单 Inline 处理',
      met: true,
      reasons: ['达标'],
      metrics: [
        {
          key: 'inline_adoption_rate',
          title: 'Inline 采纳率',
          unit: 'rate',
          direction: 'up',
          kind: 'primary',
          baseline: 0.5,
          current: 0.8,
          delta: 0.3,
          improvement: 0.6,
          comparable: true,
          improved: true,
          sample: 100,
        },
      ],
    },
  ],
  negative: [],
  gate: {
    passed: true,
    pilotsMet: 2,
    required: 2,
    reasons: ['达标：可按 §7 顺序放量 Dock'],
    thresholds: ['§11.5：≥2 项试点相对基线改善 ≥30%'],
  },
  notes: [],
};

const grantWrite = (allowed: boolean) => {
  mockedPermissions.mockReturnValue({
    hasPermission: (resource: string, action: string) => allowed && resource === 'ai' && action === 'write',
  } as unknown as ReturnType<typeof usePermissions>);
};

const healthFixture: AIHealthResponse = {
  snapshot: {
    tenantId: 1,
    windowDays: 7,
    generatedAt: '2026-10-18T12:00:00Z',
    runs: { total: 10, completed: 8, failed: 2, running: 0, successRate: 0.8, avgDurationMs: 1200 },
    tools: { total: 6, errors: 2, errorRate: 2 / 6, avgDurationMs: 400 },
    confirmations: { approved: 1, rejected: 0, expired: 0, pending: 1, expireRate: 0 },
    queue: { available: true, length: 2, capacity: 100 },
    providers: { available: true, callCount: 20, successRate: 0.95, known: true, avgLatencyMs: 900 },
    quality: {
      errorShown: 10,
      errorRecovered: 9,
      recoveryRate: 0.9,
      latencyP95Ms: 2500,
      taskStarted: 12,
      taskCompleted: 10,
    },
    notes: ['窗口内无 LLM 调用记录（Provider 段不可判定）'],
  },
  slos: [
    {
      rule: 'ai_task_success_rate',
      title: '任务成功率',
      status: 'breach',
      actual: 0.8,
      threshold: 0.95,
      comparator: 'gte',
      sample: 10,
      message: '任务成功率 80.00% 低于阈值 95.00%',
    },
    {
      rule: 'ai_tool_error_rate',
      title: '工具错误率',
      status: 'breach',
      actual: 2 / 6,
      threshold: 0.05,
      comparator: 'lte',
      sample: 6,
    },
    {
      rule: 'ai_llm_success_rate',
      title: 'LLM 调用成功率',
      status: 'ok',
      actual: 0.95,
      threshold: 0.95,
      comparator: 'gte',
      sample: 20,
    },
  ],
};

const dispatchFixture: AIHealthDispatchResult = {
  snapshot: healthFixture.snapshot,
  slos: healthFixture.slos,
  alerts: [
    {
      rule: 'ai_task_success_rate',
      severity: 'critical',
      title: '【AI 健康】任务成功率',
      message: '任务成功率 80.00% 低于阈值 95.00%（样本 10）',
      actual: 0.8,
      threshold: 0.95,
    },
  ],
  recipients: 2,
  created: 2,
  skipped: 0,
  // O1-02 条件项：后端已回传退订过滤人数，面板需显式展示（口径可解释）。
  skippedByPreference: 1,
};

const traceFixture: AIRunTrace = {
  run: {
    id: 5,
    conversationId: 9,
    botId: 7,
    entrypoint: 'ticket_detail',
    status: 'completed',
    model: 'gpt-4o',
    targetType: 'ticket',
    targetId: 42,
    startedAt: '2026-10-18T10:00:00Z',
    finishedAt: '2026-10-18T10:00:03Z',
  },
  steps: [{ id: 1, stepIndex: 0, type: 'llm', durationMs: 800, createdAt: '2026-10-18T10:00:00Z' }],
  events: [{ id: 1, seq: 0, type: 'run_started', payload: { entrypoint: 'ticket_detail' }, createdAt: '2026-10-18T10:00:00Z' }],
  toolInvocations: [
    {
      id: 1,
      toolName: 'create_ticket',
      status: 'success',
      approvalState: 'approved',
      confirmationState: 'confirmed',
      needsApproval: true,
      verifyState: 'verified',
      durationMs: 120,
      targetType: 'ticket',
      targetId: '42',
      stepId: 1,
      userId: 7,
      createdAt: '2026-10-18T10:00:01Z',
    },
  ],
  artifacts: [
    {
      id: 31,
      kind: 'analysis',
      toolName: 'analyze_ci_impact_plan',
      title: '影响分析',
      createdAt: '2026-10-18T10:00:02Z',
    },
  ],
};

const conversationTraceFixture: AIConversationTrace = {
  conversation: { id: 12, userId: 7, botId: 0, title: '连不上网', createdAt: '2026-10-18T10:00:00Z' },
  messages: [
    { id: 1, role: 'user', content: '我连不上网了', createdAt: '2026-10-18T10:00:00Z' },
    { id: 2, role: 'assistant', content: '请先确认范围', createdAt: '2026-10-18T10:00:01Z' },
  ],
  messageCount: 2,
  messagesTruncated: false,
  runs: [],
  toolInvocations: [],
  artifacts: [],
};

const fixture: AssistantTelemetrySummary = {
  days: 7,
  totalEvents: 120,
  distinctSessions: 8,
  errorEvents: 6,
  byName: [
    { key: 'assistant_opened', count: 30 },
    { key: 'message_sent', count: 40 },
    { key: 'stream_end', count: 35 },
    { key: 'task_started', count: 10 },
    { key: 'task_completed', count: 7 },
    { key: 'suggestion_clicked', count: 12 },
    { key: 'confirmation_accepted', count: 4 },
    { key: 'confirmation_rejected', count: 1 },
    { key: 'error_shown', count: 6 },
  ],
  byPage: [
    { key: 'ai.chat', count: 80 },
    { key: 'ticket.detail', count: 40 },
  ],
  latencyMs: {
    avg: 850.5,
    p50: 800,
    p95: 1500,
    max: 3000,
    count: 35,
    firstToken: { avg: 300, p50: 280, p95: 450, max: 600, count: 35 },
    overall: { avg: 850.5, p50: 800, p95: 1500, max: 3000, count: 35 },
  },
  quality: {
    answerCount: 10,
    citationAnswerCount: 6,
    feedbackCount: 8,
    downvoteCount: 2,
    citationCoverage: 0.6,
    noCitationRate: 0.4,
    downvoteRate: 0.25,
  },
  models: [
    { key: 'gpt-4o-mini', count: 30 },
    { key: 'claude-sonnet-4', count: 5 },
  ],
  generatedAt: '2026-10-18T12:00:00Z',
};

describe('AssistantObservabilityPage（O0-03 看板）', () => {
  beforeEach(() => {
    mockedGet.mockReset();
    mockedTrace.mockReset();
    mockedConversationTrace.mockReset();
    mockedHealth.mockReset();
    mockedDispatch.mockReset();
    mockedPilot.mockReset();
    mockedSegments.mockReset();
    mockedDocMetrics.mockReset();
    mockedHealth.mockResolvedValue(healthFixture);
    mockedDispatch.mockResolvedValue(dispatchFixture);
    mockedPilot.mockResolvedValue(pilotFixture);
    mockedSegments.mockResolvedValue(segmentsFixture);
    mockedDocMetrics.mockResolvedValue(docMetricsFixture);
    mockedConversationTrace.mockResolvedValue(conversationTraceFixture);
    // 默认无写面：派发按钮不渲染（用例按需放开）。
    grantWrite(false);
    window.history.replaceState({}, '', '/');
  });

  it('渲染四层指标、分解表与生成时间', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);

    await waitFor(() =>
      expect(mockedGet).toHaveBeenCalledWith({ days: 7, pageKey: undefined, role: undefined })
    );

    // 采用层
    expect(await screen.findByTestId('stat-total-events')).toHaveTextContent('120');
    expect(screen.getByTestId('stat-distinct-sessions')).toHaveTextContent('8');
    expect(screen.getByTestId('stat-assistant-opened')).toHaveTextContent('30');
    expect(screen.getByTestId('stat-suggestion-clicked')).toHaveTextContent('12');
    // 效果层
    expect(screen.getByTestId('stat-task-started')).toHaveTextContent('10');
    expect(screen.getByTestId('stat-task-completed')).toHaveTextContent('7');
    expect(screen.getByTestId('stat-task-completion-rate')).toHaveTextContent('70.0%');
    // 质量层
    expect(screen.getByTestId('stat-error-events')).toHaveTextContent('6');
    expect(screen.getByTestId('stat-error-rate')).toHaveTextContent('5.0%');
    expect(screen.getByTestId('stat-confirmation-accepted')).toHaveTextContent('4');
    expect(screen.getByTestId('stat-confirmation-rejected')).toHaveTextContent('1');
    // T5-06b：质量层三指标 + 分母样本
    expect(screen.getByTestId('quality-citation-coverage')).toHaveTextContent('60.0%');
    expect(screen.getByTestId('quality-no-citation-rate')).toHaveTextContent('40.0%');
    expect(screen.getByTestId('quality-downvote-rate')).toHaveTextContent('25.0%');
    expect(screen.getByTestId('quality-citation-sample')).toHaveTextContent('有引用 6 / 可统计 10 回答');
    expect(screen.getByTestId('quality-downvote-sample')).toHaveTextContent('点踩 2 / 反馈 8');
    // 成本与时延
    expect(screen.getByTestId('stat-first-token-p50')).toHaveTextContent('280 ms');
    expect(screen.getByTestId('stat-first-token-p95')).toHaveTextContent('450 ms');
    expect(screen.getByTestId('stat-first-token-count')).toHaveTextContent('35');
    expect(screen.getByTestId('stat-overall-p50')).toHaveTextContent('800 ms');
    expect(screen.getByTestId('stat-overall-p95')).toHaveTextContent('1500 ms');
    expect(screen.getByTestId('stat-overall-max')).toHaveTextContent('3000 ms');
    // T5-06b：模型分布（仅 stream_end 上报）
    expect(screen.getByTestId('model-distribution')).toHaveTextContent('gpt-4o-mini · 30');
    expect(screen.getByTestId('model-distribution')).toHaveTextContent('claude-sonnet-4 · 5');
    // 分解表与生成时间
    expect(screen.getByTestId('observability-by-name')).toHaveTextContent('assistant_opened');
    expect(screen.getByTestId('observability-by-page')).toHaveTextContent('ticket.detail');
    expect(screen.getByTestId('observability-generated-at')).toHaveTextContent('2026-10-18T12:00:00Z');
    // 关键转化漏斗：打开 30 → 发送 40（133.3%）→ 完成回答 35（87.5%）→ 任务完成 7（20.0%）
    expect(screen.getByTestId('funnel-assistant_opened')).toHaveTextContent('30');
    expect(screen.getByTestId('funnel-message_sent')).toHaveTextContent('133.3%');
    expect(screen.getByTestId('funnel-stream_end')).toHaveTextContent('87.5%');
    expect(screen.getByTestId('funnel-task_completed')).toHaveTextContent('20.0%');
  });

  it('角色过滤：输入回车后按新过滤重载', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);
    await waitFor(() => expect(mockedGet).toHaveBeenCalledTimes(1));

    const roleInput = screen.getByTestId('observability-role');
    fireEvent.change(roleInput, { target: { value: 'agent' } });
    fireEvent.keyDown(roleInput, { key: 'Enter', code: 'Enter', keyCode: 13 });

    await waitFor(() =>
      expect(mockedGet).toHaveBeenLastCalledWith({ days: 7, pageKey: undefined, role: 'agent' })
    );
  });

  it('P2：渲染文档命中率/解决率报表（比率/缺口页/覆盖率）', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);

    expect(await screen.findByTestId('doc-metrics-card')).toBeInTheDocument();
    await waitFor(() => expect(mockedDocMetrics).toHaveBeenCalledWith({ days: 7 }));
    expect(screen.getByTestId('doc-metrics-hit-rate')).toHaveTextContent('87.5%');
    expect(screen.getByTestId('doc-metrics-resolve-rate')).toHaveTextContent('87.5%');
    expect(screen.getByTestId('doc-metrics-feedback-rate')).toHaveTextContent('83.3%');
    expect(screen.getByTestId('doc-metrics-gaps')).toHaveTextContent('1');
    expect(screen.getByTestId('doc-metrics-coverage')).toHaveTextContent('81.0%');
    expect(screen.getByTestId('doc-metrics-by-page')).toHaveTextContent('dashboard');
  });

  it('空数据窗口展示空态提示', async () => {
    mockedGet.mockResolvedValue({
      ...fixture,
      totalEvents: 0,
      errorEvents: 0,
      byName: [],
      byPage: [],
      latencyMs: undefined,
      quality: undefined,
      models: [],
    });

    render(<AssistantObservabilityPage />);

    expect(await screen.findByTestId('observability-empty')).toHaveTextContent('暂无埋点数据');
    expect(screen.getByTestId('quality-empty')).toBeInTheDocument();
    expect(screen.getByTestId('model-distribution-empty')).toBeInTheDocument();
  });

  it('服务未装配（503）展示未启用提示，不报错', async () => {
    mockedGet.mockRejectedValue({ response: { status: 503 } });

    render(<AssistantObservabilityPage />);

    expect(await screen.findByTestId('observability-disabled')).toHaveTextContent('埋点服务未启用');
    expect(screen.queryByTestId('stat-total-events')).not.toBeInTheDocument();
  });

  it('其他错误展示错误提示并可重试', async () => {
    mockedGet.mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce(fixture);

    render(<AssistantObservabilityPage />);
    expect(await screen.findByTestId('observability-error')).toHaveTextContent('boom');

    fireEvent.click(screen.getByTestId('observability-refresh'));
    await waitFor(() => expect(mockedGet).toHaveBeenCalledTimes(2));
    expect(await screen.findByTestId('stat-total-events')).toHaveTextContent('120');
  });

  it('轨迹回放：输入 run_id 展示完整链路（会话→运行→工具/确认→产物→业务对象）', async () => {
    mockedGet.mockResolvedValue(fixture);
    mockedTrace.mockResolvedValue(traceFixture);

    render(<AssistantObservabilityPage />);
    await screen.findByTestId('stat-total-events');

    fireEvent.change(screen.getByTestId('observability-trace-input'), {
      target: { value: '5' },
    });
    fireEvent.click(screen.getByTestId('observability-trace-load'));

    await waitFor(() => expect(mockedTrace).toHaveBeenCalledWith(5));
    expect(await screen.findByTestId('run-trace-view')).toBeInTheDocument();
    expect(screen.getByTestId('run-trace-tool-1-confirmation')).toHaveTextContent('已确认');
    expect(screen.getByTestId('run-trace-artifact-31')).toHaveTextContent('artifact:31');
    expect(screen.getByTestId('run-trace-targets')).toHaveTextContent('ticket #42');
  });

  it('轨迹回放：?runId= 深链自动加载；404 提示不存在', async () => {
    window.history.replaceState({}, '', '/ai/observability?runId=5');
    mockedGet.mockResolvedValue(fixture);
    mockedTrace.mockRejectedValue({ response: { status: 404 } });

    render(<AssistantObservabilityPage />);

    await waitFor(() => expect(mockedTrace).toHaveBeenCalledWith(5));
    expect(await screen.findByTestId('observability-trace-error')).toHaveTextContent(
      '轨迹不存在或跨租户不可见'
    );
  });

  it('会话轨迹回放：输入 conversation_id 展示会话维度链路（消息/纯问答）', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);
    await screen.findByTestId('stat-total-events');

    fireEvent.change(screen.getByTestId('observability-conversation-trace-input'), {
      target: { value: '12' },
    });
    fireEvent.click(screen.getByTestId('observability-conversation-trace-load'));

    await waitFor(() => expect(mockedConversationTrace).toHaveBeenCalledWith(12));
    expect(await screen.findByTestId('conversation-trace-view')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-trace-messages')).toHaveTextContent('我连不上网了');
    expect(screen.getByTestId('conversation-trace-pure-qa')).toBeInTheDocument();
    // run 维度零请求（互斥）。
    expect(mockedTrace).not.toHaveBeenCalled();
  });

  it('会话轨迹回放：?conversationId= 深链自动加载；404 提示不存在', async () => {
    window.history.replaceState({}, '', '/ai/observability?conversationId=12');
    mockedGet.mockResolvedValue(fixture);
    mockedConversationTrace.mockRejectedValue({ response: { status: 404 } });

    render(<AssistantObservabilityPage />);

    await waitFor(() => expect(mockedConversationTrace).toHaveBeenCalledWith(12));
    expect(await screen.findByTestId('observability-conversation-trace-error')).toHaveTextContent(
      '会话轨迹不存在或跨租户不可见'
    );
  });

  it('实时健康与 SLO：快照指标 + SLO 状态标签渲染', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);

    expect(await screen.findByTestId('observability-health-card')).toBeInTheDocument();
    expect(await screen.findByTestId('health-task-success-rate')).toHaveTextContent('80.0%');
    expect(screen.getByTestId('health-tool-error-rate')).toHaveTextContent('33.3%');
    expect(screen.getByTestId('health-queue')).toHaveTextContent('2/100');
    expect(screen.getByTestId('health-provider-success-rate')).toHaveTextContent('95.0%');
    expect(screen.getByTestId('health-slo-ai_task_success_rate')).toHaveTextContent('违规');
    expect(screen.getByTestId('health-slo-ai_llm_success_rate')).toHaveTextContent('通过');
    expect(screen.getByTestId('health-notes')).toHaveTextContent('Provider 段不可判定');
  });

  it('实时健康：ai:write 下派发告警并展示结果', async () => {
    mockedGet.mockResolvedValue(fixture);
    grantWrite(true);

    render(<AssistantObservabilityPage />);
    await screen.findByTestId('observability-health-card');

    fireEvent.click(screen.getByTestId('health-dispatch'));

    await waitFor(() => expect(mockedDispatch).toHaveBeenCalledWith(7));
    expect(await screen.findByTestId('health-dispatch-result')).toHaveTextContent('新建 2 条');
    expect(screen.getByTestId('health-dispatch-result')).toHaveTextContent('收件人 2');
    // O1-02 条件项闭环：退订过滤人数显式可见（非「去重跳过」口径）。
    expect(screen.getByTestId('health-dispatch-preference-filtered')).toHaveTextContent(
      '另有 1 名候选收件人因订阅偏好（站内通知关闭）被排除'
    );
  });

  it('实时健康：派发结果未回传 skippedByPreference 时不渲染退订过滤文案（诚实缺省）', async () => {
    mockedGet.mockResolvedValue(fixture);
    mockedDispatch.mockResolvedValue({ ...dispatchFixture, skippedByPreference: 0 });
    grantWrite(true);

    render(<AssistantObservabilityPage />);
    await screen.findByTestId('observability-health-card');
    fireEvent.click(screen.getByTestId('health-dispatch'));

    expect(await screen.findByTestId('health-dispatch-result')).toBeInTheDocument();
    expect(screen.queryByTestId('health-dispatch-preference-filtered')).not.toBeInTheDocument();
  });

  it('实时健康：无 ai:write 不渲染派发按钮', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);

    await screen.findByTestId('observability-health-card');
    expect(screen.queryByTestId('health-dispatch')).not.toBeInTheDocument();
  });

  it('V0-02 基线：自助服务卡展示自助解决率、转建单与字段纠正（误分类率代理）', async () => {
    mockedGet.mockResolvedValue({
      ...fixture,
      totalEvents: 220,
      byName: [
        ...fixture.byName,
        { key: 'self_service_answered', count: 10 },
        { key: 'self_service_resolved', count: 6 },
        { key: 'self_service_ticket_created', count: 3 },
        { key: 'ai_create_field_corrected', count: 2 },
      ],
    });

    render(<AssistantObservabilityPage />);

    expect(await screen.findByTestId('observability-self-service-card')).toBeInTheDocument();
    expect(screen.getByTestId('self-service-answered')).toHaveTextContent('10');
    expect(screen.getByTestId('self-service-resolve-rate')).toHaveTextContent('60.0%');
    expect(screen.getByTestId('self-service-ticket-created')).toHaveTextContent('3');
    expect(screen.getByTestId('self-service-field-corrected')).toHaveTextContent('2');
    expect(screen.queryByTestId('self-service-empty')).not.toBeInTheDocument();
  });

  it('V0-04：试点对照卡渲染 §11.5 门槛结论与指标对照', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);

    expect(await screen.findByTestId('pilot-report-card')).toBeInTheDocument();
    expect(await screen.findByTestId('pilot-report-gate')).toHaveTextContent('达标（可放量）');
    expect(screen.getByTestId('pilot-report-table')).toHaveTextContent('Inline 采纳率');
    expect(screen.getByTestId('pilot-report-table')).toHaveTextContent('+60.0%');
  });

  it('O2-01：分群对比卡按角色维度渲染子集指标', async () => {
    mockedGet.mockResolvedValue(fixture);

    render(<AssistantObservabilityPage />);

    expect(await screen.findByTestId('segment-breakdown-card')).toBeInTheDocument();
    await waitFor(() =>
      expect(mockedSegments).toHaveBeenCalledWith({ dimension: 'role', days: 7 })
    );
    expect(screen.getByTestId('segment-table')).toHaveTextContent('agent');
    expect(screen.getByTestId('segment-table')).toHaveTextContent('30.0%');
  });
});
