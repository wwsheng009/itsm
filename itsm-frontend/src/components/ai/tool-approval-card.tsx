/**
 * 对话内「已提交待审批」卡片（M1-05）。
 *
 * 一期边界（R2 退化形态，分析报告 §6.5-6）：**外置审批闭环**——卡片只做提示与跳转，
 * 不提供内联确认（内联确认依赖阶段一 B1 的确认状态机，属二期）。
 *
 * 状态语义与「禁止重复操作」：
 *   - `pending`  ：待审批 → 提供「前往审批」跳转；
 *   - `approved` / `rejected` / 已执行终态：已处理 → **只读**展示决策人与原因，不再提供任何操作入口；
 *   - `expired`  ：记录不可得（404/查询失败），或 `createdAt` 超过待审批保留期（默认 7 天，仅前端提示，
 *                  后端当前无过期状态机）→ 提示「已过期/不可用」并禁止操作，只保留「前往审批页确认」。
 *
 * 数据来源：`GET /api/v1/agent/tools/:id` **按需拉取一次**（挂载时 + 手动刷新），不做轮询。
 */
import React, { useCallback, useEffect, useState } from 'react';
import { Button, Space, Tag, Tooltip, Typography, theme } from 'antd';
import { AlertTriangle, CheckCircle2, ExternalLink, RefreshCw, ShieldAlert, XCircle } from 'lucide-react';

import { aiGetToolInvocation, type ToolInvocationDetail } from '@/lib/api/ai-api';

/** 待审批保留期（天）：仅用于前端「可能已过期」提示，不代表后端有过期状态机。 */
export const PENDING_TTL_DAYS = 7;

/** 卡片可视状态（四态 + 加载/未知）。 */
export type ApprovalCardState = 'loading' | 'pending' | 'approved' | 'rejected' | 'expired' | 'unknown';

export interface ToolApprovalCardProps {
  /** tool_invocation 主键（来自 SSE 的 approval_pending 事件）。 */
  invocationId: number;
  /** 工具可调用名（事件回带；拉取失败时仍可展示）。 */
  tool: string;
  /** 来源：builtin | mcp。 */
  provider?: string;
  /** MCP 服务器标识。 */
  server?: string;
  /** 刷新成功后回调（供外层更新状态，例如同一 id 只渲染一张卡片）。 */
  onLoaded?: (detail: ToolInvocationDetail) => void;
  /** 跳转审批页（由外层注入路由跳转，便于单测断言）。 */
  onOpenApproval?: (invocationId: number) => void;
  /** 当前时间（单测注入，默认 Date.now）。 */
  now?: () => number;
}

interface CardView {
  state: ApprovalCardState;
  detail?: ToolInvocationDetail;
  errorMessage?: string;
}

/** 由接口返回推导卡片状态（纯函数，便于单测）。 */
export const deriveApprovalState = (
  detail: ToolInvocationDetail | undefined,
  ttlDays: number = PENDING_TTL_DAYS,
  nowMs: number = Date.now()
): ApprovalCardState => {
  if (!detail) return 'unknown';
  const approval = (detail.approvalState || '').toLowerCase();
  const status = (detail.status || '').toLowerCase();

  if (approval === 'approved') return 'approved';
  if (approval === 'rejected') return 'rejected';

  // 未决：pending 状态下超过保留期 → 前端提示过期（后端无过期状态机）。
  if (approval === 'pending' || status === 'pending') {
    const created = detail.createdAt ? Date.parse(detail.createdAt) : NaN;
    if (!Number.isNaN(created) && nowMs - created > ttlDays * 24 * 60 * 60 * 1000) {
      return 'expired';
    }
    return 'pending';
  }
  // 已执行/失败等终态：视为已处理（只读）。
  if (status === 'done' || status === 'failed') return 'approved';
  return 'unknown';
};

const STATE_META: Record<ApprovalCardState, { color: string; label: string; icon: React.ReactNode }> = {
  loading: { color: 'processing', label: '查询中', icon: <ShieldAlert size={14} /> },
  pending: { color: 'warning', label: '待审批', icon: <ShieldAlert size={14} /> },
  approved: { color: 'success', label: '已处理', icon: <CheckCircle2 size={14} /> },
  rejected: { color: 'error', label: '已驳回', icon: <XCircle size={14} /> },
  expired: { color: 'default', label: '已过期/不可用', icon: <AlertTriangle size={14} /> },
  unknown: { color: 'default', label: '状态未知', icon: <AlertTriangle size={14} /> },
};

