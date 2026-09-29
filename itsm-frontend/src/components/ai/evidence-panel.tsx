/**
 * 证据/时间线面板（B1-08）。运行状态条见同目录 `run-status-bar.tsx`。
 *
 * 数据来源与边界：
 *   - 运行态来自 v2 事件（`run_started` / `step`，B1-03 注册表；对象载荷带 `v:2`）；
 *   - 工具行来自 `tool_call_*` 事件（M1-03 契约），配对规则复用 `mergeToolEvents`（与 M1-05 卡片一致）；
 *   - `targetType/targetId/supportRef` **不在 SSE 事件里**：由调用方（AIChat）从已拉取的 invoke 详情汇总传入
 *     `detailsByInvocation`（卡片 `onLoaded` 即产出，零额外请求）；缺失时该行不渲染跳转/引用，不臆造。
 *   - 跳转 URL 仅对**已核实存在**的路由生成（tickets/incidents/cmdb cis 详情），未知类型退化为纯文本。
 *
 * 兼容：旧后端不产生 v2 事件 → 面板不渲染，AIChat 回退既有 `ToolCallTimeline`（M1-04）。
 */
import React, { useMemo } from 'react';
import { Space, Tag, Typography, theme } from 'antd';
import { CircleDashed, Wrench } from 'lucide-react';

import type { AIRunStepEvent, AIToolStreamEvent } from '@/lib/api/ai-api';
import { mergeToolEvents } from './tool-call-timeline';
import { formatDuration } from './run-status-bar';

/** 目标对象元信息（来自 invoke 详情；M0-03/B0-02 联合字段）。 */
export interface EvidenceTargetMeta {
  targetType?: string;
  targetId?: string;
  supportRef?: string;
}

/** 已核实的详情页路由（其他类型返回 null → 渲染为纯文本，不生成猜测链接）。 */
export const targetUrl = (targetType: string | undefined, targetId: string | undefined): string | null => {
  if (!targetType || !targetId) return null;
  switch (targetType.toLowerCase()) {
    case 'ticket':
      return `/tickets/${targetId}`;
    case 'incident':
      return `/incidents/${targetId}`;
    case 'ci':
      return `/cmdb/cis/${targetId}`;
    default:
      return null;
  }
};

const TOOL_STATUS_META: Record<string, { color: string; label: string }> = {
  started: { color: 'processing', label: '执行中' },
  done: { color: 'success', label: '完成' },
  failed: { color: 'error', label: '失败' },
  pending: { color: 'warning', label: '待审批' },
};

const STEP_TYPE_LABEL: Record<string, string> = {
  llm: '模型',
  tool: '工具',
};

export interface EvidencePanelProps {
  steps?: AIRunStepEvent[];
  toolEvents?: AIToolStreamEvent[];
  /** invocation id → 目标对象元信息（调用方从已拉取详情汇总；缺失则不强渲染）。 */
  detailsByInvocation?: Record<number, EvidenceTargetMeta>;
  /** 目标对象跳转（由调用方注入路由跳转，便于单测断言与跨页复用）。 */
  onOpenTarget?: (targetType: string, targetId: string) => void;
  /** 证据加载错误（如 run 档案接口失败）；展示为只读提示，不影响回答本身。 */
  error?: string;
}

/**
 * 证据面板：运行步骤（step 事件）+ 工具调用（tool_call 事件配对）。
 *
 * 只读展示，不引入新交互路径；空态/错误态明确。
 */
