/**
 * H0-04 面板帮助视图：当前页帮助（状态 + 文档正文）/ 帮助检索 / 缺失引导。
 *
 * 数据源：`GET /ai/help/page?pageKey=`、`GET /ai/help/search?q=&pageKey=`
 *（内嵌索引来自 docs/help 真源；H0-03 数据面）。
 *
 * 缺失/过时 → 「让 AI 生成初稿」：切回对话页签并发送结构化起草提示
 *（提示要求参考同类文档结构并调用 help_create_draft 保存草稿；草稿只落产物、不发布）。
 */
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Input, Space, Spin, Tag, Typography, theme } from 'antd';
import { FileText, RefreshCw, Search } from 'lucide-react';

import {
  aiGetPageHelp,
  aiSearchHelp,
  type HelpDistributedSearchResult,
  type HelpPageResult,
  type HelpSearchResult,
} from '@/lib/api/ai-api';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useI18n } from '@/lib/i18n/useI18n';
import MarkdownMessage from '../MarkdownMessage';

const { Text } = Typography;

const STATUS_META: Record<string, { color: string; labelKey: string }> = {
  ok: { color: 'green', labelKey: 'assistant.help.statusOk' },
  stale: { color: 'orange', labelKey: 'assistant.help.statusStale' },
  missing: { color: 'red', labelKey: 'assistant.help.statusMissing' },
  unknown: { color: 'default', labelKey: 'assistant.help.statusUnknown' },
};

export interface AssistantHelpPanelProps {
  /** 切回对话并发送起草提示（提示词由本组件组装，含 pageKey 与结构要求）。 */
  onDraft: (prompt: string) => void;
}

