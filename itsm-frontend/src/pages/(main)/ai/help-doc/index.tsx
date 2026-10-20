/**
 * I0-04 帮助文档详情（引用来源「点了有内容」的落点）。
 *
 * 数据源：`GET /api/v1/ai/help/doc?path=docs/help/...`（只读内嵌帮助索引）。
 * 来源行展示剥离 `docs/help/` 前缀与 `.md` 后缀；完整路径只在 URL 中保留。
 */
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Space, Spin, Tag, Typography } from 'antd';
import { ArrowLeft } from 'lucide-react';
import { useNavigate, useSearchParams } from 'react-router';

import MarkdownMessage from '@/components/ai/MarkdownMessage';
import { aiGetHelpDoc, type HelpDoc } from '@/lib/api/ai-api';
import { stripDocPath } from '@/lib/ai/citations';

const { Title, Text } = Typography;

const HelpDocPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const path = searchParams.get('path') ?? '';
  const [doc, setDoc] = useState<HelpDoc | null>(null);
  const [loading, setLoading] = useState(Boolean(path));
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!path) return;
    setLoading(true);
    setError(null);
    try {
      setDoc(await aiGetHelpDoc(path));
    } catch (err) {
      setError((err as Error)?.message || '帮助文档不存在');
      setDoc(null);
    } finally {
      setLoading(false);
    }
  }, [path]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div data-testid="ai-help-doc-page" style={{ padding: 24, maxWidth: 900 }}>
      <Space align="center" style={{ marginBottom: 12 }} wrap>
        <Button type="text" icon={<ArrowLeft size={16} />} onClick={() => navigate(-1)}>
          返回
        </Button>
        <Title level={4} style={{ margin: 0 }}>
          {doc?.title || stripDocPath(path) || '帮助文档'}
        </Title>
        {doc?.pageKeys?.length ? <Tag>{doc.pageKeys[0]}</Tag> : null}
      </Space>

      {!path ? (
        <Alert type="warning" showIcon message="缺少文档路径" data-testid="ai-help-doc-missing-path" />
      ) : null}
      {loading ? <Spin data-testid="ai-help-doc-loading" /> : null}
      {!loading && error ? (
        <Alert
          type="warning"
          showIcon
          message={error}
          data-testid="ai-help-doc-error"
          action={
            <Button size="small" onClick={() => void load()}>
              重试
            </Button>
          }
        />
      ) : null}

      {!loading && doc ? (
        <Card size="small">
          <Text type="secondary" style={{ fontSize: 12 }} data-testid="ai-help-doc-source">
            {stripDocPath(doc.path)}
          </Text>
          <div style={{ marginTop: 8 }}>
            <MarkdownMessage content={doc.text} />
          </div>
        </Card>
      ) : null}
    </div>
  );
};

export default HelpDocPage;
