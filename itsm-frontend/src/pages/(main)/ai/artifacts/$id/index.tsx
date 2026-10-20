/**
 * I0-04 产物详情（结果落点「打开」的真实目标）。
 *
 * 数据源：`GET /api/v1/ai/artifacts/:id`（只读；租户 + 发起人收敛，越权/不存在统一 404）。
 * 边界：本页只展示，不写业务表；帮助草稿的提交/发布仍在「帮助覆盖率」审阅队列完成。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Descriptions, Space, Spin, Tag, Typography } from 'antd';
import { ArrowLeft, Copy, ExternalLink } from 'lucide-react';
import { useNavigate, useParams } from 'react-router';

import { aiGetArtifact, type AIArtifactDetail } from '@/lib/api/ai-api';

const { Title, Text, Paragraph } = Typography;

const KIND_META: Record<string, { color: string; label: string }> = {
  plan: { color: 'gold', label: '计划' },
  analysis: { color: 'geekblue', label: '分析' },
  draft: { color: 'purple', label: '草稿' },
};

const prettyJson = (value: unknown): string => {
  if (value === null || value === undefined) return '';
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
};

/** 从内容里提取一段人类可读摘要（标题之外的第一层结构说明）。 */
const summarizeContent = (value: unknown): string | undefined => {
  if (!value || typeof value !== 'object') return undefined;
  const record = value as Record<string, unknown>;
  for (const key of ['summary', 'description', 'note', 'title']) {
    const candidate = record[key];
    if (typeof candidate === 'string' && candidate.trim()) return candidate.trim();
  }
  return undefined;
};

const ArtifactDetailPage: React.FC = () => {
  const navigate = useNavigate();
  const params = useParams();
  const id = Number.parseInt(params.id ?? '', 10);
  const validId = Number.isFinite(id) && id > 0;

  const [detail, setDetail] = useState<AIArtifactDetail | null>(null);
  const [loading, setLoading] = useState(validId);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    if (!validId) return;
    setLoading(true);
    setError(null);
    try {
      setDetail(await aiGetArtifact(id));
    } catch (err) {
      // 404 与网络失败同一提示口径：不泄露资源是否存在（避免枚举）。
      setError((err as Error)?.message || '产物不存在或无权访问');
      setDetail(null);
    } finally {
      setLoading(false);
    }
  }, [id, validId]);

  useEffect(() => {
    void load();
  }, [load]);

  const kindMeta = detail ? KIND_META[detail.kind] ?? { color: 'default', label: detail.kind } : null;
  const contentText = useMemo(() => prettyJson(detail?.content), [detail?.content]);
  const evidenceText = useMemo(() => prettyJson(detail?.evidence), [detail?.evidence]);
  const summary = useMemo(() => summarizeContent(detail?.content), [detail?.content]);

  const handleCopy = useCallback(async () => {
    if (!contentText || !navigator.clipboard?.writeText) return;
    try {
      await navigator.clipboard.writeText(contentText);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // 剪贴板不可用：保持只读展示，不打断阅读。
    }
  }, [contentText]);

  return (
    <div data-testid="ai-artifact-page" style={{ padding: 24, maxWidth: 1080 }}>
      <Space align="center" style={{ marginBottom: 16 }} wrap>
        <Button
          type="text"
          icon={<ArrowLeft size={16} />}
          onClick={() => navigate(-1)}
          data-testid="ai-artifact-back"
        >
          返回
        </Button>
        <Title level={4} style={{ margin: 0 }}>
          {detail?.title || 'AI 产物'}
        </Title>
        {kindMeta ? <Tag color={kindMeta.color}>{kindMeta.label}</Tag> : null}
      </Space>

      {!validId ? (
        <Alert type="error" showIcon message="产物编号无效" data-testid="ai-artifact-invalid" />
      ) : null}
      {loading ? <Spin data-testid="ai-artifact-loading" /> : null}
      {!loading && error ? (
        <Alert
          type="warning"
          showIcon
          message={error}
          data-testid="ai-artifact-error"
          action={
            <Button size="small" onClick={() => void load()}>
              重试
            </Button>
          }
        />
      ) : null}

      {!loading && detail ? (
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          {summary ? <Paragraph type="secondary">{summary}</Paragraph> : null}

          <Card size="small" title="产物信息">
            <Descriptions size="small" column={2} data-testid="ai-artifact-meta">
              <Descriptions.Item label="类型">{kindMeta?.label ?? detail.kind}</Descriptions.Item>
              <Descriptions.Item label="产出工具">{detail.toolName || '—'}</Descriptions.Item>
              <Descriptions.Item label="创建时间">{detail.createdAt || '—'}</Descriptions.Item>
              <Descriptions.Item label="来源会话">
                {detail.conversationId ? `#${detail.conversationId}` : '—'}
              </Descriptions.Item>
            </Descriptions>
          </Card>

          {contentText ? (
            <Card
              size="small"
              title="内容"
              extra={
                <Space size={4}>
                  <Button
                    size="small"
                    type="text"
                    icon={<Copy size={13} />}
                    onClick={() => void handleCopy()}
                    data-testid="ai-artifact-copy-content"
                  >
                    {copied ? '已复制' : '复制'}
                  </Button>
                  <Button
                    size="small"
                    type="text"
                    icon={<ExternalLink size={13} />}
                    onClick={() => navigate('/ai/chat')}
                    data-testid="ai-artifact-open-chat"
                  >
                    回到对话
                  </Button>
                </Space>
              }
            >
              <pre
                data-testid="ai-artifact-content"
                style={{
                  margin: 0,
                  maxHeight: '52vh',
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  fontSize: 12,
                }}
              >
                {contentText}
              </pre>
            </Card>
          ) : (
            <Alert type="info" showIcon message="该产物没有可展示的内容" data-testid="ai-artifact-empty" />
          )}

          {evidenceText ? (
            <Card size="small" title="证据/依据">
              <Text type="secondary" style={{ fontSize: 12 }}>
                证据由产出工具写入，仅作核验线索。
              </Text>
              <pre
                data-testid="ai-artifact-evidence"
                style={{
                  margin: '8px 0 0',
                  maxHeight: 240,
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  fontSize: 12,
                }}
              >
                {evidenceText}
              </pre>
            </Card>
          ) : null}
        </Space>
      ) : null}
    </div>
  );
};

export default ArtifactDetailPage;