const AssistantHelpPanel: React.FC<AssistantHelpPanelProps> = ({ onDraft }) => {
  const { token } = theme.useToken();
  const { t } = useI18n();
  const { pageKey, route, declaration } = usePageAssistantContext();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<HelpPageResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [results, setResults] = useState<HelpSearchResult[] | null>(null);
  const [searchError, setSearchError] = useState<string | null>(null);
  // P2：服务商（MSP）分发帮助的检索结果（与内嵌结果分开来源标注）。
  const [distributedResults, setDistributedResults] = useState<HelpDistributedSearchResult[]>([]);
  // P2：用户显式反馈（已解决/未解决）→ doc_feedback 事件（反馈解决率口径）。
  const [feedback, setFeedback] = useState<'helpful' | 'unhelpful' | null>(null);
  // P2：帮助打开去重（同一 pageKey 每次挂载只上报一次 help_opened）。
  const openedRef = useRef<Set<string>>(new Set());

  const load = useCallback(async () => {
    if (!pageKey || pageKey === 'unknown') {
      setData(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      setData(await aiGetPageHelp(pageKey));
    } catch (err) {
      setError((err as Error)?.message || t('assistant.help.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [pageKey, t]);

  useEffect(() => {
    void load();
  }, [load]);

  // 路由切换：清空检索态（避免把上一页的检索结果当前页展示）。
  useEffect(() => {
    setQuery('');
    setResults(null);
    setDistributedResults([]);
    setSearchError(null);
    setFeedback(null);
  }, [pageKey]);

  // H1-01：帮助缺口上报（missing/stale 每次加载一轮；pageKey 维度聚类 → 文档待办队列 /ai/help/todos）。
  useEffect(() => {
    if (!data || !pageKey || pageKey === 'unknown') return;
    if (data.status !== 'missing' && data.status !== 'stale') return;
    trackAssistantEvent('help_gap', {
      pageKey,
      route,
      coverageStatus: data.status,
      extra: { hasDocs: data.docs.length > 0 },
    });
  }, [data, pageKey, route]);

  // P2：帮助打开上报（文档命中率/解决率运营报表的会话分母；同一 pageKey 只报一次）。
  useEffect(() => {
    if (!data || !pageKey || pageKey === 'unknown') return;
    if (openedRef.current.has(pageKey)) return;
    openedRef.current.add(pageKey);
    trackAssistantEvent('help_opened', {
      pageKey,
      route,
      coverageStatus: data.status,
      extra: { docCount: data.docs.length + (data.distributedDocs?.length ?? 0) },
    });
  }, [data, pageKey, route]);

  const handleSearch = useCallback(async (value: string) => {
    const q = value.trim();
    if (!q) {
      setResults(null);
      setDistributedResults([]);
      return;
    }
    setSearching(true);
    setSearchError(null);
    try {
      const res = await aiSearchHelp(q, undefined, 5);
      setResults(res.results ?? []);
      setDistributedResults(res.distributedResults ?? []);
    } catch (err) {
      setSearchError((err as Error)?.message || t('assistant.help.searchFailed'));
    } finally {
      setSearching(false);
    }
  }, [t]);

  // P2：文档反馈（每条文档视图最多一次）——「已解决」= 命中且解决；「未解决」= 需转人工/建单。
  const handleFeedback = useCallback(
    (helpful: boolean) => {
      if (feedback) return;
      setFeedback(helpful ? 'helpful' : 'unhelpful');
      trackAssistantEvent('doc_feedback', {
        pageKey,
        route,
        extra: {
          helpful,
          docCount: (data?.docs.length ?? 0) + (data?.distributedDocs?.length ?? 0),
        },
      });
    },
    [feedback, pageKey, route, data]
  );

  // 加载中/首帧不得显示「未注册页面」：只有确认 pageKey 缺失时才是 unknown，
  // 已注册页面在首帧应显示「查询中」（避免把"还没拉到"谎报为"页面未注册"）。
  const status = data?.status ?? 'unknown';
  const meta = loading
    ? { color: 'processing', labelKey: 'assistant.help.statusQuerying' }
    : !pageKey || pageKey === 'unknown'
      ? STATUS_META.unknown
      : (STATUS_META[status] ?? STATUS_META.unknown);
  const canDraft = status === 'missing' || status === 'stale';

  const buildDraftPrompt = useCallback(() => {
    const title = declaration?.title || data?.title || pageKey;
    const reason = status === 'stale' ? '当前文档已过时' : '当前缺少帮助文档';
    return (
      `「帮助文档起草」页面「${title}」（pageKey=${pageKey}）${reason}。请：` +
      '1）用 help_search/help_read_doc 参考同类页面文档结构；' +
      '2）在回答中给出可采用的 Markdown 草稿（适用场景 / 关键概念 / 常用操作 / 常见问题 / AI 助手可用动作）；' +
      `3）如工具可用，调用 help_create_draft 保存草稿（pageKey=${pageKey}），不要发布。`
    );
  }, [declaration?.title, data?.title, pageKey, status]);

  return (
    <div
      data-testid="assistant-help-panel"
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        background: token.colorBgContainer,
      }}
    >
      <div
        style={{
          flexShrink: 0,
          padding: '8px 12px',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
        }}
      >
        <Space size={6}>
          <FileText size={14} color={token.colorTextSecondary} />
          <Text strong style={{ fontSize: 13 }}>
            {t('assistant.help.title')}
          </Text>
          <Tag color={meta.color} data-testid="help-status" style={{ marginInlineEnd: 0 }}>
            {t(meta.labelKey)}
          </Tag>
          {data?.coverage ? (
            <Text type="secondary" style={{ fontSize: 11 }} data-testid="help-coverage">
              {t('assistant.help.coverage', { pct: (data.coverage.coverage * 100).toFixed(0) })}
            </Text>
          ) : null}
        </Space>
        <span style={{ flex: 1 }} />
        <Button
          data-testid="help-refresh"
          type="text"
          size="small"
          icon={<RefreshCw size={13} />}
          onClick={() => void load()}
          aria-label={t('assistant.help.refresh')}
        />
      </div>

      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '10px 12px' }}>
        {loading ? <Spin size="small" /> : null}
        {error ? (
          <Alert type="error" showIcon message={error} data-testid="help-error" />
        ) : null}

        {!loading && !error && data && data.docs.length > 0 ? (
          <div data-testid="help-doc">
            {data.docs.map(doc => (
              <div key={doc.path}>
                <Text type="secondary" style={{ fontSize: 11 }}>
                  {doc.title} · {doc.path}
                </Text>
                <MarkdownMessage content={doc.text} />
              </div>
            ))}
          </div>
        ) : null}

        {/* P2：服务商分发帮助（copy-in 快照；active 跟随 provider / unbound 转客户原生副本） */}
        {!loading && !error && data && (data.distributedDocs?.length ?? 0) > 0 ? (
          <div
            data-testid="help-distributed-doc"
            style={{ marginTop: data.docs.length > 0 ? 10 : 0 }}
          >
            {(data.distributedDocs ?? []).map(doc => (
              <div
                key={`${doc.sourceSlug}:${doc.sourceVersion}`}
                data-testid={`help-distributed-doc-${doc.sourceSlug}`}
                style={{ marginBottom: 8 }}
              >
                <Space size={6} wrap>
                  <Tag color="purple" style={{ marginInlineEnd: 0 }}>
                    {t('assistant.help.distributed')}
                  </Tag>
                  {doc.status === 'unbound' ? (
                    <Tag style={{ marginInlineEnd: 0 }}>{t('assistant.help.unboundNative')}</Tag>
                  ) : null}
                  <Text type="secondary" style={{ fontSize: 11 }}>
                    {doc.title} · {doc.sourceSlug} v{doc.sourceVersion}
                  </Text>
                </Space>
                <MarkdownMessage content={doc.text} />
              </div>
            ))}
          </div>
        ) : null}

        {!loading && !error && data && (data.docs.length > 0 || (data.distributedDocs?.length ?? 0) > 0) ? (
          <div
            data-testid="help-doc-feedback"
            style={{ marginTop: 10, display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}
          >
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('assistant.help.feedbackQuestion')}
            </Text>
            <Button
              size="small"
              data-testid="help-feedback-helpful"
              disabled={feedback !== null}
              onClick={() => handleFeedback(true)}
            >
              {t('assistant.help.helpful')}
            </Button>
            <Button
              size="small"
              data-testid="help-feedback-unhelpful"
              disabled={feedback !== null}
              onClick={() => handleFeedback(false)}
            >
              {t('assistant.help.unhelpful')}
            </Button>
            {feedback ? (
              <Text type="secondary" style={{ fontSize: 12 }} data-testid="help-feedback-thanks">
                {t('assistant.help.feedbackThanks')}
              </Text>
            ) : null}
          </div>
        ) : null}

        {!loading && !error && canDraft ? (
          <Alert
            type="warning"
            showIcon
            message={t(status === 'stale' ? 'assistant.help.staleAlert' : 'assistant.help.missingAlert')}
            description={t('assistant.help.draftAlertDescription')}
            data-testid="help-missing-alert"
            action={
              <Button
                size="small"
                type="primary"
                data-testid="help-generate-draft"
                onClick={() => onDraft(buildDraftPrompt())}
              >
                {t('assistant.help.generateDraft')}
              </Button>
            }
            style={{ marginTop: data?.docs.length ? 12 : 0 }}
          />
        ) : null}

        {!loading && !error && !data && pageKey === 'unknown' ? (
          <Alert
            type="info"
            showIcon
            message={t('assistant.help.unknownPage')}
            description={
              <div>
                <div>{t('assistant.help.unknownPageDescription')}</div>
                <div
                  data-testid="help-recommended-topics"
                  style={{ marginTop: 8, display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}
                >
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {t('assistant.help.recommendedTopics')}
                  </Text>
                  {[
                    ['assistant.help.topicAccount', '账号开通'],
                    ['assistant.help.topicDevice', '设备故障'],
                    ['assistant.help.topicProgress', '请求进度'],
                  ].map(([key, fallback]) => {
                    const topic = t(key) || fallback;
                    return (
                      <Button
                        key={key}
                        size="small"
                        data-testid={`help-topic-${key.split('.').pop()}`}
                        onClick={() => {
                          setQuery(topic);
                          void handleSearch(topic);
                        }}
                      >
                        {topic}
                      </Button>
                    );
                  })}
                </div>
              </div>
            }
            data-testid="help-unknown-page"
          />
        ) : null}
      </div>

      <div
        style={{
          flexShrink: 0,
          padding: '8px 12px',
          borderTop: `1px solid ${token.colorBorderSecondary}`,
        }}
      >
        <Input.Search
          data-testid="help-search-input"
          placeholder={t('assistant.help.searchPlaceholder')}
          value={query}
          onChange={e => setQuery(e.target.value)}
          onSearch={value => void handleSearch(value)}
          enterButton={<Search size={14} />}
          loading={searching}
          allowClear
        />
        {searchError ? (
          <Alert
            type="error"
            showIcon
            message={searchError}
            data-testid="help-search-error"
            style={{ marginTop: 6 }}
          />
        ) : null}
        {results ? (
          <div
            data-testid="help-search-results"
            style={{ marginTop: 8, maxHeight: 180, overflowY: 'auto' }}
          >
            {results.length === 0 && distributedResults.length === 0 ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                {t('assistant.help.noResults')}
              </Text>
            ) : (
              results.map(item => (
                <div
                  key={item.path}
                  data-testid={`help-result-${item.path}`}
                  style={{
                    padding: '6px 0',
                    borderBottom: `1px dashed ${token.colorBorderSecondary}`,
                  }}
                >
                  <Text strong style={{ fontSize: 12 }}>
                    {item.title}
                  </Text>
                  <div>
                    <Text type="secondary" style={{ fontSize: 11 }}>
                      {item.path}
                    </Text>
                  </div>
                  <div style={{ fontSize: 12, color: token.colorTextSecondary }}>{item.snippet}</div>
                </div>
              ))
            )}
            {/* P2：服务商分发的检索结果（来源标注，与内嵌结果分区展示） */}
            {distributedResults.length > 0 ? (
              <div data-testid="help-distributed-search-results" style={{ marginTop: 8 }}>
                {distributedResults.map(item => (
                  <div
                    key={`${item.sourceSlug}:${item.sourceVersion}`}
                    data-testid={`help-distributed-result-${item.sourceSlug}`}
                    style={{
                      padding: '6px 0',
                      borderBottom: `1px dashed ${token.colorBorderSecondary}`,
                    }}
                  >
                    <Space size={6} wrap>
                      <Tag color="purple" style={{ marginInlineEnd: 0 }}>
                        {t('assistant.help.distributed')}
                      </Tag>
                      <Text strong style={{ fontSize: 12 }}>
                        {item.title}
                      </Text>
                    </Space>
                    <div style={{ fontSize: 12, color: token.colorTextSecondary }}>{item.snippet}</div>
                  </div>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default AssistantHelpPanel;