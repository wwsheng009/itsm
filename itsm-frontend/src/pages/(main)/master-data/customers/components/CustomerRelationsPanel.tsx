import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  App,
  Button,
  Card,
  Empty,
  Popconfirm,
  Space,
  Table,
  Tag,
  Tree,
  Typography,
  type TreeDataNode,
} from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import {
  masterDataService,
  type CustomerRelationship,
  type CustomerRelationsTree,
  type RelationshipTreeNode,
  type ServiceCustomer,
} from '@/lib/services/masterDataService';
import CustomerRelationshipModal from './CustomerRelationshipModal';

const { Text } = Typography;

interface CustomerRelationsPanelProps {
  customerId: number;
  canWrite: boolean;
  onChanged: () => void;
}

const relationTypeMeta: Record<string, { label: string; color: string }> = {
  parent: { label: '母公司', color: 'blue' },
  subsidiary: { label: '子公司', color: 'cyan' },
  partner: { label: '生态伙伴', color: 'green' },
  reseller: { label: '经销商', color: 'gold' },
  vendor: { label: '供应商', color: 'purple' },
  competitor: { label: '竞争对手', color: 'red' },
  successor: { label: '业务承继方', color: 'orange' },
  former_name_of: { label: '曾用名主体', color: 'default' },
};

const reverseRelationType: Record<string, string> = {
  parent: 'subsidiary',
  subsidiary: 'parent',
  partner: 'partner',
  competitor: 'competitor',
};

const relationTag = (relationType: string) => {
  const meta = relationTypeMeta[relationType];
  return meta ? <Tag color={meta.color}>{meta.label}</Tag> : <Tag>{relationType}</Tag>;
};

const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleDateString('zh-CN') : '-';

function toTreeData(nodes: RelationshipTreeNode[]): TreeDataNode[] {
  return nodes.map(node => ({
    key: `${node.relationType}-${node.customer.id}-${node.depth}`,
    title: (
      <Space size={6} wrap>
        <Text strong>{node.customer.name}</Text>
        {node.customer.code && <Text type="secondary">{node.customer.code}</Text>}
        <Tag>{node.customer.tier}</Tag>
        {relationTag(node.relationType)}
      </Space>
    ),
    children: node.children?.length ? toTreeData(node.children) : undefined,
  }));
}

