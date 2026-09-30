/**
 * 对话内确认抽屉（B1-07）。
 *
 * 与审批页同源：数据来自同一接口 `GET /api/v1/agent/tools/:id`（脱敏参数 + 可选预览快照 + `expires_at`），
 * 决策走同一端点 `POST /api/v1/agent/tools/:id/approve`（B1-05 状态机：过期/冲突/回放都有稳定语义）。
 *
 * 交互边界：
 *   - 仅 `confirmationState=pending` 且未过期时可操作；
 *   - 倒计时归零 → 立即禁用并提示「已过期，请重新发起」；
 *   - 拒绝原因必填（回填会话，供模型下一轮重新规划）；
 *   - 通过/拒绝均为一次性动作，提交中禁用防重复点击。
 */
import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Drawer, Input, Space, Tag, Typography, theme } from 'antd';
import { AlertTriangle, CheckCircle2, Clock, ShieldAlert, XCircle } from 'lucide-react';

import type { ToolInvocationDetail } from '@/lib/api/ai-api';

/** 抽屉可视状态。 */
export type ConfirmationView = 'pending' | 'expired' | 'decided' | 'unknown';

export interface ConfirmationDrawerProps {
  open: boolean;
  /** 确认单详情（由调用方拉取；B1-05 字段随详情返回）。 */
  detail?: ToolInvocationDetail | null;
  /** 关闭抽屉。 */
  onClose: () => void;
  /** 决策回调；`approve=false` 时 reason 必填（组件内已校验）。 */
  onDecision: (approve: boolean, reason: string) => Promise<void> | void;
  /** 当前时间注入（单测用；默认 Date.now）。 */
  now?: () => number;
  /** 倒计时刷新间隔（毫秒；默认 1000，测试可调大避免抖动）。 */
  tickMs?: number;
  /** 决策接口错误信息（由调用方回填展示）。 */
  errorMessage?: string;
}

/** 规范状态推导（优先 B1-05 的 `confirmationState`；旧后端按 approvalState 降级）。 */
export const deriveConfirmationView = (
  detail: ToolInvocationDetail | undefined | null,
  nowMs: number = Date.now()
): ConfirmationView => {
  if (!detail) return 'unknown';
  const state = (detail.confirmationState || '').toLowerCase();
  const approval = (detail.approvalState || '').toLowerCase();
  const status = (detail.status || '').toLowerCase();

  // 已决优先于 pending 兜底：approvalState=rejected/approved 的记录即使 status 仍是 pending
  // （历史数据/半途状态）也必须按「已处理」只读展示，禁止再次操作。
  if (state === 'confirmed' || state === 'rejected' || state === 'cancelled') return 'decided';
  if (approval === 'approved' || approval === 'rejected') return 'decided';

  // 过期：显式终态，或 pending 且 expires_at 已过。
  if (state === 'expired' || approval === 'expired' || status === 'expired') return 'expired';
  const expires = detail.expiresAt ? Date.parse(detail.expiresAt) : NaN;
  const isPending = state === 'pending' || (!state && (approval === 'pending' || status === 'pending'));
  if (isPending) {
    if (!Number.isNaN(expires) && nowMs >= expires) return 'expired';
    return 'pending';
  }
  if (status === 'done' || status === 'failed' || status === 'rejected') return 'decided';
  return 'unknown';
};

/** 剩余时长文案（<0 记 0；>=1 天时只到分钟粒度）。 */
export const formatRemaining = (expiresAt: string | null | undefined, nowMs: number): string => {
  if (!expiresAt) return '未设置有效期';
  const expires = Date.parse(expiresAt);
  if (Number.isNaN(expires)) return '未设置有效期';
  const remaining = Math.max(0, expires - nowMs);
  const totalSeconds = Math.floor(remaining / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours >= 24) {
    return `剩余 ${Math.floor(hours / 24)} 天 ${hours % 24} 小时`;
  }
  if (hours > 0) return `剩余 ${hours} 小时 ${minutes} 分`;
  if (minutes > 0) return `剩余 ${minutes} 分 ${seconds} 秒`;
  return `剩余 ${seconds} 秒`;
};

/** 脱敏参数美化（非法 JSON 原样展示——后端落库即为 JSON 文本，异常时不吞内容）。 */
const prettyArgs = (raw?: string): string => {
  if (!raw) return '';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
};

