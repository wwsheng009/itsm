import { useCallback, useEffect, useState } from 'react';
import { Alert, App, Button, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import { DeleteOutlined, ReloadOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import {
  masterDataService,
  type ContractProjectLink,
} from '@/lib/services/masterDataService';

const { Text } = Typography;

interface ProjectContractsPanelProps {
  projectId: number;
  canWrite: boolean;
}

export default function ProjectContractsPanel({
  projectId,
  canWrite,
}: ProjectContractsPanelProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<ContractProjectLink[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    try {
      const result = await masterDataService.projectContractLinks(projectId);
      setItems(result.items ?? []);
    } catch {
      message.error('加载项目关联合同失败');
    } finally {
      setLoading(false);
    }
  }, [message, projectId]);

  useEffect(() => {
    void load();
  }, [load]);

  const remove = async (item: ContractProjectLink) => {
    try {
      await masterDataService.deleteContractProjectLink(item.contractId, item.id);
      message.success('合同项目关联已解除');
      await load();
    } catch {
      message.error('解除合同项目关联失败');
    }
  };

  const columns: ColumnsType<ContractProjectLink> = [
    {
      title: '合同',
      key: 'contract',
      render: (_, item) => (
        <div>
          <Text strong>{item.contractNumber || `#${item.contractId}`}</Text>
          <div className="text-xs text-gray-500">{item.contractName || '-'}</div>
        </div>
      ),
    },
    {
      title: '关系类型',
      dataIndex: 'relationType',
      width: 110,
      render: (value: string) => (
        <Tag color={value === 'billing' ? 'gold' : value === 'basis' ? 'blue' : 'default'}>
          {value}
        </Tag>
      ),
    },
    {
      title: '服务项',
      dataIndex: 'serviceItemName',
      width: 160,
      render: (value: string, item) => value || item.serviceItemId || '-',
    },
    {
      title: '分配额度',
      key: 'allocation',
      width: 190,
      render: (_, item) =>
        [
          item.allocatedAmount != null ? `金额 ${item.allocatedAmount}` : '',
          item.allocatedHours != null ? `数量 ${item.allocatedHours}` : '',
          item.allocationPct != null ? `比例 ${item.allocationPct}%` : '',
        ]
          .filter(Boolean)
          .join(' / ') || '-',
    },
    {
      title: '主依据',
      dataIndex: 'isPrimary',
      width: 90,
      render: (value: boolean) => (value ? <Tag color="blue">是</Tag> : '-'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (value: string) => <Tag>{value}</Tag>,
    },
    {
      title: '操作',
      width: 100,
      render: (_, item) =>
        canWrite ? (
          <Popconfirm title="解除该合同与项目的关联？" onConfirm={() => remove(item)}>
            <Button size="small" danger icon={<DeleteOutlined />}>
              解除
            </Button>
          </Popconfirm>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <div className="space-y-3">
      <Space wrap>
        <Button icon={<ReloadOutlined />} onClick={() => void load()}>
          刷新
        </Button>
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={items}
        pagination={false}
        size="small"
        locale={{ emptyText: '暂无关联合同' }}
      />
      <Alert
        type="info"
        showIcon
        message="合同项目关联从合同详情侧维护；这里展示项目被哪些合同覆盖，以及 billing 预占额度。"
      />
    </div>
  );
}
