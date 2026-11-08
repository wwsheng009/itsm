import { useEffect, useState } from 'react';
import {
  Alert,
  App,
  Checkbox,
  Descriptions,
  Form,
  Modal,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd';
import { masterDataService, type ServiceCustomer } from '@/lib/services/masterDataService';

const { Text } = Typography;

interface CustomerMergeModalProps {
  open: boolean;
  source: ServiceCustomer | null;
  onCancel: () => void;
  onSaved: () => void;
}

interface MergeFormValues {
  targetCustomerId: number;
  confirmed: boolean;
}

interface MergeReport {
  branchesCreated: number;
  contactsMoved: number;
  tenantLinksMoved: number;
  relationshipsMoved: number;
  contractsMoved: number;
  externalRefsMoved: number;
  externalIdsMoved: number;
  conversationsMoved: number;
  projectLinksMoved: number;
}

interface MergeImpact {
  branchCount: number;
  contactCount: number;
  contractCount: number;
  tenantLinkCount: number;
  projectLinkCount: number;
  externalIdCount: number;
}

export default function CustomerMergeModal({
  open,
  source,
  onCancel,
  onSaved,
}: CustomerMergeModalProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<MergeFormValues>();
  const [customers, setCustomers] = useState<ServiceCustomer[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [impact, setImpact] = useState<MergeImpact | null>(null);
  const [impactLoading, setImpactLoading] = useState(false);

  useEffect(() => {
    if (!open) return;
    form.resetFields();
    setLoading(true);
    masterDataService
      .customers({ page: 1, pageSize: 300 })
      .then(result => setCustomers(result.items ?? []))
      .catch(() => message.error('加载可合并客户失败'))
      .finally(() => setLoading(false));
  }, [form, message, open]);

  useEffect(() => {
    if (!open || !source) {
      setImpact(null);
      return;
    }
    setImpactLoading(true);
    Promise.allSettled([
      masterDataService.customerDetail(source.id),
      masterDataService.customerProjects(source.id),
    ])
      .then(([detailResult, projectResult]) => {
        const customer = detailResult.status === 'fulfilled' ? detailResult.value.customer : null;
        setImpact({
          branchCount: customer?.branchCount ?? 0,
          contactCount: customer?.contactCount ?? 0,
          contractCount: customer?.contractCount ?? 0,
          tenantLinkCount: customer?.tenantLinkCount ?? 0,
          projectLinkCount:
            projectResult.status === 'fulfilled' ? projectResult.value.total ?? 0 : 0,
          externalIdCount:
            detailResult.status === 'fulfilled'
              ? detailResult.value.externalIds?.length ?? 0
              : 0,
        });
      })
      .finally(() => setImpactLoading(false));
  }, [open, source]);

  const submit = async () => {
    if (!source) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      const report = (await masterDataService.mergeCustomer(
        source.id,
        values.targetCustomerId
      )) as unknown as MergeReport;
      const moved = [
        report.branchesCreated ? `服务点 ${report.branchesCreated}` : '',
        report.contactsMoved ? `联系人 ${report.contactsMoved}` : '',
        report.tenantLinksMoved ? `租户关联 ${report.tenantLinksMoved}` : '',
        report.relationshipsMoved ? `客户关系 ${report.relationshipsMoved}` : '',
        report.contractsMoved ? `合同 ${report.contractsMoved}` : '',
        report.externalRefsMoved ? `外部编号 ${report.externalRefsMoved}` : '',
        report.externalIdsMoved ? `外部标识 ${report.externalIdsMoved}` : '',
        report.conversationsMoved ? `邮件会话 ${report.conversationsMoved}` : '',
        report.projectLinksMoved ? `项目关联 ${report.projectLinksMoved}` : '',
      ]
        .filter(Boolean)
        .join('、');
      message.success(`客户已合并${moved ? `：${moved}` : ''}，7 天内可撤销`);
      onSaved();
    } catch (error) {
      message.error((error as Error).message || '合并客户失败');
    } finally {
      setSaving(false);
    }
  };

  const targetOptions = customers
    .filter(customer => customer.id !== source?.id)
    .map(customer => ({
      value: customer.id,
      label: `${customer.name}（${customer.code || '无编码'}）`,
    }));

  return (
    <Modal
      title="合并客户主数据"
      open={open}
      onOk={submit}
      onCancel={onCancel}
      confirmLoading={saving}
      okText="确认合并"
      okButtonProps={{ danger: true }}
      width={620}
      destroyOnHidden
    >
      <Space direction="vertical" size={16} className="w-full">
        <Alert
          type="warning"
          showIcon
          message="合并会迁移源客户的分支、联系人、租户关联、合同、项目关联等数据"
          description="源客户将被软删除并标记合并去向；7 天内可通过审计快照撤销。重复联系人按邮箱去重，冲突关联自动降级或删除。"
        />

        {source && (
          <div className="rounded border border-gray-200 p-3">
            <Text type="secondary">源客户</Text>
            <div className="mt-1">
              <Space wrap>
                <Text strong>{source.name}</Text>
                <Tag>{source.code || '无编码'}</Tag>
                <Tag>#{source.id}</Tag>
              </Space>
            </div>
          </div>
        )}

        {source && (
          <div className="rounded border border-gray-200 p-3">
            <Text type="secondary">关键影响面预览</Text>
            <Spin spinning={impactLoading}>
              <Descriptions
                className="mt-2"
                size="small"
                column={3}
                items={[
                  { key: 'branches', label: '服务点', children: impact?.branchCount ?? 0 },
                  { key: 'contacts', label: '联系人', children: impact?.contactCount ?? 0 },
                  { key: 'contracts', label: '合同', children: impact?.contractCount ?? 0 },
                  { key: 'tenants', label: '租户关联', children: impact?.tenantLinkCount ?? 0 },
                  { key: 'projects', label: '项目关联', children: impact?.projectLinkCount ?? 0 },
                  { key: 'externalIds', label: '外部标识', children: impact?.externalIdCount ?? 0 },
                ]}
              />
            </Spin>
          </div>
        )}

        <Form form={form} layout="vertical">
          <Form.Item
            name="targetCustomerId"
            label="目标客户"
            rules={[{ required: true, message: '请选择目标客户' }]}
          >
            <Select
              showSearch
              loading={loading}
              optionFilterProp="label"
              options={targetOptions}
              placeholder="选择保留的主档客户"
            />
          </Form.Item>
          <Form.Item
            name="confirmed"
            valuePropName="checked"
            rules={[
              {
                validator: (_, value) =>
                  value ? Promise.resolve() : Promise.reject(new Error('请确认合并操作')),
              },
            ]}
          >
            <Checkbox>我确认执行合并，并知晓 7 天后不可撤销</Checkbox>
          </Form.Item>
        </Form>
      </Space>
    </Modal>
  );
}