const sourceLabel = (provider?: string, server?: string): string => {
  if (provider === 'mcp') return server ? `MCP · ${server}` : 'MCP';
  return '内置';
};

/**
 * 待审批卡片：来源徽标 + 风险 + 状态 + 跳转/刷新。
 *
 * 操作面随状态收敛：只有 `pending` 提供「前往审批」；已处理/过期一律只读（禁止重复操作）。
 */
export const ToolApprovalCard: React.FC<ToolApprovalCardProps> = ({
  invocationId,
  tool,
  provider,
  server,
  onLoaded,
  onOpenApproval,
  now = Date.now,
}) => {
  const { token } = theme.useToken();
  const [view, setView] = useState<CardView>({ state: 'loading' });
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      const detail = await aiGetToolInvocation(invocationId);
      setView({ state: deriveApprovalState(detail, PENDING_TTL_DAYS, now()), detail });
      onLoaded?.(detail);
    } catch (err) {
      // 记录不可得（已清理/跨租户/无权限）→ 过期/不可用，禁止操作但保留人工确认入口。
      setView({ state: 'expired', errorMessage: (err as Error)?.message });
    } finally {
      setRefreshing(false);
    }
  }, [invocationId, now, onLoaded]);

  useEffect(() => {
    void load();
    // 仅按 invocationId 拉取一次：不轮询（状态更新走手动刷新）。
  }, [load]);

  const meta = STATE_META[view.state];
  const detail = view.detail;
  const risk = detail?.risk;
  const canAct = view.state === 'pending';

  return (
    <div
      data-testid={`tool-approval-card-${invocationId}`}
      style={{
        marginTop: 10,
        padding: '10px 12px',
        borderRadius: 10,
        border: `1px solid ${token.colorBorderSecondary}`,
        background: canAct ? token.colorWarningBg : token.colorFillQuaternary,
      }}
    >
      <Space size={6} wrap style={{ fontSize: 12 }}>
        <Tag icon={meta.icon} color={meta.color} style={{ marginInlineEnd: 0 }}>
          {meta.label}
        </Tag>
        <Tag color={provider === 'mcp' ? 'geekblue' : 'default'} style={{ marginInlineEnd: 0 }}>
          {sourceLabel(provider ?? detail?.provider, server ?? detail?.serverName)}
        </Tag>
        <Typography.Text code style={{ fontSize: 12 }}>
          {detail?.toolName || tool}
        </Typography.Text>
        {risk ? (
          <Tooltip title="风险标注（来自工具治理）">
            <Tag color={risk === 'high' ? 'red' : risk === 'medium' ? 'orange' : 'default'} style={{ marginInlineEnd: 0 }}>
              风险 · {risk}
            </Tag>
          </Tooltip>
        ) : null}
        {detail?.approvalState === 'rejected' && detail.approvalReason ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            驳回原因：{detail.approvalReason}
          </Typography.Text>
        ) : null}
      </Space>

      <div style={{ marginTop: 6, fontSize: 12, color: token.colorTextSecondary }}>
        {view.state === 'loading' ? '正在查询审批状态…' : null}
        {view.state === 'pending' ? (
          <>已提交人工审批（#{invocationId}），审批通过后由系统执行；本会话不提供内联确认。</>
        ) : null}
        {view.state === 'approved' ? <>该请求已处理（#{invocationId}），无需再次操作。</> : null}
        {view.state === 'rejected' ? <>该请求已被驳回（#{invocationId}），未执行。</> : null}
        {view.state === 'expired' ? (
          <>记录已不可用或待审批时间过长（#{invocationId}），请前往审批页确认后再操作。</>
        ) : null}
        {view.state === 'unknown' ? <>未能确认审批状态（#{invocationId}），请前往审批页查看。</> : null}
      </div>

      <Space size={8} style={{ marginTop: 8 }}>
        {canAct ? (
          <Button
            type="primary"
            size="small"
            icon={<ExternalLink size={12} />}
            onClick={() => onOpenApproval?.(invocationId)}
          >
            前往审批
          </Button>
        ) : null}
        {!canAct ? (
          <Button size="small" icon={<ExternalLink size={12} />} onClick={() => onOpenApproval?.(invocationId)}>
            前往审批页确认
          </Button>
        ) : null}
        <Button
          type="text"
          size="small"
          icon={<RefreshCw size={12} />}
          loading={refreshing}
          onClick={() => void load()}
        >
          刷新状态
        </Button>
      </Space>
    </div>
  );
};

export default ToolApprovalCard;
