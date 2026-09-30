/**
 * 运行状态条（B1-08）。
 *
 * 数据来源：v2 事件（`run_started` / `step` / `done` / `error`，B1-03 注册表）。
 * 旧后端不产生 v2 事件 → `info` 保持 undefined，组件不渲染（渲染零影响）。
 *
 * 诚实边界：预算消耗仅在调用方已知上限时展示（事件只带步骤索引与耗时，
 * 不含预算计数）；未知时不渲染该段，不臆造上限。
 */
import React, { useEffect, useState } from 'react';
import { Space, Tag, Typography, theme } from 'antd';
import { Activity, AlertTriangle, CheckCircle2, Timer } from 'lucide-react';

/** AIChat 每条助手消息持有的运行快照（由 v2 事件增量维护）。 */
export interface RunSnapshot {
  runId?: number;
  status: 'running' | 'done' | 'failed';
  providerLabel?: string;
  stepCount?: number;
  lastStepType?: string;
  lastDurationMs?: number;
  /** 运行开始时间（ms epoch）：用于 running 态计时。 */
  startedAt?: number;
  /** 预算消耗（调用方已知时传入；未知则不渲染）。 */
  budget?: { stepsUsed?: number; stepsLimit?: number; toolCallsUsed?: number; toolCallsLimit?: number };
}

/** 耗时格式化（毫秒 → 秒，保留一位；<=0/缺省返回 null）。 */
export const formatDuration = (ms: number | undefined): string | null =>
  typeof ms === 'number' && ms > 0 ? `${(ms / 1000).toFixed(1)}s` : null;

/** 已运行时长（秒 → 「N 分 M 秒」/「M 秒」）。 */
export const formatElapsed = (startedAt: number | undefined, nowMs: number): string => {
  if (!startedAt) return '';
  const total = Math.max(0, Math.floor((nowMs - startedAt) / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return minutes > 0 ? `${minutes} 分 ${seconds} 秒` : `${seconds} 秒`;
};

export interface RunStatusBarProps {
  info?: RunSnapshot;
  /** 当前时间注入（单测；默认 Date.now）。 */
  now?: () => number;
  /** running 态计时刷新间隔（ms；默认 1000）。 */
  tickMs?: number;
}

/** 运行状态条：run 编号、状态、生效实例、步骤数、最近步骤耗时、预算消耗（有则显示）。 */
export const RunStatusBar: React.FC<RunStatusBarProps> = ({ info, now = Date.now, tickMs = 1000 }) => {
  const { token } = theme.useToken();
  const [tick, setTick] = useState(() => now());

  useEffect(() => {
    if (!info || info.status !== 'running') return undefined;
    const timer = window.setInterval(() => setTick(now()), tickMs);
    return () => window.clearInterval(timer);
  }, [info, info?.status, now, tickMs]);

  if (!info) return null;

  const meta =
    info.status === 'running'
      ? { color: 'processing', label: '运行中', icon: <Activity size={12} /> }
      : info.status === 'done'
        ? { color: 'success', label: '已完成', icon: <CheckCircle2 size={12} /> }
        : { color: 'error', label: '已中断', icon: <AlertTriangle size={12} /> };

  const lastDuration = formatDuration(info.lastDurationMs);
  const elapsed = info.status === 'running' ? formatElapsed(info.startedAt, tick) : '';

  return (
    <div
      data-testid="run-status-bar"
      style={{
        marginTop: 10,
        padding: '6px 10px',
        borderRadius: 8,
        border: `1px solid ${token.colorBorderSecondary}`,
        background: token.colorFillQuaternary,
      }}
    >
      <Space size={8} wrap style={{ fontSize: 12 }}>
        <Tag icon={meta.icon} color={meta.color} style={{ marginInlineEnd: 0 }}>
          {meta.label}
        </Tag>
        <Typography.Text code style={{ fontSize: 12 }}>
          run#{info.runId ?? '—'}
        </Typography.Text>
        {info.providerLabel ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {info.providerLabel}
          </Typography.Text>
        ) : null}
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          步骤 {info.stepCount ?? 0}
          {info.lastStepType ? ` · 最近 ${info.lastStepType}` : ''}
        </Typography.Text>
        {lastDuration ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            <Timer size={12} style={{ verticalAlign: -2, marginRight: 2 }} />
            {lastDuration}
          </Typography.Text>
        ) : null}
        {elapsed ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            已运行 {elapsed}
          </Typography.Text>
        ) : null}
        {info.budget && (info.budget.stepsLimit || info.budget.toolCallsLimit) ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            预算：步数 {info.budget.stepsUsed ?? 0}/{info.budget.stepsLimit ?? '—'} · 工具{' '}
            {info.budget.toolCallsUsed ?? 0}/{info.budget.toolCallsLimit ?? '—'}
          </Typography.Text>
        ) : null}
      </Space>
    </div>
  );
};

export default RunStatusBar;
