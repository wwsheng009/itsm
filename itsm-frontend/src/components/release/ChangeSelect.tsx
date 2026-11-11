import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Select, Spin } from 'antd';

import { ChangeApi, type Change } from '@/lib/api/change-api';

interface ChangeOption {
  value: number;
  label: string;
}

interface ChangeSelectProps {
  value?: number;
  onChange?: (value?: number) => void;
  disabled?: boolean;
  id?: string;
}

function toOption(change: Change): ChangeOption {
  return {
    value: change.id,
    label: `${change.title} · #${change.id}`,
  };
}

/**
 * 发布单的关联变更选择器。
 *
 * 后端 `releases.change_id` 只存数字 ID，且 Create/Update 会校验变更属于当前租户；
 * 这里按标题/编号远程搜索，避免把全量变更列表拉进页面。
 */
export const ChangeSelect: React.FC<ChangeSelectProps> = ({ value, onChange, disabled, id }) => {
  const [options, setOptions] = useState<ChangeOption[]>([]);
  const [loading, setLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const loadedIdsRef = useRef<Set<number>>(new Set());

  const fetchOptions = useCallback(async (search?: string) => {
    setLoading(true);
    try {
      const result = await ChangeApi.getChanges({
        page: 1,
        pageSize: 20,
        search: search?.trim() || undefined,
      });
      const next = (result.changes ?? []).map(toOption);
      next.forEach(item => loadedIdsRef.current.add(item.value));
      setOptions(next);
    } catch {
      // 搜索失败保留已有选项，不阻塞表单其余字段填写。
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchOptions();
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [fetchOptions]);

  // 编辑态：回填的变更可能不在默认搜索结果内，按 ID 补一条可读选项。
  useEffect(() => {
    if (!value || value <= 0 || loadedIdsRef.current.has(value)) return;
    let cancelled = false;
    void ChangeApi.getChange(value)
      .then(change => {
        if (cancelled) return;
        loadedIdsRef.current.add(value);
        setOptions(prev =>
          prev.some(item => item.value === value) ? prev : [toOption(change), ...prev],
        );
      })
      .catch(() => {
        // 变更不可见/已删除时保留原始 ID，由后端在提交时给出明确校验错误。
      });
    return () => {
      cancelled = true;
    };
  }, [value]);

  const handleSearch = useCallback(
    (term: string) => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        void fetchOptions(term);
      }, 300);
    },
    [fetchOptions],
  );

  return (
    <Select<number>
      id={id}
      allowClear
      showSearch
      filterOption={false}
      loading={loading}
      options={options}
      value={value}
      onChange={onChange}
      onSearch={handleSearch}
      disabled={disabled}
      notFoundContent={loading ? <Spin size="small" /> : undefined}
      placeholder="搜索并选择变更"
      data-testid="release-change-select"
    />
  );
};

export default ChangeSelect;
