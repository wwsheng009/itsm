/**
 * V0-02 员工侧自助问答（先自助 → 再建单）。
 *
 * 流程：描述问题 → 知识库检索（Top 3，带来源/匹配度）→
 * 「已解决（不用建单）」或「还没解决，去建单」（把问题描述带给建单表单，**不自动提交**）。
 *
 * 埋点（§13.2）：`self_service_answered`（命中/未命中）、`self_service_resolved`（自助解决率分子）、
 * `self_service_ticket_created`（转建单）。
 * 边界：不做任何写操作；命中与否都不自动建单，也不阻断建单路径。
 */
import React, { useCallback, useState } from 'react';
import { Alert, Button, Card, Input, Space, Tag, Typography, theme } from 'antd';
import { Search, ThumbsUp, Wrench } from 'lucide-react';

import { aiSearchKB, type RagAnswer } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

const { Paragraph, Text } = Typography;

export const SELF_SERVICE_TESTID = 'self-service-assistant';

interface SelfServiceAssistantProps {
  /** 「去建单」：把问题描述交给建单表单（只带入意图，不自动提交）。 */
  onProceedToCreate: (query: string) => void;
  /** 埋点用页面键（缺省不落字段）。 */
  pageKey?: string;
}

const SelfServiceAssistant: React.FC<SelfServiceAssistantProps> = ({
  onProceedToCreate,
  pageKey,
}) => {
  const { token } = theme.useToken();
  const [query, setQuery] = useState('');
  const [asked, setAsked] = useState('');
  const [answers, setAnswers] = useState<RagAnswer[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [resolved, setResolved] = useState(false);

  const handleAsk = useCallback(async () => {
    const text = query.trim();
    if (!text || loading) return;
    setLoading(true);
    setResolved(false);
    try {
      const { answers: found } = await aiSearchKB(text, 3);
      setAnswers(found);
      setAsked(text);
      trackAssistantEvent('self_service_answered', {
        pageKey,
        source: 'self_service',
        // query 为敏感 key：SDK 会哈希后再上报（元数据优先）。
        extra: { query: text, results: found.length },
      });
    } catch {
      setAnswers([]);
      setAsked(text);
      trackAssistantEvent('self_service_answered', {
        pageKey,
        source: 'self_service',
        errorCode: 'search_failed',
        extra: { query: text, results: 0 },
      });
    } finally {
      setLoading(false);
    }
  }, [query, loading, pageKey]);

  const handleResolved = useCallback(() => {
    setResolved(true);
    trackAssistantEvent('self_service_resolved', {
      pageKey,
      source: 'self_service',
      extra: { query: asked || query },
    });
  }, [asked, query, pageKey]);

  const handleProceed = useCallback(() => {
    const text = (asked || query).trim();
    trackAssistantEvent('self_service_ticket_created', {
      pageKey,
      source: 'self_service',
      extra: { query: text },
    });
    onProceedToCreate(text);
  }, [asked, query, pageKey, onProceedToCreate]);

  return (
    <Card data-testid={SELF_SERVICE_TESTID} title="先自助，再建单">
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          先查一下知识库：命中且解决则无需建单；未命中或未解决再建单（不会自动提交，提交前你可核对/修改）。
        </Text>

        <Space.Compact style={{ width: '100%' }}>
          <Input
            data-testid="self-service-input"
            value={query}
            placeholder="描述你遇到的问题，例如：VPN 连不上怎么办？"
            onChange={e => setQuery(e.target.value)}
            onPressEnter={() => void handleAsk()}
            allowClear
          />
          <Button
            type="primary"
            icon={<Search size={14} />}
            loading={loading}
            disabled={!query.trim()}
            onClick={() => void handleAsk()}
            data-testid="self-service-ask"
          >
            查询知识库
          </Button>
        </Space.Compact>

        {answers && answers.length > 0 ? (
          <div data-testid="self-service-answers" style={{ display: 'grid', gap: 8 }}>
            {answers.map(item => (
              <div
                key={`${item.objectType}-${item.id}`}
                data-testid={`self-service-answer-${item.id}`}
                style={{
                  padding: '8px 10px',
                  borderRadius: 8,
                  border: `1px solid ${token.colorBorderSecondary}`,
                  background: token.colorFillQuaternary,
                }}
              >
                <Space wrap size={6}>
                  <Text strong>{item.title || `知识 #${item.id}`}</Text>
                  {item.category ? <Tag>{item.category}</Tag> : null}
                  {typeof item.score === 'number' ? (
                    <Text type="secondary" style={{ fontSize: 11 }}>
                      匹配 {item.score.toFixed(2)}
                    </Text>
                  ) : null}
                </Space>
                <Paragraph style={{ fontSize: 12, margin: '4px 0 0', whiteSpace: 'pre-wrap' }}>
                  {item.snippet}
                </Paragraph>
              </div>
            ))}
            <Space wrap>
              <Button
                icon={<ThumbsUp size={14} />}
                onClick={handleResolved}
                data-testid="self-service-resolved"
              >
                已解决（不用建单）
              </Button>
              <Button
                type="primary"
                ghost
                icon={<Wrench size={14} />}
                onClick={handleProceed}
                data-testid="self-service-proceed"
              >
                还没解决，去建单
              </Button>
            </Space>
          </div>
        ) : null}

        {answers && answers.length === 0 ? (
          <div data-testid="self-service-empty">
            <Alert
              type="info"
              showIcon
              message="知识库暂未命中"
              description="可直接把问题带到下方建单表单，AI 会生成草稿（提交前你可核对/修改）。"
              action={
                <Button
                  size="small"
                  type="primary"
                  onClick={handleProceed}
                  data-testid="self-service-proceed-empty"
                >
                  去建单
                </Button>
              }
            />
          </div>
        ) : null}

        {resolved ? (
          <div data-testid="self-service-resolved-hint">
            <Alert type="success" showIcon message="已记录：本次自助解决（不建单）" />
          </div>
        ) : null}
      </Space>
    </Card>
  );
};

export default SelfServiceAssistant;