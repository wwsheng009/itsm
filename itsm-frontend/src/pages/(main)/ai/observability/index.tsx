/**
 * AI 观测看板（O0-03）
 *
 * 数据来源：`GET /api/v1/ai/telemetry/summary`（service/ai_assistant_telemetry.go 聚合 ai_telemetry_events）。
 * 口径（方案 §13.3 四层）：采用 / 效果 / 质量 / 成本与时延；支持 days / pageKey / role 过滤。
 * 降级：埋点服务未装配（503/404）→ 「服务未启用」提示；窗口内无数据 → 空态说明（事件随使用产生）。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Col, Input, Row, Select, Space, Statistic, Table, Tag, Typography } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useI18n } from '@/lib/i18n/useI18n';
import { usePermissions } from '@/lib/hooks/use-permissions';
import {
  aiGetAssistantTelemetrySummary,
  aiGetConversationTrace,
  aiGetRunTrace,
  aiGetAIHealth,
  aiDispatchAIHealthAlerts,
  type AIRunTrace,
  type AIConversationTrace,
  type AIHealthDispatchResult,
  type AIHealthResponse,
  type AIHealthSLOStatus,
  type AssistantTelemetryBucket,
  type AssistantTelemetrySummary,
} from '@/lib/api/ai-api';
import ConversationTraceView from '@/components/ai/run-trace/ConversationTraceView';
import RunTraceView from '@/components/ai/run-trace/RunTraceView';
import PilotReportCard from '@/components/ai/observability/PilotReportCard';
import SegmentBreakdownCard from '@/components/ai/observability/SegmentBreakdownCard';
import DocMetricsCard from '@/components/ai/observability/DocMetricsCard';
import QualityMetricsCard from '@/components/ai/observability/QualityMetricsCard';
import LLMCallObservabilityCard from '@/components/ai/observability/LLMCallObservabilityCard';

const { Title, Text } = Typography;

export const pickObservabilityErrStatus = (err: unknown): number | undefined => {
  const e = err as { response?: { status?: number }; status?: number; code?: number } | undefined;
  return e?.response?.status ?? e?.status ?? (typeof e?.code === 'number' ? e.code : undefined);
};

export const fmtObservabilityRate = (value: number | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—';

export const fmtObservabilityMs = (value: number | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${Math.round(value)} ms` : '—';

interface ObservabilityFilters {
  days: number;
  pageKey?: string;
  role: string;
}

const AssistantObservabilityPage: React.FC = () => {
  const { t } = useI18n();
  const [filters, setFilters] = useState<ObservabilityFilters>({ days: 7, role: '' });
  const [roleInput, setRoleInput] = useState('');
  const [pageOptions, setPageOptions] = useState<{ value: string; label: string }[]>([]);
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<AssistantTelemetrySummary | null>(null);
  const [disabled, setDisabled] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { hasPermission } = usePermissions();
  // O1-02：实时健康与 SLO（只读快照；告警派发为写面）。
  const canDispatchAlerts = hasPermission('ai', 'write');
  const [health, setHealth] = useState<AIHealthResponse | null>(null);
  const [healthLoading, setHealthLoading] = useState(false);
  const [healthError, setHealthError] = useState<string | null>(null);
  const [dispatching, setDispatching] = useState(false);
  const [dispatchResult, setDispatchResult] = useState<AIHealthDispatchResult | null>(null);
  const [dispatchError, setDispatchError] = useState<string | null>(null);

  const loadHealth = useCallback(async (days: number) => {
    setHealthLoading(true);
    setHealthError(null);
    try {
      setHealth(await aiGetAIHealth(days));
    } catch (err) {
      const status = pickObservabilityErrStatus(err);
      setHealth(null);
      setHealthError(
        status === 503 ? t('aiObservability.disabledTitle') : t('aiObservability.healthLoadFailed')
      );
    } finally {
      setHealthLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadHealth(filters.days);
  }, [filters.days, loadHealth]);

  const handleDispatchAlerts = useCallback(async () => {
    setDispatching(true);
    setDispatchError(null);
    try {
      const result = await aiDispatchAIHealthAlerts(filters.days);
      setDispatchResult(result);
      await loadHealth(filters.days);
    } catch {
      setDispatchResult(null);
      setDispatchError(t('aiObservability.healthDispatchFailed'));
    } finally {
      setDispatching(false);
    }
  }, [filters.days, loadHealth, t]);

  const sloStatusColor = useCallback((status: AIHealthSLOStatus): string => {
    if (status === 'ok') return 'green';
    if (status === 'breach') return 'red';
    return 'default';
  }, []);

  const sloStatusLabel = useCallback(
    (status: AIHealthSLOStatus): string => {
      if (status === 'ok') return t('aiObservability.healthSloOk');
      if (status === 'breach') return t('aiObservability.healthSloBreach');
      return t('aiObservability.healthSloInsufficient');
    },
    [t]
  );

  // O1-01 轨迹回放：支持 /ai/observability?runId=<id> 深链（工具/审计页跳转入口）。
  const [traceInput, setTraceInput] = useState(() =>
    typeof window === 'undefined' ? '' : new URLSearchParams(window.location.search).get('runId') ?? ''
  );
  const [traceRunId, setTraceRunId] = useState<number | undefined>(() => {
    if (typeof window === 'undefined') return undefined;
    const parsed = Number.parseInt(new URLSearchParams(window.location.search).get('runId') ?? '', 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
  });
  const [trace, setTrace] = useState<AIRunTrace | null>(null);
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceError, setTraceError] = useState<'not_found' | 'failed' | null>(null);

  // O1-01 条件项：会话维度回放，支持 /ai/observability?conversationId=<id> 深链
  // （审计页工具行/纯问答会话跳转入口）；与 run 维度互斥显示。
  const [conversationTraceInput, setConversationTraceInput] = useState(() =>
    typeof window === 'undefined' ? '' : new URLSearchParams(window.location.search).get('conversationId') ?? ''
  );
  const [traceConversationId, setTraceConversationId] = useState<number | undefined>(() => {
    if (typeof window === 'undefined') return undefined;
    const params = new URLSearchParams(window.location.search);
    // runId 深链优先：两者同时出现时只回放 run 维度（避免双请求歧义）。
    if ((params.get('runId') ?? '') !== '') return undefined;
    const parsed = Number.parseInt(params.get('conversationId') ?? '', 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
  });
  const [conversationTrace, setConversationTrace] = useState<AIConversationTrace | null>(null);
  const [conversationTraceLoading, setConversationTraceLoading] = useState(false);
  const [conversationTraceError, setConversationTraceError] = useState<'not_found' | 'failed' | null>(null);

  useEffect(() => {
    if (!traceRunId) return undefined;
    let alive = true;
    setTraceLoading(true);
    setTraceError(null);
    setConversationTrace(null);
    setConversationTraceError(null);
    aiGetRunTrace(traceRunId)
      .then(result => {
        if (alive) setTrace(result);
      })
      .catch(err => {
        if (!alive) return;
        setTrace(null);
        setTraceError(pickObservabilityErrStatus(err) === 404 ? 'not_found' : 'failed');
      })
      .finally(() => {
        if (alive) setTraceLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [traceRunId]);

  useEffect(() => {
    if (!traceConversationId) return undefined;
    let alive = true;
    setConversationTraceLoading(true);
    setConversationTraceError(null);
    setTrace(null);
    setTraceError(null);
    aiGetConversationTrace(traceConversationId)
      .then(result => {
        if (alive) setConversationTrace(result);
      })
      .catch(err => {
        if (!alive) return;
        setConversationTrace(null);
        setConversationTraceError(pickObservabilityErrStatus(err) === 404 ? 'not_found' : 'failed');
      })
      .finally(() => {
        if (alive) setConversationTraceLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [traceConversationId]);

  const parsedTraceInput = Number.parseInt(traceInput.trim(), 10);
  const canLoadTrace = Number.isFinite(parsedTraceInput) && parsedTraceInput > 0;
  const parsedConversationTraceInput = Number.parseInt(conversationTraceInput.trim(), 10);
  const canLoadConversationTrace =
    Number.isFinite(parsedConversationTraceInput) && parsedConversationTraceInput > 0;

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await aiGetAssistantTelemetrySummary({
        days: filters.days,
        pageKey: filters.pageKey || undefined,
        role: filters.role || undefined,
      });
      setData(result);
      setDisabled(false);
      if (!filters.pageKey) {
        setPageOptions(
          (result.byPage ?? []).map(item => ({
            value: item.key,
            label: `${item.key} (${item.count})`,
          }))
        );
      }
    } catch (err) {
      const status = pickObservabilityErrStatus(err);
      if (status === 503 || status === 404) {
        setDisabled(true);
        setData(null);
      } else {
        setError((err as Error)?.message || t('aiObservability.loadFailed'));
      }
    } finally {
      setLoading(false);
    }
  }, [filters, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const countOf = useCallback(
    (name: string): number => (data?.byName ?? []).find(b => b.key === name)?.count ?? 0,
    [data]
  );

  const columns = useMemo(
    () => [
      { title: t('aiObservability.colKey'), dataIndex: 'key', key: 'key' },
      { title: t('aiObservability.colCount'), dataIndex: 'count', key: 'count' },
    ],
    [t]
  );

  // 关键转化漏斗（事件计数口径：同窗口内事件量对比，不区分用户）。
  const funnelSteps = useMemo(
    () => [
      { key: 'assistant_opened', label: t('aiObservability.funnelOpened') },
      { key: 'message_sent', label: t('aiObservability.funnelMessage') },
      { key: 'stream_end', label: t('aiObservability.funnelStreamEnd') },
      { key: 'task_completed', label: t('aiObservability.funnelTask') },
    ],
    [t]
  );

  const applyRole = useCallback(() => {
    const nextRole = roleInput.trim();
    // 未变化时返回原引用，避免 onBlur 触发无意义的重复请求。
    setFilters(prev => (prev.role === nextRole ? prev : { ...prev, role: nextRole }));
  }, [roleInput]);

  if (disabled) {
    return (
      <div data-testid="ai-observability-page" style={{ padding: 24 }}>
        <Alert
          type="info"
          showIcon
          message={t('aiObservability.disabledTitle')}
          description={t('aiObservability.disabledHint')}
          data-testid="observability-disabled"
        />
      </div>
    );
  }

  const taskStarted = countOf('task_started');
  const taskCompleted = countOf('task_completed');
  const completionRate = taskStarted > 0 ? taskCompleted / taskStarted : undefined;
  const errorRate = data && data.totalEvents > 0 ? data.errorEvents / data.totalEvents : undefined;
  const models = data?.models ?? [];

  return (
    <div data-testid="ai-observability-page" style={{ padding: 24 }}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Row justify="space-between" align="middle" gutter={[8, 8]}>
          <Col>
            <Title level={4} style={{ margin: 0 }}>
              {t('aiObservability.title')}
            </Title>
            <Text type="secondary">{t('aiObservability.description')}</Text>
          </Col>
          <Col>
            <Space wrap>
              <Select
                data-testid="observability-days"
                value={filters.days}
                onChange={days => setFilters(prev => ({ ...prev, days }))}
                options={[7, 14, 30, 90].map(d => ({
                  value: d,
                  label: t('aiObservability.lastDays', { days: d }),
                }))}
                style={{ width: 140 }}
              />
              <Select
                data-testid="observability-page-key"
                value={filters.pageKey}
                onChange={pageKey => setFilters(prev => ({ ...prev, pageKey }))}
                options={pageOptions}
                allowClear
                placeholder={t('aiObservability.pageFilter')}
                style={{ width: 220 }}
              />
              <Input
                data-testid="observability-role"
                value={roleInput}
                onChange={e => setRoleInput(e.target.value)}
                onPressEnter={applyRole}
                onBlur={applyRole}
                allowClear
                placeholder={t('aiObservability.roleFilter')}
                style={{ width: 180 }}
              />
              <Button
                data-testid="observability-refresh"
                icon={<ReloadOutlined />}
                loading={loading}
                onClick={() => {
                  const nextRole = roleInput.trim();
                  if (nextRole !== filters.role) {
                    // 输入框有新值时先应用过滤（state 变化会自动触发加载）。
                    setFilters(prev => ({ ...prev, role: nextRole }));
                  } else {
                    void load();
                  }
                }}
              >
                {t('aiObservability.refresh')}
              </Button>
            </Space>
          </Col>
        </Row>

        {error ? (
          <Alert type="error" showIcon message={error} data-testid="observability-error" />
        ) : null}

        {data && data.totalEvents === 0 ? (
          <Alert
            type="info"
            showIcon
            message={t('aiObservability.empty')}
            data-testid="observability-empty"
          />
        ) : null}

        {/* O1-02 实时健康与 SLO（快照只读；违规可派发到通知中心） */}
        <Card
          title={t('aiObservability.healthTitle')}
          data-testid="observability-health-card"
          extra={
            canDispatchAlerts ? (
              <Button
                data-testid="health-dispatch"
                type="primary"
                size="small"
                loading={dispatching}
                onClick={() => void handleDispatchAlerts()}
              >
                {t('aiObservability.healthDispatch')}
              </Button>
            ) : null
          }
        >
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('aiObservability.healthDescription')}
            </Text>
            {healthError ? (
              <Alert type="warning" showIcon data-testid="health-error" message={healthError} />
            ) : null}
            {health ? (
              <>
                <Row gutter={[16, 16]}>
                  <Col span={6}>
                    <div data-testid="health-task-success-rate">
                      <Statistic
                        title={t('aiObservability.healthRunsSuccessRate')}
                        value={fmtObservabilityRate(health.snapshot?.runs?.successRate)}
                      />
                    </div>
                  </Col>
                  <Col span={6}>
                    <div data-testid="health-tool-error-rate">
                      <Statistic
                        title={t('aiObservability.healthToolErrorRate')}
                        value={fmtObservabilityRate(health.snapshot?.tools?.errorRate)}
                      />
                    </div>
                  </Col>
                  <Col span={6}>
                    <div data-testid="health-queue">
                      <Statistic
                        title={t('aiObservability.healthQueue')}
                        value={
                          health.snapshot?.queue?.available
                            ? `${health.snapshot.queue.length}/${health.snapshot.queue.capacity}`
                            : '—'
                        }
                      />
                    </div>
                  </Col>
                  <Col span={6}>
                    <div data-testid="health-provider-success-rate">
                      <Statistic
                        title={t('aiObservability.healthProviderSuccessRate')}
                        value={
                          health.snapshot?.providers?.known
                            ? fmtObservabilityRate(health.snapshot.providers.successRate)
                            : '—'
                        }
                      />
                    </div>
                  </Col>
                </Row>
                <Space wrap size={[8, 8]} data-testid="health-slo-list">
                  {health.slos.map(item => (
                    <Tag
                      key={item.rule}
                      color={sloStatusColor(item.status)}
                      title={item.message || `${item.rule} ${item.actual} / ${item.threshold}`}
                      data-testid={`health-slo-${item.rule}`}
                    >
                      {item.title}：{sloStatusLabel(item.status)}
                    </Tag>
                  ))}
                </Space>
                {dispatchResult ? (
                  <Space direction="vertical" size={4} style={{ width: '100%' }}>
                    <Alert
                      type={dispatchResult.alerts.length > 0 ? 'warning' : 'success'}
                      showIcon
                      data-testid="health-dispatch-result"
                      message={t('aiObservability.healthDispatchResult', {
                        created: dispatchResult.created,
                        skipped: dispatchResult.skipped,
                        recipients: dispatchResult.recipients,
                      })}
                    />
                    {/* O1-02 条件项闭环：订阅偏好过滤人数（后端提供且命中时显示，口径可解释）。 */}
                    {typeof dispatchResult.skippedByPreference === 'number' &&
                    dispatchResult.skippedByPreference > 0 ? (
                      <Text
                        type="secondary"
                        data-testid="health-dispatch-preference-filtered"
                        style={{ fontSize: 12 }}
                      >
                        {t('aiObservability.healthDispatchPreferenceFiltered', {
                          muted: dispatchResult.skippedByPreference,
                        })}
                      </Text>
                    ) : null}
                  </Space>
                ) : null}
                {dispatchError ? (
                  <Alert
                    type="error"
                    showIcon
                    data-testid="health-dispatch-error"
                    message={dispatchError}
                  />
                ) : null}
                {health.snapshot?.notes?.length ? (
                  <Text type="secondary" style={{ fontSize: 12 }} data-testid="health-notes">
                    {health.snapshot.notes.join('；')}
                  </Text>
                ) : null}
              </>
            ) : healthLoading ? (
              <Text type="secondary">{`${t('aiObservability.refresh')}…`}</Text>
            ) : null}
          </Space>
        </Card>

        {/* 租户级 LLM 调用历史与错误观测（不做预算/金额闸门）。 */}
        <LLMCallObservabilityCard days={filters.days} />

        {/* V0-02 基线：员工侧自助服务与一句话建单（自助解决率 / 误分类率代理） */}
        <Card
          title={t('aiObservability.selfServiceTitle')}
          data-testid="observability-self-service-card"
        >
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Row gutter={[16, 16]}>
              <Col span={6}>
                <div data-testid="self-service-answered">
                  <Statistic
                    title={t('aiObservability.selfServiceAnswered')}
                    value={countOf('self_service_answered')}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="self-service-resolve-rate">
                  <Statistic
                    title={t('aiObservability.selfServiceResolveRate')}
                    value={fmtObservabilityRate(
                      countOf('self_service_answered') > 0
                        ? countOf('self_service_resolved') / countOf('self_service_answered')
                        : undefined
                    )}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="self-service-ticket-created">
                  <Statistic
                    title={t('aiObservability.selfServiceTicketCreated')}
                    value={countOf('self_service_ticket_created')}
                  />
                </div>
              </Col>
              <Col span={6}>
                <div data-testid="self-service-field-corrected">
                  <Statistic
                    title={t('aiObservability.selfServiceFieldCorrected')}
                    value={countOf('ai_create_field_corrected')}
                  />
                </div>
              </Col>
            </Row>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('aiObservability.selfServiceDescription')}
            </Text>
            {countOf('self_service_answered') === 0 ? (
              <Text type="secondary" style={{ fontSize: 12 }} data-testid="self-service-empty">
                {t('aiObservability.empty')}
              </Text>
            ) : null}
          </Space>
        </Card>

        {/* V0-04 试点对照报告 + §11.5 放量门槛（本期 vs 基线；可导出 Markdown） */}
        <PilotReportCard days={filters.days} pageKey="ai.observability" />

        {/* T5-06b §11.5 质量层三指标：引用覆盖率 / 无引用回答率 / 点踩率（可测） */}
        <QualityMetricsCard quality={data?.quality} />

        {/* O2-01 分群对比（按角色/页面找"哪里对用户真正有用"） */}
        <SegmentBreakdownCard days={filters.days} />

        {/* P2 文档命中率/解决率运营报表（帮助闭环：命中率/解决率/缺口收敛 + 账本覆盖率） */}
        <DocMetricsCard days={filters.days} pageKey="ai.observability" />

        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card title={t('aiObservability.layerAdoption')} data-testid="layer-adoption">
              <Row gutter={[16, 16]}>
                <Col span={6}>
                  <div data-testid="stat-total-events">
                    <Statistic title={t('aiObservability.totalEvents')} value={data?.totalEvents ?? 0} />
                  </div>
                </Col>
                <Col span={6}>
                  <div data-testid="stat-distinct-sessions">
                    <Statistic
                      title={t('aiObservability.distinctSessions')}
                      value={data?.distinctSessions ?? 0}
                    />
                  </div>
                </Col>
                <Col span={6}>
                  <div data-testid="stat-assistant-opened">
                    <Statistic title={t('aiObservability.assistantOpened')} value={countOf('assistant_opened')} />
                  </div>
                </Col>
                <Col span={6}>
                  <div data-testid="stat-suggestion-clicked">
                    <Statistic
                      title={t('aiObservability.suggestionClicked')}
                      value={countOf('suggestion_clicked')}
                    />
                  </div>
                </Col>
              </Row>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title={t('aiObservability.layerOutcome')} data-testid="layer-outcome">
              <Row gutter={[16, 16]}>
                <Col span={6}>
                  <div data-testid="stat-task-started">
                    <Statistic title={t('aiObservability.taskStarted')} value={taskStarted} />
                  </div>
                </Col>
                <Col span={6}>
                  <div data-testid="stat-task-completed">
                    <Statistic title={t('aiObservability.taskCompleted')} value={taskCompleted} />
                  </div>
                </Col>
                <Col span={12}>
                  <div data-testid="stat-task-completion-rate">
                    <Statistic
                      title={t('aiObservability.taskCompletionRate')}
                      value={fmtObservabilityRate(completionRate)}
                    />
                  </div>
                </Col>
              </Row>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title={t('aiObservability.layerQuality')} data-testid="layer-quality">
              <Row gutter={[16, 16]}>
                <Col span={6}>
                  <div data-testid="stat-error-events">
                    <Statistic title={t('aiObservability.errorEvents')} value={data?.errorEvents ?? 0} />
                  </div>
                </Col>
                <Col span={6}>
                  <div data-testid="stat-error-rate">
                    <Statistic title={t('aiObservability.errorRate')} value={fmtObservabilityRate(errorRate)} />
                  </div>
                </Col>
                <Col span={6}>
                  <div data-testid="stat-confirmation-accepted">
                    <Statistic
                      title={t('aiObservability.confirmationAccepted')}
                      value={countOf('confirmation_accepted')}
                    />
                  </div>
                </Col>
                <Col span={6}>
                  <div data-testid="stat-confirmation-rejected">
                    <Statistic
                      title={t('aiObservability.confirmationRejected')}
                      value={countOf('confirmation_rejected')}
                    />
                  </div>
                </Col>
              </Row>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title={t('aiObservability.layerCost')} data-testid="layer-latency">
              <Text strong style={{ display: 'block', marginBottom: 8 }}>
                {t('aiObservability.latencyFirstTokenTitle')}
              </Text>
              <Row gutter={[16, 16]}>
                <Col span={8}>
                  <div data-testid="stat-first-token-p50">
                    <Statistic
                      title={t('aiObservability.latencyP50')}
                      value={fmtObservabilityMs(data?.latencyMs?.firstToken?.p50)}
                    />
                  </div>
                </Col>
                <Col span={8}>
                  <div data-testid="stat-first-token-p95">
                    <Statistic
                      title={t('aiObservability.latencyP95')}
                      value={fmtObservabilityMs(data?.latencyMs?.firstToken?.p95)}
                    />
                  </div>
                </Col>
                <Col span={8}>
                  <div data-testid="stat-first-token-count">
                    <Statistic
                      title={t('aiObservability.latencySampleCount')}
                      value={data?.latencyMs?.firstToken?.count ?? '—'}
                    />
                  </div>
                </Col>
              </Row>
              <Text strong style={{ display: 'block', marginTop: 16, marginBottom: 8 }}>
                {t('aiObservability.latencyOverallTitle')}
              </Text>
              <Row gutter={[16, 16]}>
                <Col span={8}>
                  <div data-testid="stat-overall-p50">
                    <Statistic
                      title={t('aiObservability.latencyP50')}
                      value={fmtObservabilityMs(data?.latencyMs?.overall?.p50)}
                    />
                  </div>
                </Col>
                <Col span={8}>
                  <div data-testid="stat-overall-p95">
                    <Statistic
                      title={t('aiObservability.latencyP95')}
                      value={fmtObservabilityMs(data?.latencyMs?.overall?.p95)}
                    />
                  </div>
                </Col>
                <Col span={8}>
                  <div data-testid="stat-overall-max">
                    <Statistic
                      title={t('aiObservability.latencyMax')}
                      value={fmtObservabilityMs(data?.latencyMs?.overall?.max)}
                    />
                  </div>
                </Col>
              </Row>
              <div style={{ marginTop: 16 }} data-testid="model-distribution">
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {t('aiObservability.modelDistributionTitle')}
                </Text>
                <Space wrap size={[8, 8]} style={{ marginTop: 8 }}>
                  {models.length > 0 ? (
                    models.map(item => (
                      <Tag key={item.key} data-testid={`model-distribution-${item.key}`}>
                        {item.key} · {item.count}
                      </Tag>
                    ))
                  ) : (
                    <Text type="secondary" style={{ fontSize: 12 }} data-testid="model-distribution-empty">
                      {t('aiObservability.modelDistributionEmpty')}
                    </Text>
                  )}
                </Space>
              </div>
            </Card>
          </Col>
        </Row>

        <Card title={t('aiObservability.funnelTitle')} data-testid="observability-funnel">
          <Row gutter={[16, 16]}>
            {funnelSteps.map((step, index) => {
              const current = countOf(step.key);
              const previous = index === 0 ? undefined : countOf(funnelSteps[index - 1].key);
              const conversion =
                index === 0 || previous === undefined || previous === 0 ? undefined : current / previous;
              return (
                <Col span={6} key={step.key}>
                  <div data-testid={`funnel-${step.key}`}>
                    <Statistic title={step.label} value={current} />
                    {index > 0 ? (
                      <Text type="secondary">
                        {t('aiObservability.funnelConversion')} {fmtObservabilityRate(conversion)}
                      </Text>
                    ) : null}
                  </div>
                </Col>
              );
            })}
          </Row>
        </Card>

        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card title={t('aiObservability.byName')}>
              <div data-testid="observability-by-name">
                <Table<AssistantTelemetryBucket>
                  rowKey="key"
                  size="small"
                  columns={columns}
                  dataSource={data?.byName ?? []}
                  pagination={false}
                />
              </div>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title={t('aiObservability.byPage')}>
              <div data-testid="observability-by-page">
                <Table<AssistantTelemetryBucket>
                  rowKey="key"
                  size="small"
                  columns={columns}
                  dataSource={data?.byPage ?? []}
                  pagination={false}
                />
              </div>
            </Card>
          </Col>
        </Row>

        {data?.generatedAt ? (
          <Text type="secondary" data-testid="observability-generated-at">
            {t('aiObservability.generatedAt', { time: data.generatedAt })}
          </Text>
        ) : null}

        {/* O1-01 轨迹回放：run_id（任务式）与 conversation_id（纯问答/会话全局）双入口。 */}
        <Card title={t('aiObservability.traceTitle')} data-testid="observability-trace-card">
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Space wrap>
              <Input
                data-testid="observability-trace-input"
                value={traceInput}
                onChange={e => setTraceInput(e.target.value)}
                onPressEnter={() => {
                  if (!canLoadTrace) return;
                  setConversationTrace(null);
                  setTraceConversationId(undefined);
                  setTraceRunId(parsedTraceInput);
                }}
                placeholder={t('aiObservability.traceRunIdPlaceholder')}
                allowClear
                style={{ width: 200 }}
              />
              <Button
                data-testid="observability-trace-load"
                type="primary"
                disabled={!canLoadTrace}
                loading={traceLoading}
                onClick={() => {
                  setConversationTrace(null);
                  setTraceConversationId(undefined);
                  setTraceRunId(parsedTraceInput);
                }}
              >
                {t('aiObservability.traceLoad')}
              </Button>
              <Input
                data-testid="observability-conversation-trace-input"
                value={conversationTraceInput}
                onChange={e => setConversationTraceInput(e.target.value)}
                onPressEnter={() => {
                  if (!canLoadConversationTrace) return;
                  setTrace(null);
                  setTraceRunId(undefined);
                  setTraceConversationId(parsedConversationTraceInput);
                }}
                placeholder={t('aiObservability.conversationTraceIdPlaceholder')}
                allowClear
                style={{ width: 200 }}
              />
              <Button
                data-testid="observability-conversation-trace-load"
                disabled={!canLoadConversationTrace}
                loading={conversationTraceLoading}
                onClick={() => {
                  setTrace(null);
                  setTraceRunId(undefined);
                  setTraceConversationId(parsedConversationTraceInput);
                }}
              >
                {t('aiObservability.conversationTraceLoad')}
              </Button>
            </Space>
            {traceError ? (
              <Alert
                type={traceError === 'not_found' ? 'warning' : 'error'}
                showIcon
                data-testid="observability-trace-error"
                message={
                  traceError === 'not_found'
                    ? t('aiObservability.traceNotFound')
                    : t('aiObservability.traceLoadFailed')
                }
              />
            ) : null}
            {trace ? <RunTraceView trace={trace} pageKey="ai.observability" /> : null}
            {conversationTraceError ? (
              <Alert
                type={conversationTraceError === 'not_found' ? 'warning' : 'error'}
                showIcon
                data-testid="observability-conversation-trace-error"
                message={
                  conversationTraceError === 'not_found'
                    ? t('aiObservability.conversationTraceNotFound')
                    : t('aiObservability.conversationTraceLoadFailed')
                }
              />
            ) : null}
            {conversationTrace ? (
              <ConversationTraceView trace={conversationTrace} pageKey="ai.observability" />
            ) : null}
          </Space>
        </Card>
      </Space>
    </div>
  );
};

export default AssistantObservabilityPage;