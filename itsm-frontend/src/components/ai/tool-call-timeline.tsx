/**
 * 对话内工具调用时间线（M1-04）。
 *
 * 数据来源：**仅 SSE**（M1-03 的 `tool_call_started/finished/failed/approval_pending`），
 * 不额外轮询、不读审计表——事件是过程信号，审计真源仍是 `tool_invocations`。
 *
 * 三条刻意设计：
 *  1. **事件配对而非按名合并**：`started` 开一条，后续同工具的终态事件闭合「最近一条未闭合」
 *     的调用。同一工具被调用两次会得到两条记录，不会互相覆盖。
 *  2. **事件丢失可降级**：只有终态事件（或缺 `started`）时照样渲染单条记录——绝不出现空白块；
 *     一条事件都没有时组件返回 `null`（由最终消息承载内容）。
 *  3. **工具输出按纯文本渲染**：工具返回值是不可信输入（外部 MCP 服务器可控），因此**不**交给
 *     Markdown 渲染器，而用 `<pre>` + React 文本转义（无 HTML 注入面，比 sanitize 链更小的攻击面）。
 */
import React, { useMemo, useState } from 'react';
import { Tag, Tooltip, Typography, theme } from 'antd';
import { AlertCircle, ChevronDown, ChevronRight, Clock, Loader2, ShieldAlert, Wrench } from 'lucide-react';

import type { AIToolStreamEvent } from '@/lib/api/ai-api';

/** 截断标记（与后端 `pkg/redact.TruncatedMarker` 同字面量；后端保证输出带该后缀）。 */
export const TRUNCATED_MARKER = '…(truncated)';

/** 时间线条目：`started` 与终态事件合并后的视图模型。 */
export interface ToolCallEntry {
  /** React key（工具名 + 序号，保证同名多次调用稳定区分）。 */
  key: string;
  tool: string;
  /** builtin | mcp（其余取值按 builtin 兜底展示）。 */
  provider: string;
  server?: string;
  /** read | write。 */
  phase: string;
  /** started | done | failed | pending。 */
  status: string;
  summary?: string;
  durationMs?: number;
  errorCode?: string;
  invocationId?: number;
}

const isTerminal = (status: string): boolean =>
  status === 'done' || status === 'failed' || status === 'pending';

/**
 * 把 SSE 事件流合并为时间线条目（纯函数，便于单测）。
 *
 * 顺序语义：`started` 追加新条目；终态事件优先闭合该工具最近一条「未闭合」条目，
 * 找不到可闭合条目时（事件丢失/乱序）自建一条终态条目，而不是丢弃。
 */
export const mergeToolEvents = (events: readonly AIToolStreamEvent[] = []): ToolCallEntry[] => {
  const entries: ToolCallEntry[] = [];
  const openIndex = new Map<string, number>(); // tool → 最近未闭合条目下标

  events.forEach((event, index) => {
    if (!event || typeof event.tool !== 'string' || event.tool.length === 0) return;
    const status = typeof event.status === 'string' && event.status ? event.status : 'started';
    const key = `${event.tool}#${index}`;

    if (status === 'started') {
      entries.push({
        key,
        tool: event.tool,
        provider: event.provider || 'builtin',
        server: event.server,
        phase: event.phase || 'read',
        status,
      });
      openIndex.set(event.tool, entries.length - 1);
      return;
    }

    const open = openIndex.get(event.tool);
    const target: ToolCallEntry | undefined = open === undefined ? undefined : entries[open];
    const merged: ToolCallEntry = {
      key: target?.key ?? key,
      tool: event.tool,
      provider: event.provider || target?.provider || 'builtin',
      server: event.server ?? target?.server,
      phase: event.phase || target?.phase || 'read',
      status,
      summary: event.summary ?? target?.summary,
      durationMs: event.durationMs ?? target?.durationMs,
      errorCode: event.errorCode,
      invocationId: event.id ?? target?.invocationId,
    };

    if (target) {
      entries[open as number] = merged;
      openIndex.delete(event.tool);
      return;
    }
    // 事件丢失/乱序：终态事件自建条目（降级渲染，不丢过程）。
    entries.push(merged);
  });

  return entries;
};

const STATUS_META: Record<string, { color: string; label: string }> = {
  started: { color: 'processing', label: '执行中' },
  done: { color: 'success', label: '已完成' },
  failed: { color: 'error', label: '失败' },
  pending: { color: 'warning', label: '待审批' },
  // 未知状态（后端新增阶段）按信息展示，不隐藏。
  unknown: { color: 'default', label: '进行中' },
};

const statusMeta = (status: string) => STATUS_META[status] ?? STATUS_META.unknown;

/** 来源徽标：内置 / MCP·服务器名（MCP 缺服务器名时退化为「MCP」）。 */
const sourceLabel = (entry: ToolCallEntry): string => {
  if (entry.provider === 'mcp') {
    return entry.server ? `MCP · ${entry.server}` : 'MCP';
  }
  return '内置';
};

const statusIcon = (status: string) => {
  switch (status) {
    case 'done':
      return <Wrench size={13} />;
    case 'failed':
      return <AlertCircle size={13} />;
    case 'pending':
      return <ShieldAlert size={13} />;
    default:
      return <Loader2 size={13} className="ai-tool-spin" />;
  }
};

