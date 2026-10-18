import React, { useEffect, useMemo, useState } from 'react';

import AppSelect from '@/components/ui/AppSelect';
import { usePermissions } from '@/lib/hooks/use-permissions';
import projectService, {
  type Project,
  type ProjectStatus,
  type RecordDomain,
} from '@/lib/services/project-service';

/**
 * ProjectSelect —— 「创建单据时选择所属项目」共享选择器（PE-5 增补）。
 *
 * - 数据面：GET /api/v1/projects（需 project:read），只列出可挂接项目（排除终态 completed/cancelled，
 *   与后端 ValidateAttach 口径一致）；
 * - 可嵌入 Form.Item（受控 value/onChange），也可独立受控使用；
 * - 权限预判由 useProjectLinkPermission 提供（前端预判，最终以后端动态复核 project:write 为准）。
 */

/** 终态项目不可再挂接（与后端 ValidateAttach 一致）。 */
const TERMINAL_PROJECT_STATUS: ProjectStatus[] = ['completed', 'cancelled'];

/** 各域"加入/移出项目"所需的域写权限码（与后端路由声明一致：工单为 ticket:update，其余为 <domain>:write）。 */
const DOMAIN_WRITE_PERMISSION: Record<RecordDomain, [resource: string, action: string]> = {
  ticket: ['ticket', 'update'],
  incident: ['incident', 'write'],
  problem: ['problem', 'write'],
  change: ['change', 'write'],
  service_request: ['service_request', 'write'],
};

export interface ProjectLinkPermission {
  /** 可见项目列表/项目详情（数据面 project:read）。 */
  canRead: boolean;
  /** 项目写权限（project:write，后端动态复核）。 */
  canWrite: boolean;
  /** 单据所在域的写权限码。 */
  canWriteRecord: boolean;
  /** 允许在创建页选择项目 / 在详情页加入项目。 */
  canAttach: boolean;
}

/** 项目挂接权限预判（最终以后端为准）。 */
export const useProjectLinkPermission = (domain: RecordDomain): ProjectLinkPermission => {
  const { hasPermission } = usePermissions();
  const canRead = hasPermission('project', 'read');
  const canWrite = hasPermission('project', 'write');
  const [resource, action] = DOMAIN_WRITE_PERMISSION[domain];
  const canWriteRecord = hasPermission(resource, action);
  return { canRead, canWrite, canWriteRecord, canAttach: canRead && canWrite && canWriteRecord };
};

export interface ProjectSelectProps {
  value?: number;
  onChange?: (value?: number) => void;
  placeholder?: string;
  disabled?: boolean;
  allowClear?: boolean;
  style?: React.CSSProperties;
  /** 仅列出可挂接项目（默认 true；排除已结项/取消）。 */
  attachableOnly?: boolean;
  'aria-label'?: string;
}

const ProjectSelect: React.FC<ProjectSelectProps> = ({
  value,
  onChange,
  placeholder = '选择所属项目',
  disabled,
  allowClear = true,
  style,
  attachableOnly = true,
  'aria-label': ariaLabel = '选择所属项目',
}) => {
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    projectService
      .listProjects({ page: 1, pageSize: 100 })
      .then(result => {
        if (!active) return;
        setProjects(result.items ?? result.projects ?? []);
      })
      .catch(() => {
        // 静默降级：无 project:read 或网络失败时保留空列表，不打断创建流程。
        if (active) setProjects([]);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  const options = useMemo(
    () =>
      projects
        .filter(project => !attachableOnly || !TERMINAL_PROJECT_STATUS.includes(project.status))
        .map(project => ({
          label: project.code ? `${project.name}（${project.code}）` : project.name,
          value: project.id,
        })),
    [projects, attachableOnly]
  );

  return (
    <AppSelect
      value={value}
      onChange={onChange}
      options={options}
      loading={loading}
      placeholder={loading ? '加载项目中…' : placeholder}
      disabled={disabled}
      allowClear={allowClear}
      style={style}
      aria-label={ariaLabel}
      notFoundContent={loading ? undefined : '暂无可挂接项目'}
    />
  );
};

export default ProjectSelect;
