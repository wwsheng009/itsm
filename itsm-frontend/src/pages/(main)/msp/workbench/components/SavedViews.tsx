/**
 * SavedViews 工作台自定义视图控件（IP-P2-4a 前端）。
 *
 * 契约（后端 c1f0793b）：
 * - 列表 = own + 同 provider 分享（isOwner=false 只读）；
 * - 保存 = 当前筛选组合（filters 与 WorkbenchTicketQuery 对齐）；
 * - 分享 = 同 provider 可见；默认 = 每 owner 至多一条；
 * - 灰度由服务端 `enabled` 决定：父组件仅在 enabled=true 时渲染本控件。
 */
import { useState } from 'react';
import { Button, Dropdown, Form, Input, Modal, Select, Space, Switch, message } from 'antd';
import type { MenuProps } from 'antd';
import { Bookmark } from 'lucide-react';
import type { WorkbenchView, WorkbenchViewPayload } from '@/lib/api/msp-workbench-api';

export interface SavedViewsProps {
  views: WorkbenchView[];
  activeViewId?: number;
  /** 当前页面筛选组合（保存/更新时作为 filters 载荷）。 */
  currentFilters: WorkbenchViewPayload['filters'];
  onApply: (view: WorkbenchView) => void;
  onClear: () => void;
  onCreate: (payload: WorkbenchViewPayload) => Promise<void>;
  onUpdate: (id: number, payload: WorkbenchViewPayload) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
  onSetDefault: (id: number) => Promise<void>;
}

export default function SavedViews({
  views,
  activeViewId,
  currentFilters,
  onApply,
  onClear,
  onCreate,
  onUpdate,
  onDelete,
  onSetDefault,
}: SavedViewsProps) {
  const [modalMode, setModalMode] = useState<'create' | 'edit' | null>(null);
  const [name, setName] = useState('');
  const [isShared, setIsShared] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const activeView = views.find(view => view.id === activeViewId);
  const isOwner = activeView?.isOwner === true;

  const openCreate = () => {
    setName('');
    setIsShared(false);
    setModalMode('create');
  };

  const openEdit = () => {
    if (!activeView) return;
    setName(activeView.name);
    setIsShared(activeView.isShared);
    setModalMode('edit');
  };

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      message.warning('请输入视图名称');
      return;
    }
    setSubmitting(true);
    try {
      if (modalMode === 'create') {
        await onCreate({ name: trimmed, filters: currentFilters, isShared });
        message.success('视图已保存');
      } else if (modalMode === 'edit' && activeView) {
        await onUpdate(activeView.id, { name: trimmed, filters: currentFilters, isShared });
        message.success('视图已更新');
      }
      setModalMode(null);
    } catch (err) {
      message.error(err instanceof Error && err.message ? err.message : '操作失败');
    } finally {
      setSubmitting(false);
    }
  };

  const confirmDelete = () => {
    if (!activeView) return;
    Modal.confirm({
      title: `删除视图「${activeView.name}」？`,
      content: '删除后不可恢复；分享给同 provider 的可见性同时移除。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: () => onDelete(activeView.id),
    });
  };

  const menuItems: MenuProps['items'] = [
    { key: 'save', label: '保存当前筛选为视图', onClick: openCreate },
    {
      key: 'default',
      label: '设为默认视图',
      disabled: !isOwner,
      onClick: () => activeView && void onSetDefault(activeView.id),
    },
    { key: 'edit', label: '编辑视图', disabled: !isOwner, onClick: openEdit },
    { type: 'divider' },
    { key: 'delete', label: '删除视图', danger: true, disabled: !isOwner, onClick: confirmDelete },
  ];

  return (
    <Space size={4} data-testid="workbench-saved-views">
      <Select
        allowClear
        placeholder="保存的视图"
        style={{ width: 200 }}
        value={activeViewId}
        onChange={value => {
          if (value === undefined || value === null) {
            onClear();
            return;
          }
          const view = views.find(item => item.id === value);
          if (view) onApply(view);
        }}
        options={views.map(view => ({
          value: view.id,
          label: `${view.isDefault ? '★ ' : ''}${view.name}${view.isOwner ? '' : '（分享）'}`,
        }))}
        data-testid="workbench-view-select"
      />
      <Dropdown menu={{ items: menuItems }} trigger={['click']}>
        <Button icon={<Bookmark size={14} />} data-testid="workbench-view-menu">
          视图
        </Button>
      </Dropdown>
      <Modal
        open={modalMode !== null}
        title={modalMode === 'create' ? '保存当前筛选为视图' : '编辑视图'}
        okText="保存"
        cancelText="取消"
        confirmLoading={submitting}
        onOk={() => void submit()}
        onCancel={() => setModalMode(null)}
        destroyOnHidden
      >
        <Form layout="vertical">
          <Form.Item label="视图名称" required>
            <Input
              value={name}
              onChange={event => setName(event.target.value)}
              maxLength={60}
              placeholder="如：Acme 高优未关闭"
              data-testid="workbench-view-name"
            />
          </Form.Item>
          <Form.Item label="同 provider 分享" style={{ marginBottom: 0 }}>
            <Switch
              checked={isShared}
              onChange={setIsShared}
              data-testid="workbench-view-shared"
            />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
