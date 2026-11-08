import { useCallback, useEffect, useState } from 'react';
import { Alert, App, Button, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import { ReloadOutlined, UndoOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import {
  masterDataService,
  type CustomerMergeHistoryItem,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface CustomerMergeHistoryPanelProps {
  customerId: number;
  canWrite: boolean;
  onChanged: () => void;
}

const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';

export default function CustomerMergeHistoryPanel({
  customerId,
  canWrite,
  onChanged,
}: CustomerMergeHistoryPanelProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<CustomerMergeHistoryItem[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    if (!customerId) return;
    setLoading(true);
    try {
      const result = await masterDataService.customerMergeHistory(customerId);
      setItems(result.items ?? []);
    } catch {
      message.error('加载合并记录失败');
    } finally {
      setLoading(false);
    }
  }, [customerId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const undo = async (item: CustomerMergeHistoryItem) => {
    try {
      await masterDataService.undoCustomerMerge(item.sourceId);
      message.success(`已撤销 ${item.sourceName} 的合并，源客户已恢复`);
      await load();
      onChanged();
    } catch (error) {
      message.error((error as Error).message || '撤销合并失败');
    }
  };

  const columns: ColumnsType<CustomerMergeHistoryItem> = [
    {
      title: '被合并客户',
      key: 'source',
      render: (_, item) => (
        <div>
          <Text strong>{item.sourceName || `#${item.sourceId}`}</Text>
          <div className="text-xs text-gray-500">
            {[item.sourceCode, `#${item.sourceId}`].filter(Boolean).join(' · ')}
          </div>
        </div>
      ),
    },
    {
      title: '合并时间',
      dataIndex: 'mergedAt',
      width: 180,
      render: formatDate,
    },
    {
      title: '撤销截止',
      dataIndex: 'undoExpiresAt',
      width: 180,
      render: formatDate,
    },
    {
      title: '状态',
      dataIndex: 'canUndo',
      width: 100,
      render: (canUndo: boolean) =>
        canUndo ? <Tag color="orange">可撤销</Tag> : <Tag>已撤销/不可撤销</Tag>,
    },
    {
      title: '操作',
      width: 120,
      render: (_, item) =>
        canWrite && item.canUndo ? (
          <Popconfirm
            title="撤销这次合并？"
            description="源客户及其分支、联系人、合同、项目关联等将按快照恢复。"
            onConfirm={() => undo(item)}
          >
            <Button size="small" icon={<UndoOutlined />}>
              撤销合并
            </Button>
          </Popconfirm>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <div className="space-y-3">
      <Space>
        <Button size="small" icon={<ReloadOutlined />} onClick={() => void load()}>
          刷新
        </Button>
      </Space>
      <Table
        rowKey="sourceId"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={items}
        pagination={false}
        locale={{ emptyText: '近 7 天没有合并记录' }}
      />
      <Alert
        type="info"
        showIcon
        message="撤销窗口"
        description="合并后 7 天内可依据审计快照反向恢复；超过窗口或源客户状态已变化时不可撤销。"
      />
    </div>
  );
}