export default function CustomerRelationsPanel({
  customerId,
  canWrite,
  onChanged,
}: CustomerRelationsPanelProps) {
  const { message } = App.useApp();
  const [tree, setTree] = useState<CustomerRelationsTree | null>(null);
  const [customers, setCustomers] = useState<ServiceCustomer[]>([]);
  const [loading, setLoading] = useState(false);
  const [modal, setModal] = useState<{
    open: boolean;
    relationship: CustomerRelationship | null;
  }>({ open: false, relationship: null });

  const load = useCallback(async () => {
    if (!customerId) return;
    setLoading(true);
    try {
      const [treeResult, customerResult] = await Promise.allSettled([
        masterDataService.relationsTree(customerId, 5),
        masterDataService.customers({ page: 1, pageSize: 200 }),
      ]);
      if (treeResult.status === 'fulfilled') {
        setTree(treeResult.value);
      } else {
        message.error('加载客户关系失败');
      }
      if (customerResult.status === 'fulfilled') {
        setCustomers(customerResult.value.items ?? []);
      }
    } finally {
      setLoading(false);
    }
  }, [customerId, message]);

  useEffect(() => {
    void load();
  }, [load]);

  const visibleRelations = useMemo(() => {
    const rows = tree?.directRelations ?? [];
    const outbound = rows.filter(row => row.direction === 'outbound');
    return rows.filter(row => {
      if (row.direction === 'outbound') return true;
      const inboundType = row.relationType;
      return !outbound.some(
        candidate =>
          candidate.toCustomerId === row.fromCustomerId &&
          reverseRelationType[candidate.relationType] === inboundType
      );
    });
  }, [tree]);

  const deleteRelation = async (relation: CustomerRelationship) => {
    try {
      await masterDataService.deleteRelationship(customerId, relation.id);
      message.success('客户关系已结束');
      await load();
      onChanged();
    } catch {
      message.error('删除客户关系失败');
    }
  };

  const columns: ColumnsType<CustomerRelationship> = [
    {
      title: '对方客户',
      key: 'counterpart',
      render: (_, relation) => {
        const counterpart =
          relation.direction === 'outbound' ? relation.toCustomer : relation.fromCustomer;
        return (
          <div>
            <Text strong>{counterpart?.name ?? '-'}</Text>
            <div className="text-xs text-gray-500">
              {[counterpart?.code, counterpart?.shortName, counterpart?.tier]
                .filter(Boolean)
                .join(' · ') || '-'}
            </div>
          </div>
        );
      },
    },
    {
      title: '关系',
      dataIndex: 'relationType',
      width: 110,
      render: (value: string) => relationTag(value),
    },
    {
      title: '方向',
      dataIndex: 'direction',
      width: 80,
      render: (value: string) =>
        value === 'outbound' ? <Tag color="blue">我方定义</Tag> : <Tag>对方定义</Tag>,
    },
    {
      title: '有效期',
      key: 'effective',
      width: 190,
      render: (_, relation) => (
        <span className="text-xs">
          {formatDate(relation.effectiveFrom)} ~ {formatDate(relation.effectiveTo)}
        </span>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (value: string) =>
        value === 'active' ? <Tag color="green">有效</Tag> : <Tag>已结束</Tag>,
    },
    {
      title: '备注',
      dataIndex: 'note',
      ellipsis: true,
      render: (value?: string) => value || '-',
    },
    {
      title: '操作',
      width: 150,
      render: (_, relation) =>
        canWrite && relation.direction === 'outbound' ? (
          <Space>
            <Button
              size="small"
              icon={<EditOutlined />}
              onClick={() => setModal({ open: true, relationship: relation })}
            >
              编辑
            </Button>
            <Popconfirm
              title="结束该客户关系？"
              description="双向关系会同时结束。"
              onConfirm={() => deleteRelation(relation)}
            >
              <Button size="small" danger icon={<DeleteOutlined />}>
                结束
              </Button>
            </Popconfirm>
          </Space>
        ) : (
          '-'
        ),
    },
  ];

  const parentChain = [...(tree?.parents ?? [])].reverse();
  const childrenTree = toTreeData(tree?.children ?? []);

  return (
    <div className="space-y-4">
      <Space wrap>
        {canWrite && customers.length > 0 && (
          <Button
            type="primary"
            size="small"
            icon={<PlusOutlined />}
            onClick={() => setModal({ open: true, relationship: null })}
          >
            添加客户关系
          </Button>
        )}
        <Button size="small" icon={<ReloadOutlined />} onClick={() => void load()}>
          刷新
        </Button>
      </Space>

      {!loading && visibleRelations.length === 0 && !parentChain.length && !childrenTree.length ? (
        <Empty description="暂无客户关系" />
      ) : (
        <>
          <Card size="small" title="上级组织链">
            {parentChain.length ? (
              <Space direction="vertical" size={8}>
                {parentChain.map(node => (
                  <Space key={`${node.customer.id}-${node.depth}`} size={8} wrap>
                    <Tag color="blue">上级 {node.depth}</Tag>
                    <Text strong>{node.customer.name}</Text>
                    <Text type="secondary">{node.customer.code}</Text>
                    <Tag>{node.customer.tier}</Tag>
                  </Space>
                ))}
              </Space>
            ) : (
              <Text type="secondary">未登记上级组织（最高层或未维护）</Text>
            )}
          </Card>

          <Card size="small" title="下级组织树">
            {childrenTree.length ? (
              <Tree treeData={childrenTree} defaultExpandAll showLine selectable={false} />
            ) : (
              <Text type="secondary">暂无下级组织</Text>
            )}
          </Card>

          <Card size="small" title="直接关系">
            <Table
              rowKey="id"
              size="small"
              loading={loading}
              columns={columns}
              dataSource={visibleRelations}
              pagination={false}
              locale={{ emptyText: '暂无直接关系' }}
            />
          </Card>
        </>
      )}

      <Alert
        type="info"
        showIcon
        message="关系口径"
        description="父/子公司、伙伴、竞争对手会自动写入反向关系；关系树带环检测，最大展开 5 层。"
      />

      <CustomerRelationshipModal
        open={modal.open}
        customerId={customerId}
        relationship={modal.relationship}
        customers={customers}
        onCancel={() => setModal({ open: false, relationship: null })}
        onSaved={async () => {
          setModal({ open: false, relationship: null });
          await load();
          onChanged();
        }}
      />
    </div>
  );
}
