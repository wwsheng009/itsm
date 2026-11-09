/**
 * 业务单据统一的“所属客户”选择器与只读名称组件。
 *
 * 数据源为租户内轻量客户查询接口（仅 id/name/code），模块级缓存避免
 * 每个表单/详情重复拉取。customerId 为可选字段，0/null 表示未指定。
 */

import React, { useEffect, useMemo, useState } from 'react';
import { Select, Typography } from 'antd';
import {
  customerLookupService,
  type CustomerOption as ApiCustomerOption,
} from '@/lib/services/customer-lookup-service';
import { getTenantId } from '@/lib/auth/token-storage';

const { Text } = Typography;

export interface CustomerOption {
  value: number;
  label: string;
  name: string;
  code?: string;
}

// 按租户隔离缓存：切换租户后不得复用上一个租户的客户列表。
const customerListCache = new Map<string, Promise<ApiCustomerOption[]>>();

function loadCustomers(): Promise<ApiCustomerOption[]> {
  const tenantKey = getTenantId() ?? 'anonymous';
  const cached = customerListCache.get(tenantKey);
  if (cached) {
    return cached;
  }
  const promise = customerLookupService.listCustomers().catch(error => {
    customerListCache.delete(tenantKey);
    throw error;
  });
  customerListCache.set(tenantKey, promise);
  return promise;
}

export function toCustomerOptions(customers: ApiCustomerOption[]): CustomerOption[] {
  return customers.map(customer => ({
    value: customer.id,
    name: customer.name,
    label: customer.code ? `${customer.name}（${customer.code}）` : customer.name,
    code: customer.code,
  }));
}

export function useCustomerOptions(): {
  options: CustomerOption[];
  loading: boolean;
} {
  const [options, setOptions] = useState<CustomerOption[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    loadCustomers()
      .then(customers => {
        if (active) setOptions(toCustomerOptions(customers));
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

export interface CustomerSelectProps {
  value?: number | null;
  onChange?: (value?: number) => void;
  disabled?: boolean;
  allowClear?: boolean;
  placeholder?: string;
  style?: React.CSSProperties;
  'aria-label'?: string;
}

export const CustomerSelect: React.FC<CustomerSelectProps> = ({
  value,
  onChange,
  disabled,
  allowClear = true,
  placeholder = '选择所属客户',
  style,
  'aria-label': ariaLabel = '选择所属客户',
}) => {
  const { options, loading } = useCustomerOptions();

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
      placeholder={loading ? '加载客户中…' : placeholder}
      style={{ width: '100%', ...style }}
      aria-label={ariaLabel}
      notFoundContent={loading ? undefined : '暂无客户'}
    />
  );
};

export interface CustomerNameProps {
  customerId?: number | null;
  fallback?: React.ReactNode;
}

/** 只读客户名称：无 ID 显示 fallback；未知 ID 降级为“客户 #id”。 */
export const CustomerName: React.FC<CustomerNameProps> = ({
  customerId,
  fallback = '-',
}) => {
  const { options } = useCustomerOptions();
  const name = useMemo(
    () => options.find(option => option.value === customerId)?.name,
    [customerId, options],
  );

  if (!customerId) return <>{fallback}</>;
  return <Text>{name ?? `客户 #${customerId}`}</Text>;
};

export default CustomerSelect;