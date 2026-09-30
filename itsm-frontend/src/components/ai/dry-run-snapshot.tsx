/**
 * dry-run 预览快照（B1-09）。
 *
 * 语义：B0-04 的 dry-run 只落「预览记录」（不写业务数据），其快照（`result`）只在
 * **详情接口**（`GET /api/v1/agent/tools/:id`）返回；列表接口不承诺携带。
 * 因此本组件在展开行时按需拉取一次（用户触发，不轮询）。
 *
 * 脱敏边界：只展示 `argsRedacted` 与 `result`（后端已脱敏）；原始参数不回显。
 */
import React, { useEffect, useState } from 'react';
import { Alert, Space, Spin, Tag, Typography, theme } from 'antd';
import { FlaskConical } from 'lucide-react';

import { aiGetToolInvocation } from '@/lib/api/ai-api';

export interface DryRunSnapshotProps {
  /** tool_invocation 主键（dry-run 预览记录）。 */
  invocationId: number;
}

const pretty = (raw?: string | null): string => {
  if (!raw) return '';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
};

export const DryRunSnapshot: React.FC<DryRunSnapshotProps> = ({ invocationId }) => {
  const { token } = theme.useToken();
  const [loading, setLoading] = useState(true);
  const [result, setResult] = useState<string | null>(null);
  const [args, setArgs] = useState<string>('');
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(undefined);
    aiGetToolInvocation(invocationId)
      .then(detail => {
        if (cancelled) return;
        setResult(detail.result ?? null);
        setArgs(detail.argsRedacted ?? '');
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message || '预览快照加载失败');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [invocationId]);

  const preStyle: React.CSSProperties = {
    margin: '6px 0 0',
    padding: 8,
    borderRadius: 8,
    background: token.colorFillQuaternary,
    fontSize: 12,
    maxHeight: 180,
    overflow: 'auto',
    whiteSpace: 'pre-wrap',
  };

  return (
    <div data-testid={`dry-run-snapshot-${invocationId}`} style={{ marginTop: 12 }}>
      <Space size={6}>
        <Tag icon={<FlaskConical size={12} />} color="purple" style={{ marginInlineEnd: 0 }}>
          dry-run 预览
        </Tag>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          本次调用只生成预览记录，未写入业务数据。
        </Typography.Text>
      </Space>

      {loading ? (
        <div style={{ marginTop: 8 }}>
          <Spin size="small" /> <Typography.Text type="secondary">正在加载预览快照…</Typography.Text>
        </div>
      ) : null}

      {error ? (
        <Alert style={{ marginTop: 8 }} type="warning" showIcon message={`预览快照不可用：${error}`} />
      ) : null}

      {!loading && !error ? (
        <>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            预览结果
          </Typography.Text>
          <pre data-testid="dry-run-result" style={preStyle}>
            {pretty(result) || '（空快照）'}
          </pre>
          {args ? (
            <>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                脱敏参数
              </Typography.Text>
              <pre data-testid="dry-run-args" style={preStyle}>
                {pretty(args)}
              </pre>
            </>
          ) : null}
        </>
      ) : null}
    </div>
  );
};

export default DryRunSnapshot;
