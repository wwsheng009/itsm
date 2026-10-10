
import { useCallback, useEffect, useState } from 'react';
import { Alert, App, Button, Card, Input, Modal, Space, Switch, Table, Tag, Typography } from 'antd';
import { Copy, Pencil, Plus, RotateCcw } from 'lucide-react';
import { TicketTypeFormModal } from '@/components/business/TicketTypeFormModal';
import { TicketTypeApi } from '@/lib/api/ticketTypeApi';
import { usePermissions } from '@/lib/hooks/use-permissions';
import type { CreateTicketTypeRequest, TicketTypeDefinition, TicketTypePresetDefinition, UpdateTicketTypeRequest } from '@/types/ticket-type';

const { Title, Text } = Typography;

export default function TicketTypesPage() {
  const { message, modal } = App.useApp();
  const { hasPermission } = usePermissions();
  // 预设安装是独立权限点（ticket_type:install_preset）；无权限角色不渲染入口，
  // 避免点击后落 403「权限不足」（服务端 RequirePermission 仍为最终闸门）。
  const canInstallPreset = hasPermission('ticket_type', 'install_preset');
  // 入口级门禁：新建/编辑/复制/启停/恢复 → ticket_type:manage
  // （rbac_precheck_gen.go：POST /ticket-types、PUT /ticket-types/*、clone/enable/disable/restore 均 manage）。
  const canManage = hasPermission('ticket_type', 'manage');
  const [items, setItems] = useState<TicketTypeDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [keyword, setKeyword] = useState('');
  const [includeArchived, setIncludeArchived] = useState(false);
  const [editing, setEditing] = useState<TicketTypeDefinition | null>(null);
  const [open, setOpen] = useState(false);
	const [presetOpen, setPresetOpen] = useState(false);
	const [presets, setPresets] = useState<TicketTypePresetDefinition[]>([]);
	// 已安装预设判定：按租户现有类型编码（含已归档，与服务端 checkCodeExists 口径一致）。
	const [installedCodes, setInstalledCodes] = useState<Set<string>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await TicketTypeApi.list({ keyword, includeArchived, page: 1, pageSize: 100 });
      setItems(result.types ?? []);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载工单类型失败');
    } finally { setLoading(false); }
  }, [keyword, includeArchived, message]);

  useEffect(() => { void load(); }, [load]);

  const save = async (values: unknown) => {
    try {
      if (editing) await TicketTypeApi.update(editing.id, values as UpdateTicketTypeRequest);
      else await TicketTypeApi.create(values as CreateTicketTypeRequest);
      message.success(editing ? '工单类型已更新' : '工单类型已创建');
      setOpen(false); setEditing(null); await load();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存工单类型失败');
      throw error; // 通知表单弹窗保持打开，便于修改后重试
    }
  };

  const clone = (item: TicketTypeDefinition) => {
    let code = `${item.code}_copy`;
    let name = `${item.name} 副本`;
    modal.confirm({
      title: '复制工单类型',
      content: <Space orientation="vertical" className="w-full"><Input defaultValue={code} onChange={e => { code = e.target.value; }} /><Input defaultValue={name} onChange={e => { name = e.target.value; }} /></Space>,
      onOk: async () => {
        try {
          await TicketTypeApi.clone(item.id, code, name);
          message.success('工单类型已复制');
          await load();
        } catch (error) {
          message.error(error instanceof Error ? error.message : '复制工单类型失败');
          throw error;
        }
      },
    });
  };

	const openPresets = async () => {
    try {
      const [presetList, existing] = await Promise.all([
        TicketTypeApi.listPresets(),
        TicketTypeApi.list({ includeArchived: true, page: 1, pageSize: 100 }),
      ]);
      setPresets(presetList);
      setInstalledCodes(new Set((existing.types ?? []).map(item => item.code)));
      setPresetOpen(true);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载预设库失败');
    }
  };

  const restore = (item: TicketTypeDefinition) => {
    modal.confirm({
      title: '恢复工单类型',
      content: `确定恢复已归档的类型「${item.name}」吗？恢复后将重新启用。`,
      okText: '恢复',
      onOk: async () => { await TicketTypeApi.restore(item.id); message.success('工单类型已恢复'); await load(); },
    });
  };

  return <div className="p-6"><Card>
    <div className="mb-5 flex items-start justify-between gap-4"><div><Title level={2} className="!mb-1">工单类型</Title><Text type="secondary">租户级类型、动态表单及流程策略配置</Text></div><Space>{canInstallPreset && <Button onClick={() => void openPresets()}>从预设安装</Button>}{canManage && <Button type="primary" icon={<Plus className="h-4 w-4" />} onClick={() => { setEditing(null); setOpen(true); }}>新建类型</Button>}</Space></div>
    <Alert
      className="mb-4"
      type="info"
      showIcon
      title="工单类型是配置生效的绑定中心"
      description="在此选择已发布的工作流、已启用的 SLA 和分配规则；启用审批后补充审批层级。保存并启用类型后，创建该类型的测试工单，在流程实例、待审批、SLA 监控和审计日志中验收结果。"
    />
    <Input.Search className="mb-4 max-w-sm" allowClear placeholder="搜索名称或编码" onSearch={setKeyword} />
    <div className="mb-3"><label className="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-600"><input type="checkbox" checked={includeArchived} onChange={e => setIncludeArchived(e.target.checked)} />显示已归档类型</label></div>
    {/* 每列显式设置宽度并对长文本列开启 ellipsis，避免列宽被均分导致字段拥挤；总宽超出容器时通过 scroll.x 横向滚动兜底 */}
    <Table rowKey="id" loading={loading} dataSource={items} scroll={{ x: 'max-content' }} columns={[
      { title: '名称', dataIndex: 'name', width: 160, ellipsis: true, render: (value: string) => <Text strong>{value}</Text> },
      { title: '编码', dataIndex: 'code', width: 160, ellipsis: true, render: (value: string) => <Tag color="blue" style={{ maxWidth: '100%', overflow: 'hidden', textOverflow: 'ellipsis' }}>{value}</Tag> },
	  { title: '动态字段', dataIndex: 'customFields', width: 90, render: (value: unknown[]) => `${value?.length ?? 0} 个` },
	  { title: '默认优先级', dataIndex: 'defaultPriority', width: 100, render: (value: string) => <Tag>{value}</Tag> },
	  { title: '工作流', dataIndex: 'workflowDefinitionKey', width: 200, ellipsis: true, render: (value?: string) => value || '-' },
      { title: 'SLA', dataIndex: 'slaEnabled', width: 80, render: (value: boolean) => value ? '已绑定' : '-' },
	  { title: '排序', dataIndex: 'sortOrder', width: 64 },
      { title: '状态', dataIndex: 'status', width: 96, render: (value: string, item) => item.archivedAt ? <Tag color="default">已归档</Tag> : <Tag color={value === 'active' ? 'green' : 'default'}>{value === 'active' ? '启用' : '停用'}</Tag> },
      { title: '更新时间', dataIndex: 'updatedAt', width: 170, render: (value: string) => value ? new Date(value).toLocaleString() : '-' },
      { title: '操作', width: 240, fixed: 'right', render: (_, item) => item.archivedAt ? <Space>{canManage && <Button size="small" type="primary" ghost icon={<RotateCcw className="h-3.5 w-3.5" />} onClick={() => restore(item)}>恢复</Button>}</Space> : <Space>{canManage && (<><Button size="small" icon={<Pencil className="h-3.5 w-3.5" />} onClick={() => { setEditing(item); setOpen(true); }}>编辑</Button><Button size="small" icon={<Copy className="h-3.5 w-3.5" />} onClick={() => clone(item)}>复制</Button><Switch size="small" checked={item.status === 'active'} onChange={async enabled => { try { await TicketTypeApi.setEnabled(item.id, enabled); message.success(enabled ? '工单类型已启用' : '工单类型已停用'); await load(); } catch (error) { message.error(error instanceof Error ? error.message : '操作失败'); } }} /></>)}</Space> },
    ]} />
  </Card><TicketTypeFormModal visible={open} editingType={editing} onCancel={() => { setOpen(false); setEditing(null); }} onSubmit={save} /><Modal title="工单类型预设库" open={presetOpen} footer={null} onCancel={() => setPresetOpen(false)}><div className="flex flex-col gap-3">{presets.map(preset => { const installed = installedCodes.has(preset.definition?.code); return <div key={preset.id} className="flex items-start justify-between gap-4 border-b border-gray-100 pb-3 last:border-b-0"><div><div className="font-medium">{preset.name}</div><div className="text-sm text-gray-500">{preset.category} · {preset.description}</div></div><Button type="link" disabled={installed} onClick={async () => { try { const created = await TicketTypeApi.installPreset(preset.id); setInstalledCodes(prev => new Set(prev).add(created.code)); message.success('预设已安装'); setPresetOpen(false); await load(); } catch (error) { message.error(error instanceof Error ? error.message : '安装预设失败'); } }}>{installed ? '已安装' : '安装'}</Button></div>; })}</div></Modal></div>;
}