export const ConfirmationDrawer: React.FC<ConfirmationDrawerProps> = ({
  open,
  detail,
  onClose,
  onDecision,
  now = Date.now,
  tickMs = 1000,
  errorMessage,
}) => {
  const { token } = theme.useToken();
  const [tick, setTick] = useState(() => now());
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // 倒计时：只在抽屉打开且仍有有效期时跳动（不打开不产生定时器）。
  useEffect(() => {
    if (!open || !detail?.expiresAt) return undefined;
    const timer = window.setInterval(() => setTick(now()), tickMs);
    return () => window.clearInterval(timer);
  }, [open, detail?.expiresAt, now, tickMs]);

  useEffect(() => {
    if (open) {
      setReason('');
      setSubmitting(false);
    }
  }, [open, detail?.id]);

  const view = useMemo(() => deriveConfirmationView(detail, tick), [detail, tick]);
  const canAct = view === 'pending' && !submitting;
  const rejectDisabled = !canAct || reason.trim().length === 0;

  const decide = async (approve: boolean) => {
    if (!canAct) return;
    setSubmitting(true);
    try {
      await onDecision(approve, reason.trim());
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Drawer
      title="执行确认"
      open={open}
      onClose={onClose}
      width={420}
      destroyOnClose
      data-testid="confirmation-drawer"
      extra={
        view === 'pending' ? (
          <Tag color="warning" icon={<Clock size={12} />} style={{ marginInlineEnd: 0 }}>
            {formatRemaining(detail?.expiresAt, tick)}
          </Tag>
        ) : null
      }
    >
      {!detail ? (
        <Typography.Text type="secondary">正在载入确认单…</Typography.Text>
      ) : (
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Space size={6} wrap>
            <Tag
              color={
                view === 'pending'
                  ? 'warning'
                  : view === 'expired'
                    ? 'default'
                    : view === 'decided'
                      ? 'success'
                      : 'default'
              }
              icon={
                view === 'pending' ? (
                  <ShieldAlert size={12} />
                ) : view === 'expired' ? (
                  <AlertTriangle size={12} />
                ) : (
                  <CheckCircle2 size={12} />
                )
              }
              style={{ marginInlineEnd: 0 }}
            >
              {view === 'pending' ? '待确认' : view === 'expired' ? '已过期' : view === 'decided' ? '已处理' : '状态未知'}
            </Tag>
            <Tag color={detail.provider === 'mcp' ? 'geekblue' : 'default'} style={{ marginInlineEnd: 0 }}>
              {detail.provider === 'mcp' ? `MCP · ${detail.serverName || '未知服务器'}` : '内置'}
            </Tag>
            {detail.risk ? (
              <Tag color={detail.risk === 'high' ? 'red' : detail.risk === 'medium' ? 'orange' : 'default'} style={{ marginInlineEnd: 0 }}>
                风险 · {detail.risk}
              </Tag>
            ) : null}
          </Space>

          <div>
            <Typography.Text strong>{detail.toolName}</Typography.Text>
            <Typography.Text type="secondary" style={{ marginLeft: 8, fontSize: 12 }}>
              #{detail.id}
            </Typography.Text>
          </div>

          <div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              脱敏参数
            </Typography.Text>
            <pre
              data-testid="confirmation-args"
              style={{
                margin: '6px 0 0',
                padding: 8,
                borderRadius: 8,
                background: token.colorFillQuaternary,
                fontSize: 12,
                maxHeight: 180,
                overflow: 'auto',
                whiteSpace: 'pre-wrap',
              }}
            >
              {prettyArgs(detail.argsRedacted) || '（无参数）'}
            </pre>
          </div>

          {detail.status === 'preview' && detail.result ? (
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                预览快照（dry-run，未写入业务数据）
              </Typography.Text>
              <pre
                data-testid="confirmation-dryrun"
                style={{
                  margin: '6px 0 0',
                  padding: 8,
                  borderRadius: 8,
                  background: token.colorFillQuaternary,
                  fontSize: 12,
                  maxHeight: 180,
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                }}
              >
                {prettyArgs(detail.result)}
              </pre>
            </div>
          ) : null}

          {view === 'expired' ? (
            <Alert
              type="warning"
              showIcon
              message="该确认单已过期"
              description="过期确认单不会被执行，也不会被补批准；请重新发起一次确认。"
            />
          ) : null}

          {view === 'decided' ? (
            <Alert
              type="info"
              showIcon
              message="该确认单已处理"
              description={detail.approvalReason ? `决策说明：${detail.approvalReason}` : '仅作只读展示，不支持重复操作。'}
            />
          ) : null}

          {errorMessage ? <Alert type="error" showIcon message={errorMessage} /> : null}

          <div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              拒绝原因（拒绝时必填）
            </Typography.Text>
            <Input.TextArea
              data-testid="confirmation-reason"
              rows={2}
              value={reason}
              disabled={!canAct}
              placeholder="例如：变更窗口外，禁止执行"
              onChange={e => setReason(e.target.value)}
              style={{ marginTop: 6 }}
            />
          </div>

          <Space size={8}>
            <Button
              type="primary"
              data-testid="confirmation-approve"
              icon={<CheckCircle2 size={14} />}
              disabled={!canAct}
              loading={submitting}
              onClick={() => void decide(true)}
            >
              确认执行
            </Button>
            <Button
              danger
              data-testid="confirmation-reject"
              icon={<XCircle size={14} />}
              disabled={rejectDisabled}
              loading={submitting}
              onClick={() => void decide(false)}
            >
              拒绝
            </Button>
          </Space>
        </Space>
      )}
    </Drawer>
  );
};

export default ConfirmationDrawer;
