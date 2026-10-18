import React, { useCallback, useEffect, useState } from 'react';

import { App, Button, Modal, Popconfirm, Space, Spin, Tag, Typography } from 'antd';
import { Link } from 'react-router';
import { FolderKanban, FolderPlus } from 'lucide-react';

import ProjectSelect, { useProjectLinkPermission } from '@/components/business/ProjectSelect';
import projectService, {
  type ProjectStatus,
  type RecordDomain,
  type RecordProjectView,
} from '@/lib/services/project-service';

/**
 * RecordProjectLink —— 单据详情页「所属项目」展示 + 加入/移出（PE-5 增补）。
 *
 * - 数据面：GET /api/v1/{domain}/:id/project（项目不可见时后端返回 project=null，不泄露）；
 * - 写操作：POST/DELETE /api/v1/{domain}/:id/project（后端动态复核 project:write + 域写码）；
 * - 权限：读需 project:read（决定项目名是否可点进详情）；加入/移出需 project:write + 该域写码；
 * - MSP 跨客户只读面可传 showWhenEmpty=false 且无写权限时整体不渲染。
 */

export interface RecordProjectLinkProps {
  domain: RecordDomain;
  recordId?: number | string | null;
  /** 未关联时是否仍展示「未关联（+加入项目）」（默认 true）。 */
  showWhenEmpty?: boolean;
  /** 是否渲染「所属项目：」前缀（默认 true；嵌入 Descriptions 等自带 label 的场景可传 false）。 */
  showLabel?: boolean;
  className?: string;
  style?: React.CSSProperties;
}

const STATUS_META: Record<ProjectStatus, { label: string; color: string }> = {
  planning: { label: '规划中', color: 'blue' },
  active: { label: '进行中', color: 'green' },
  suspended: { label: '已挂起', color: 'orange' },
  completed: { label: '已完成', color: 'default' },
  cancelled: { label: '已取消', color: 'default' },
};

const RecordProjectLink: React.FC<RecordProjectLinkProps> = ({
  domain,
  recordId,
  showWhenEmpty = true,
  showLabel = true,
  className,
  style,
}) => {
  const { message } = App.useApp();
  const { canRead, canWrite, canWriteRecord, canAttach } = useProjectLinkPermission(domain);
  const numericId = typeof recordId === 'string' ? Number(recordId) : recordId;
  const validId = typeof numericId === 'number' && Number.isFinite(numericId) && numericId > 0;

  const [loading, setLoading] = useState(false);
  const [project, setProject] = useState<RecordProjectView | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [selectedProjectId, setSelectedProjectId] = useState<number | undefined>();
  const [submitting, setSubmitting] = useState(false);
  const labelPrefix = showLabel ? <Typography.Text type="secondary">所属项目：</Typography.Text> : null;

  const load = useCallback(async () => {
    if (!validId) return;
    setLoading(true);
    try {
      const result = await projectService.getRecordProject(domain, numericId as number);
      setProject(result?.project ?? null);
      setLoadFailed(false);
    } catch {
      // 静默：单据已删除（404）或瞬时失败时不打断详情页其余信息。
      setProject(null);
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  }, [domain, numericId, validId]);

  useEffect(() => {
    void load();
  }, [load]);

  const openModal = () => {
    setSelectedProjectId(undefined);
    setModalOpen(true);
  };

  const handleAttach = async () => {
    if (!validId || !selectedProjectId) return;
    setSubmitting(true);
    try {
      await projectService.attachRecord(domain, numericId as number, selectedProjectId);
      message.success('已加入项目');
      setModalOpen(false);
      await load();
    } catch {
      // 错误提示由 httpClient 统一弹出（403/404/400 语义由后端给出）。
    } finally {
      setSubmitting(false);
    }
  };

  const handleDetach = async () => {
    if (!validId) return;
    try {
      await projectService.detachRecord(domain, numericId as number);
      message.success('已移出项目');
      await load();
    } catch {
      // 静默：错误提示由 httpClient 统一弹出。
    }
  };

  if (!validId || loadFailed) {
    return null;
  }

  if (loading) {
    return (
      <Space size={6} className={className} style={style}>
        {labelPrefix}
        <Spin size="small" />
      </Space>
    );
  }

  if (!project) {
    if (!showWhenEmpty) {
      return null;
    }
    return (
      <Space size={6} wrap className={className} style={style}>
        {labelPrefix}
        <Typography.Text type="secondary">未关联</Typography.Text>
        {canAttach && (
          <Button type="link" size="small" icon={<FolderPlus className="w-3.5 h-3.5" />} onClick={openModal}>
            加入项目
          </Button>
        )}
        <AttachModal
          open={modalOpen}
          submitting={submitting}
          selectedProjectId={selectedProjectId}
          onSelect={setSelectedProjectId}
          onOk={handleAttach}
          onCancel={() => setModalOpen(false)}
        />
      </Space>
    );
  }

  const statusMeta = STATUS_META[project.status] ?? { label: project.status, color: 'default' };

  return (
    <Space size={6} wrap className={className} style={style}>
      {labelPrefix}
      {canRead ? (
        <Link to={`/projects/${project.id}`} className="inline-flex items-center gap-1">
          <FolderKanban className="w-3.5 h-3.5" />
          {project.name}
        </Link>
      ) : (
        <Typography.Text>{project.name}</Typography.Text>
      )}
      {project.code && <Typography.Text type="secondary">（{project.code}）</Typography.Text>}
      <Tag color={statusMeta.color}>{statusMeta.label}</Tag>
      {canWrite && canWriteRecord && (
        <Popconfirm title="确定将该单据移出项目？" okText="移出" cancelText="取消" onConfirm={handleDetach}>
          <Button type="link" size="small" danger>
            移出
          </Button>
        </Popconfirm>
      )}
    </Space>
  );
};

interface AttachModalProps {
  open: boolean;
  submitting: boolean;
  selectedProjectId?: number;
  onSelect: (value?: number) => void;
  onOk: () => void;
  onCancel: () => void;
}

const AttachModal: React.FC<AttachModalProps> = ({
  open,
  submitting,
  selectedProjectId,
  onSelect,
  onOk,
  onCancel,
}) => (
  <Modal
    open={open}
    title="加入项目"
    okText="加入"
    cancelText="取消"
    confirmLoading={submitting}
    okButtonProps={{ disabled: !selectedProjectId }}
    onOk={onOk}
    onCancel={onCancel}
    destroyOnHidden
  >
    <ProjectSelect value={selectedProjectId} onChange={onSelect} style={{ width: '100%' }} />
  </Modal>
);

export default RecordProjectLink;
