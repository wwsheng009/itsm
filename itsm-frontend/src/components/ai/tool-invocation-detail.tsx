/**
 * 工具调用记录展示件（M1-06 审批页 / M1-07 审计页共用）。
 *
 * 口径集中在这里，避免两个页面各自演进导致「同一字段两种解释」：
 *   - 来源：`builtin` → 内置；`mcp` → MCP · <服务器标识>（缺服务器时退化为 MCP）；
 *     老记录 `provider` 为空按内置展示（不误标为外部工具）；
 *   - 风险色：high/medium/low 三档，其余中性；
 *   - 详情：来源三元组（provider/server/raw/callable）+ 角色快照 + 权限校验 +
 *     审批人与时间 + 耗时 + 错误码 + 结果摘要 + **脱敏参数**（原始参数不回显、不还原明文）。
 */
import React from 'react';
import { Descriptions, Divider, Space, Tag, Typography } from 'antd';
import { ExternalLink } from 'lucide-react';

import type { ToolApproval } from '@/lib/api/ai-api';

const { Text } = Typography;

/** 来源徽标文案。 */
export const formatToolSource = (record: Pick<ToolApproval, 'provider' | 'serverName'>): string => {
  if (record.provider !== 'mcp') return '内置';
  return record.serverName ? `MCP · ${record.serverName}` : 'MCP';
};

/** 风险标注颜色（high/medium/low）。 */
export const riskColor = (risk?: string): string => {
  if (risk === 'high') return 'red';
  if (risk === 'medium') return 'orange';
  if (risk === 'low') return 'green';
  return 'default';
};

/** 脱敏参数美化（解析失败时按原文展示，不吞内容）。 */
export const prettyArgs = (raw?: string): string => {
  if (!raw) return '-';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
};

/** 来源徽标（表格列内使用）。 */
export const ToolSourceTag: React.FC<{ record: Pick<ToolApproval, 'provider' | 'serverName'> }> = ({ record }) => (
  <Tag color={record.provider === 'mcp' ? 'blue' : 'default'}>{formatToolSource(record)}</Tag>
);

export interface ToolInvocationDetailProps {
  record: ToolApproval;
  /** 是否展示治理页跳转（调用方按 `mcp:admin` 决定）。 */
  canGovernTools?: boolean;
  /** 治理页跳转回调（不传则不渲染跳转）。 */
  onOpenGovernance?: () => void;
}

/** 工具调用详情面板：三元组 / 执行结果 / 脱敏参数。 */
export const ToolInvocationDetail: React.FC<ToolInvocationDetailProps> = ({
  record: r,
  canGovernTools = false,
  onOpenGovernance,
}) => (
  <Space direction="vertical" size={6} style={{ width: '100%' }}>
    <Descriptions size="small" column={3} bordered>
      <Descriptions.Item label="来源">{formatToolSource(r)}</Descriptions.Item>
      <Descriptions.Item label="服务器">{r.serverName || '-'}</Descriptions.Item>
      <Descriptions.Item label="风险">{r.risk || '-'}</Descriptions.Item>
      <Descriptions.Item label="原始工具名">{r.rawToolName || '-'}</Descriptions.Item>
      <Descriptions.Item label="可调用名" span={2}>
        {r.callableName || r.toolName}
      </Descriptions.Item>
      <Descriptions.Item label="角色快照">{r.roleSnapshot || '-'}</Descriptions.Item>
      <Descriptions.Item label="权限校验">
        {r.permissionCheck || '-'}
        {r.permissionReason ? `（${r.permissionReason}）` : ''}
      </Descriptions.Item>
      <Descriptions.Item label="审批人 / 时间">
        {r.approvedBy ? `${r.approvedBy}` : '-'}
        {r.approvedAt ? ` · ${new Date(r.approvedAt).toLocaleString('zh-CN', { hour12: false })}` : ''}
      </Descriptions.Item>
      <Descriptions.Item label="耗时">{typeof r.durationMs === 'number' ? `${r.durationMs}ms` : '-'}</Descriptions.Item>
      <Descriptions.Item label="错误码">{r.errorCode || '-'}</Descriptions.Item>
      <Descriptions.Item label="结果摘要" span={2}>
        {r.outputSummary || '-'}
      </Descriptions.Item>
    </Descriptions>
    {r.approvalReason ? (
      <Text type="secondary" style={{ fontSize: 12 }}>
        审批意见：{r.approvalReason}
      </Text>
    ) : null}
    <Divider style={{ margin: '4px 0' }} />
    <Text type="secondary" style={{ fontSize: 12 }}>
      脱敏参数（原始参数仅留在后端执行记录，不回显）
    </Text>
    <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: 12 }}>
      {prettyArgs(r.argsRedacted)}
    </pre>
    {canGovernTools && onOpenGovernance && r.provider === 'mcp' ? (
      <a onClick={onOpenGovernance}>
        <ExternalLink size={12} /> 前往工具治理页查看该服务器的工具开关与风险标注
      </a>
    ) : null}
  </Space>
);

export default ToolInvocationDetail;