const formatDuration = (ms?: number): string | undefined => {
  if (typeof ms !== 'number' || Number.isNaN(ms)) return undefined;
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
};

export interface ToolCallTimelineProps {
  events?: readonly AIToolStreamEvent[];
  /** 标题文案（默认「工具调用」）。 */
  title?: string;
}

/**
 * 工具调用时间线：每次调用一个折叠块（来源徽标 → 工具名 → 状态/耗时 → 脱敏摘要）。
 */
export const ToolCallTimeline: React.FC<ToolCallTimelineProps> = ({ events, title = '工具调用' }) => {
  const { token } = theme.useToken();
  const entries = useMemo(() => mergeToolEvents(events), [events]);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  // 无事件（旧后端 / 事件丢失 / 纯知识库问答）→ 不渲染任何容器，避免空白块。
  if (entries.length === 0) return null;

  const toggle = (key: string) =>
    setExpanded(prev => ({ ...prev, [key]: !prev[key] }));

  return (
    <div
      style={{
        marginTop: 10,
        padding: '8px 12px',
        borderRadius: 10,
        background: token.colorFillQuaternary,
        border: `1px solid ${token.colorBorderSecondary}`,
      }}
      data-testid="tool-call-timeline"
    >
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {title} · {entries.length}
      </Typography.Text>
      <ul style={{ listStyle: 'none', paddingLeft: 0, margin: '6px 0 0' }}>
        {entries.map(entry => {
          const meta = statusMeta(entry.status);
          const duration = formatDuration(entry.durationMs);
          const summary = entry.summary ?? '';
          const truncated = summary.includes(TRUNCATED_MARKER);
          const open = Boolean(expanded[entry.key]);
          const expandable = summary.length > 0 || Boolean(entry.errorCode) || entry.status === 'pending';
          return (
            <li key={entry.key} style={{ marginBottom: 4 }}>
              <div
                role="button"
                tabIndex={0}
                aria-expanded={expandable ? open : undefined}
                onClick={() => expandable && toggle(entry.key)}
                onKeyDown={e => {
                  if (!expandable) return;
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggle(entry.key);
                  }
                }}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  flexWrap: 'wrap',
                  cursor: expandable ? 'pointer' : 'default',
                  fontSize: 12,
                }}
              >
                {expandable ? (
                  open ? (
                    <ChevronDown size={12} />
                  ) : (
                    <ChevronRight size={12} />
                  )
                ) : (
                  <span style={{ width: 12 }} />
                )}
                <span style={{ color: token.colorTextSecondary, display: 'inline-flex', alignItems: 'center' }}>
                  {statusIcon(entry.status)}
                </span>
                <Tag
                  color={entry.provider === 'mcp' ? 'geekblue' : 'default'}
                  style={{ marginInlineEnd: 0 }}
                >
                  {sourceLabel(entry)}
                </Tag>
                <Typography.Text code style={{ fontSize: 12 }}>
                  {entry.tool}
                </Typography.Text>
                <Tag color={meta.color} style={{ marginInlineEnd: 0 }}>
                  {meta.label}
                </Tag>
                {duration ? (
                  <span
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 2,
                      color: token.colorTextSecondary,
                    }}
                  >
                    <Clock size={11} />
                    {duration}
                  </span>
                ) : null}
                {entry.phase === 'write' ? (
                  <Tag color="orange" style={{ marginInlineEnd: 0 }}>
                    写操作
                  </Tag>
                ) : null}
              </div>

              {open ? (
                <div style={{ marginTop: 4, paddingLeft: 18 }} data-testid={`tool-call-detail-${entry.tool}`}>
                  {entry.status === 'pending' ? (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      已提交人工审批{entry.invocationId ? `（#${entry.invocationId}）` : ''}，审批通过后执行；可在「AI 审批」页处理。
                    </Typography.Text>
                  ) : null}
                  {entry.errorCode ? (
                    <div style={{ fontSize: 12, color: token.colorError }}>错误码：{entry.errorCode}</div>
                  ) : null}
                  {summary ? (
                    <div style={{ marginTop: entry.errorCode || entry.status === 'pending' ? 4 : 0 }}>
                      {truncated ? (
                        <Tooltip title="输出超过长度上限，已截断">
                          <Tag color="default" style={{ marginInlineEnd: 6, fontSize: 11 }}>
                            已截断
                          </Tag>
                        </Tooltip>
                      ) : null}
                      {/* 纯文本渲染：工具输出不可信，绝不经 Markdown/HTML 解析。 */}
                      <pre
                        style={{
                          margin: 0,
                          maxHeight: 180,
                          overflow: 'auto',
                          whiteSpace: 'pre-wrap',
                          wordBreak: 'break-all',
                          fontSize: 12,
                          color: token.colorTextSecondary,
                        }}
                      >
                        {summary}
                      </pre>
                    </div>
                  ) : entry.status === 'done' ? (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      （无摘要）
                    </Typography.Text>
                  ) : null}
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>
      <style>{'.ai-tool-spin { animation: ai-tool-spin 1s linear infinite; } @keyframes ai-tool-spin { to { transform: rotate(360deg); } }'}</style>
    </div>
  );
};

export default ToolCallTimeline;
