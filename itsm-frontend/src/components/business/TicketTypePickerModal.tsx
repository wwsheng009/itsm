
/**
 * TicketTypePickerModal —— 工单类型选择弹层
 *
 * 设计文档：docs/architecture/ticket-create-page-rich-input-optimization.md §4.2
 *  - 替换创建页常驻的类型列表，压缩首屏高度；
 *  - 类型数量 > 8 时提供搜索与业务域分组；
 *  - 选中后由父级回显「已选类型摘要 + 更换类型」。
 */

import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Card,
  Col,
  Empty,
  Input,
  Modal,
  Row,
  Segmented,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd';
import { CheckCircleFilled } from '@ant-design/icons';
import TicketTypeIcon from './TicketTypeIcon';

const { Text } = Typography;

export type TicketTypeGroup = 'all' | 'service_request' | 'incident' | 'change' | 'problem';

/** 弹层可接受的类型条目（RuntimeTicketType 的结构化超集） */
export interface TicketTypePickerItem {
  id: number;
  code: string;
  name: string;
  description?: string;
  icon?: string;
  color?: string;
  priority?: 'low' | 'medium' | 'high' | 'urgent' | 'critical';
  workflowDefinitionKey?: string;
  fields?: Array<{ required?: boolean }>;
}

export interface TicketTypePickerModalProps {
  open: boolean;
  types: TicketTypePickerItem[];
  value?: TicketTypePickerItem | null;
  loading?: boolean;
  error?: string | null;
  onCancel: () => void;
  onChange: (type: TicketTypePickerItem) => void;
}

const GROUP_OPTIONS: Array<{ label: string; value: TicketTypeGroup }> = [
  { label: '全部', value: 'all' },
  { label: '服务请求', value: 'service_request' },
  { label: '故障', value: 'incident' },
  { label: '变更', value: 'change' },
  { label: '问题', value: 'problem' },
];

/**
 * 业务域归类：与创建页 inferTicketType 的语义保持一致（code/name/workflow 关键词）。
 */
export function classifyTicketTypeGroup(type: TicketTypePickerItem): Exclude<TicketTypeGroup, 'all'> {
  const value = `${type.code || ''} ${type.name || ''} ${type.workflowDefinitionKey || ''}`;
  if (/change|变更|ddl|firewall|domain/i.test(value)) return 'change';
  if (/problem|问题/i.test(value)) return 'problem';
  if (/incident|故障|事件/i.test(value)) return 'incident';
  return 'service_request';
}

const TicketTypePickerModal: React.FC<TicketTypePickerModalProps> = ({
  open,
  types,
  value,
  loading = false,
  error = null,
  onCancel,
  onChange,
}) => {
  const [keyword, setKeyword] = useState('');
  const [group, setGroup] = useState<TicketTypeGroup>('all');

  // 关闭后重置筛选，避免下次打开残留上次的搜索词
  useEffect(() => {
    if (!open) {
      setKeyword('');
      setGroup('all');
    }
  }, [open]);

  const showFilter = types.length > 8;

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    return types.filter((type) => {
      if (group !== 'all' && classifyTicketTypeGroup(type) !== group) return false;
      if (!kw) return true;
      return `${type.name} ${type.code} ${type.description || ''}`.toLowerCase().includes(kw);
    });
  }, [types, keyword, group]);

  const handleSelect = (type: TicketTypePickerItem) => {
    onChange(type);
  };

  return (
    <Modal
      title="选择工单类型"
      open={open}
      onCancel={onCancel}
      footer={null}
      width={960}
      aria-label="选择工单类型弹层"
      data-testid="ticket-type-picker-modal"
    >
      {error ? (
        <Alert type="error" showIcon message={error} className="mb-4" />
      ) : loading ? (
        <div className="flex justify-center py-12" data-testid="ticket-type-picker-loading">
          <Spin />
        </div>
      ) : types.length === 0 ? (
        <Empty description="暂无已启用的工单类型，请先在「工单管理 → 工单类型」中创建并启用" />
      ) : (
        <>
          {showFilter && (
            <Space orientation="vertical" size={12} style={{ width: '100%', marginBottom: 16 }}>
              <Input.Search
                allowClear
                placeholder="搜索类型名称、编码或描述"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                aria-label="搜索工单类型"
              />
              <Segmented<TicketTypeGroup>
                options={GROUP_OPTIONS}
                value={group}
                onChange={(v) => setGroup(v)}
                aria-label="按业务域筛选工单类型"
              />
            </Space>
          )}

          {filtered.length === 0 ? (
            <Empty description="没有匹配的工单类型" />
          ) : (
            <Row gutter={[16, 16]} role="list" aria-label="可用工单类型列表">
              {filtered.map((type) => {
                const selected = value?.id === type.id;
                const requiredCount = (type.fields || []).filter((f) => f.required).length;
                const color = type.color || '#1677ff';

                return (
                  <Col xs={24} sm={12} lg={8} key={type.id}>
                    <Card
                      hoverable
                      size="small"
                      role="button"
                      tabIndex={0}
                      aria-pressed={selected}
                      aria-label={`选择工单类型：${type.name}`}
                      data-testid={`ticket-type-card-${type.id}`}
                      onClick={() => handleSelect(type)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          handleSelect(type);
                        }
                      }}
                      style={{
                        height: '100%',
                        borderColor: selected ? color : undefined,
                        backgroundColor: selected ? `${color}10` : undefined,
                        cursor: 'pointer',
                      }}
                    >
                      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                        <Space align="center" style={{ width: '100%' }}>
                          <span style={{ color, display: 'inline-flex' }} aria-hidden="true">
                            <TicketTypeIcon icon={type.icon} />
                          </span>
                          <Text strong style={{ flex: 1 }}>
                            {type.name}
                          </Text>
                          {selected && <CheckCircleFilled style={{ color }} aria-hidden="true" />}
                        </Space>
                        <Text type="secondary" style={{ fontSize: 12, minHeight: 36, display: 'block' }}>
                          {type.description || '暂无描述'}
                        </Text>
                        <Space size={4} wrap>
                          {type.workflowDefinitionKey && (
                            <Tag color="blue" style={{ marginInlineEnd: 0 }}>
                              审批流程
                            </Tag>
                          )}
                          {requiredCount > 0 && (
                            <Tag color="orange" style={{ marginInlineEnd: 0 }}>
                              必填 {requiredCount}
                            </Tag>
                          )}
                          {!type.workflowDefinitionKey && requiredCount === 0 && (
                            <Tag style={{ marginInlineEnd: 0 }}>基础表单</Tag>
                          )}
                        </Space>
                      </Space>
                    </Card>
                  </Col>
                );
              })}
            </Row>
          )}
        </>
      )}
    </Modal>
  );
};

export default TicketTypePickerModal;
