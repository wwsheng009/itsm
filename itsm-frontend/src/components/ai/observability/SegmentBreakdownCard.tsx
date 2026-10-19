/**
 * O2-01 分群对比卡（挂 `/ai/observability`）：按角色/页面维度找「哪里对用户真正有用」。
 *
 * 指标子集：建议 CTR、任务完成率、Inline 采纳率、错误率（分母为 0 → 服务端返回 0）。
 * 维度白名单 role | pageKey；窗口跟随看板 `days`。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Card, Segmented, Space, Table, Typography } from 'antd';

import { aiGetAssistantSegments, type AssistantSegment } from '@/lib/api/ai-api';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

export const SEGMENT_BREAKDOWN_CARD_TESTID = 'segment-breakdown-card';

type SegmentDimension = 'role' | 'pageKey';

interface SegmentBreakdownCardProps {
  days: number;
}

const fmtRate = (value: number): string => `${(value * 100).toFixed(1)}%`;

const SegmentBreakdownCard: React.FC<SegmentBreakdownCardProps> = ({ days }) => {
  const { t } = useI18n();
  const [dimension, setDimension] = useState<SegmentDimension>('role');
  const [segments, setSegments] = useState<AssistantSegment[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [disabled, setDisabled] = useState(false);

  const load = useCallback(
    async (windowDays: number, dim: SegmentDimension) => {
      setLoading(true);
      setError(null);
      try {
        const report = await aiGetAssistantSegments({ dimension: dim, days: windowDays });
        setSegments(report.segments ?? []);
        setDisabled(false);
      } catch (err) {
        const status = (err as { response?: { status?: number }; status?: number })?.response?.status
          ?? (err as { status?: number })?.status;
        if (status === 503 || status === 404) {
          setDisabled(true);
          setSegments([]);
        } else {
          setSegments([]);
          setError(t('aiObservability.segmentLoadFailed'));
        }
      } finally {
        setLoading(false);
      }
    },
    [t]
  );

  useEffect(() => {
    void load(days, dimension);
  }, [days, dimension, load]);

  const columns = useMemo(
    () => [
      { title: t('aiObservability.segmentColKey'), dataIndex: 'key', key: 'key' },
      {
        title: t('aiObservability.segmentColEvents'),
        dataIndex: 'events',
        key: 'events',
        width: 90,
      },
      {
        title: t('aiObservability.segmentColSessions'),
        dataIndex: 'sessions',
        key: 'sessions',
        width: 90,
      },
      {
        title: t('aiObservability.segmentColSuggestionCtr'),
        dataIndex: 'suggestionCtr',
        key: 'suggestionCtr',
        width: 110,
        render: (value: number) => fmtRate(value),
      },
      {
        title: t('aiObservability.segmentColTaskCompletion'),
        dataIndex: 'taskCompletionRate',
        key: 'taskCompletionRate',
        width: 120,
        render: (value: number) => fmtRate(value),
      },
      {
        title: t('aiObservability.segmentColInlineAdoption'),
        dataIndex: 'inlineAdoptionRate',
        key: 'inlineAdoptionRate',
        width: 120,
        render: (value: number) => fmtRate(value),
      },
      {
        title: t('aiObservability.segmentColErrorRate'),
        dataIndex: 'errorRate',
        key: 'errorRate',
        width: 100,
        render: (value: number) => fmtRate(value),
      },
    ],
    [t]
  );

  return (
    <Card
      data-testid={SEGMENT_BREAKDOWN_CARD_TESTID}
      title={t('aiObservability.segmentTitle')}
      extra={
        <Segmented
          data-testid="segment-dimension"
          size="small"
          value={dimension}
          onChange={value => setDimension(value as SegmentDimension)}
          options={[
            { label: t('aiObservability.segmentDimensionRole'), value: 'role' },
            { label: t('aiObservability.segmentDimensionPage'), value: 'pageKey' },
          ]}
        />
      }
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('aiObservability.segmentDescription')}
        </Text>
        {disabled ? (
          <Alert
            type="info"
            showIcon
            data-testid="segment-disabled"
            message={t('aiObservability.disabledTitle')}
          />
        ) : null}
        {error ? (
          <Alert type="warning" showIcon data-testid="segment-error" message={error} />
        ) : null}
        {!disabled && !error && segments.length === 0 && !loading ? (
          <Alert
            type="info"
            showIcon
            data-testid="segment-empty"
            message={t('aiObservability.empty')}
          />
        ) : null}
        <Table<AssistantSegment>
          data-testid="segment-table"
          rowKey="key"
          size="small"
          loading={loading}
          dataSource={segments}
          columns={columns}
          pagination={false}
        />
      </Space>
    </Card>
  );
};

export default SegmentBreakdownCard;