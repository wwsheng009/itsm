/**
 * 业务单据统一的“所属部门”选择器与只读名称组件。
 *
 * 数据源固定为当前租户的部门树；模块级缓存避免每个表单/详情各自重复拉取。
 * departmentId 是可选字段，0/null 表示未指定。
 */

import React, { useEffect, useMemo, useState } from 'react';
import { Select, Typography } from 'antd';
import { departmentService, type Department } from '@/lib/services/department-service';
import { getTenantId } from '@/lib/auth/token-storage';

const { Text } = Typography;

export interface DepartmentOption {
  value: number;
  label: string;
}

// 按租户隔离缓存：切换租户后不得复用上一个租户的部门树。
const departmentTreeCache = new Map<string, Promise<Department[]>>();

function loadDepartmentTree(): Promise<Department[]> {
  const tenantKey = getTenantId() ?? 'anonymous';
  const cached = departmentTreeCache.get(tenantKey);
  if (cached) {
    return cached;
  }
  const promise = departmentService.getDepartmentTree().catch((error) => {
    departmentTreeCache.delete(tenantKey);
    throw error;
  });
  departmentTreeCache.set(tenantKey, promise);
  return promise;
}

export function flattenDepartmentOptions(
  tree: Department[],
  level = 0,
): DepartmentOption[] {
  const result: DepartmentOption[] = [];
  for (const department of tree) {
    const name = department.name || `部门 #${department.id}`;
    result.push({
      value: department.id,
      label: level > 0 ? `${'　'.repeat(level)}${name}` : name,
    });
    if (department.children?.length) {
      result.push(...flattenDepartmentOptions(department.children, level + 1));
    }
  }
  return result;
}

export function useDepartmentOptions(): {
  options: DepartmentOption[];
  loading: boolean;
} {
  const [options, setOptions] = useState<DepartmentOption[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    loadDepartmentTree()
      .then((tree) => {
        if (active) setOptions(flattenDepartmentOptions(tree));
      })
      .catch(() => {
        if (active) setOptions([]);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  return { options, loading };
}

export interface DepartmentSelectProps {
  value?: number | null;
  onChange?: (value?: number) => void;
  disabled?: boolean;
  allowClear?: boolean;
  placeholder?: string;
  style?: React.CSSProperties;
  'aria-label'?: string;
}

export const DepartmentSelect: React.FC<DepartmentSelectProps> = ({
  value,
  onChange,
  disabled,
  allowClear = true,
  placeholder = '选择所属部门',
  style,
  'aria-label': ariaLabel = '选择所属部门',
}) => {
  const { options, loading } = useDepartmentOptions();

  return (
    <Select
      value={value ?? undefined}
      onChange={onChange}
      disabled={disabled}
      allowClear={allowClear}
      showSearch
      optionFilterProp="label"
      loading={loading}
      options={options}
      placeholder={loading ? '加载部门中…' : placeholder}
      style={{ width: '100%', ...style }}
      aria-label={ariaLabel}
      notFoundContent={loading ? undefined : '暂无部门'}
    />
  );
};

export interface DepartmentNameProps {
  departmentId?: number | null;
  fallback?: React.ReactNode;
}

/** 只读部门名称：无 ID 显示 fallback；未知 ID 降级为“部门 #id”。 */
export const DepartmentName: React.FC<DepartmentNameProps> = ({
  departmentId,
  fallback = '-',
}) => {
  const { options } = useDepartmentOptions();
  const name = useMemo(
    () => options.find((option) => option.value === departmentId)?.label,
    [departmentId, options],
  );

  if (!departmentId) return <>{fallback}</>;
  return <Text>{name ?? `部门 #${departmentId}`}</Text>;
};

export default DepartmentSelect;