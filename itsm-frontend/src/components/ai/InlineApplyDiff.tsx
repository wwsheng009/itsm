/**
 * I1-01 Inline Apply diff 预览弹窗：字段 before → after；**确认才应用，绝不自动提交**。
 * 与 `useInlineApplyFlow` 配套使用（pending/confirm/cancel 由调用方提供）。
 */
import React from 'react';
import { Alert, Button, List, Modal, Tag, Typography } from 'antd';
import { ArrowRight, ShieldCheck } from 'lucide-react';

import type { InlineChangeSet } from './use-inline-apply';

const { Text } = Typography;

export interface InlineApplyDiffProps {
  open: boolean;
  changes: InlineChangeSet[];
  applying?: boolean;
  title?: string;
  /** 说明文案（缺省：仅应用所列字段，提交仍由页面完成）。 */
  hint?: string;
  confirmText?: string;
  onConfirm: () => void;
  onCancel: () => void;
  testId?: string;
}

export const InlineApplyDiff: React.FC<InlineApplyDiffProps> = ({
  open,
  changes,
  applying,
  title = '应用 AI 建议（差异预览）',
  hint = '仅应用下列字段；保存/提交仍由你在页面上完成，撤销可随时恢复。',
  confirmText = '确认应用',
  onConfirm,
  onCancel,
  testId = 'inline-apply-diff',
}) => (
  <Modal
    title={
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
        <ShieldCheck size={16} color="#1677ff" />
        {title}
      </span>
    }
    open={open}
    onCancel={onCancel}
    onOk={onConfirm}
    okText={confirmText}
    cancelText="取消"
    confirmLoading={applying}
    destroyOnHidden
    width={520}
  >
    <div data-testid={testId}>
      <Alert type="info" showIcon message={hint} style={{ marginBottom: 12 }} />
      <List
        size="small"
        dataSource={changes}
        renderItem={item => (
          <List.Item data-testid={`${testId}-row-${item.field}`}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
              <Text strong style={{ minWidth: 64 }}>
                {item.label}
              </Text>
              <Tag>{item.before ?? '（空）'}</Tag>
              <ArrowRight size={14} color="#8c8c8c" />
              <Tag color="blue">{item.after}</Tag>
            </div>
          </List.Item>
        )}
      />
    </div>
  </Modal>
);

export default InlineApplyDiff;