export const EvidencePanel: React.FC<EvidencePanelProps> = ({
  steps,
  toolEvents,
  detailsByInvocation,
  onOpenTarget,
  error,
}) => {
  const { token } = theme.useToken();
  const orderedSteps = useMemo(() => [...(steps ?? [])].sort((a, b) => a.stepIndex - b.stepIndex), [steps]);
  const tools = useMemo(() => mergeToolEvents(toolEvents), [toolEvents]);
  const isEmpty = orderedSteps.length === 0 && tools.length === 0;

  const monoStyle: React.CSSProperties = { fontSize: 12 };

  return (
    <div
      data-testid="evidence-panel"
      style={{
        marginTop: 10,
        padding: '8px 10px',
        borderRadius: 8,
        border: `1px solid ${token.colorBorderSecondary}`,
      }}
    >
      <Space size={6} style={{ fontSize: 12 }}>
        <CircleDashed size={12} />
        <Typography.Text strong style={{ fontSize: 12 }}>
          过程证据
        </Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          步骤 {orderedSteps.length} · 工具调用 {tools.length}
        </Typography.Text>
      </Space>

      {error ? (
        <div style={{ marginTop: 6, fontSize: 12, color: token.colorError }} data-testid="evidence-error">
          {error}
        </div>
      ) : null}

      {isEmpty && !error ? (
        <div style={{ marginTop: 6, fontSize: 12, color: token.colorTextSecondary }} data-testid="evidence-empty">
          本次运行没有可展示的过程证据。
        </div>
      ) : null}

      {tools.length > 0 ? (
        <div style={{ marginTop: 8 }}>
          {tools.map((t, idx) => {
            const status = TOOL_STATUS_META[t.status] ?? { color: 'default', label: t.status };
            const meta = typeof t.invocationId === 'number' ? detailsByInvocation?.[t.invocationId] : undefined;
            const url = targetUrl(meta?.targetType, meta?.targetId);
            return (
              <div
                key={`tool-${idx}`}
                data-testid={`evidence-tool-${idx}`}
                style={{ display: 'flex', gap: 6, alignItems: 'baseline', flexWrap: 'wrap', padding: '3px 0' }}
              >
                <Wrench size={12} style={{ verticalAlign: -2, color: token.colorTextSecondary }} />
                <Typography.Text code style={monoStyle}>
                  {t.tool}
                </Typography.Text>
                <Tag color={t.provider === 'mcp' ? 'geekblue' : 'default'} style={{ marginInlineEnd: 0 }}>
                  {t.provider === 'mcp' ? `MCP · ${t.server ?? '—'}` : '内置'}
                </Tag>
                <Tag color={status.color} style={{ marginInlineEnd: 0 }}>
                  {status.label}
                </Tag>
                {formatDuration(t.durationMs) ? (
                  <Typography.Text type="secondary" style={monoStyle}>
                    {formatDuration(t.durationMs)}
                  </Typography.Text>
                ) : null}
                {t.errorCode ? (
                  <Typography.Text style={{ ...monoStyle, color: token.colorError }}>{t.errorCode}</Typography.Text>
                ) : null}
                {t.summary ? (
                  <Typography.Text type="secondary" style={monoStyle} ellipsis={{ tooltip: t.summary }}>
                    {t.summary}
                  </Typography.Text>
                ) : null}
                {meta?.targetType && meta?.targetId ? (
                  onOpenTarget && url ? (
                    <Typography.Link style={monoStyle} onClick={() => onOpenTarget(meta.targetType!, meta.targetId!)}>
                      查看目标 {meta.targetType}#{meta.targetId}
                    </Typography.Link>
                  ) : (
                    <Typography.Text type="secondary" style={monoStyle}>
                      目标 {meta.targetType}#{meta.targetId}
                    </Typography.Text>
                  )
                ) : null}
                {meta?.supportRef ? (
                  <Tag color="purple" style={{ marginInlineEnd: 0 }}>
                    依据 {meta.supportRef}
                  </Tag>
                ) : null}
              </div>
            );
          })}
        </div>
      ) : null}

      {orderedSteps.length > 0 ? (
        <div style={{ marginTop: 8 }}>
          {orderedSteps.map(step => (
            <div
              key={`step-${step.stepIndex}`}
              data-testid={`evidence-step-${step.stepIndex}`}
              style={{ display: 'flex', gap: 6, alignItems: 'baseline', flexWrap: 'wrap', padding: '2px 0' }}
            >
              <Typography.Text type="secondary" style={monoStyle}>
                #{step.stepIndex}
              </Typography.Text>
              <Tag style={{ marginInlineEnd: 0 }}>{STEP_TYPE_LABEL[step.type] ?? step.type}</Tag>
              {formatDuration(step.durationMs) ? (
                <Typography.Text type="secondary" style={monoStyle}>
                  {formatDuration(step.durationMs)}
                </Typography.Text>
              ) : null}
              {step.payloadRef ? (
                <Typography.Text code style={monoStyle}>
                  {step.payloadRef}
                </Typography.Text>
              ) : null}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
};

export default EvidencePanel;
