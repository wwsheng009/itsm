/**
 * TicketListShell —— 工单列表壳（§13.1 复用收敛 #2）。
 *
 * 收敛：antd Table 默认项（rowKey/loading/scroll/size）、行点击守卫（自动忽略
 * 按钮/链接/下拉/勾选区域）、空态文案、错误态（Alert + 重试）、分页透传
 * （页码模式）与游标「加载更多」页脚。
 * 不收敛：列定义、筛选、选择态、行内操作与数据获取（由调用方注入）。
 */
import React from 'react';
import { Alert, Button, Table } from 'antd';
import type { TableProps } from 'antd';

export interface TicketListCursor {
  hasMore: boolean;
  loading?: boolean;
  onLoadMore: () => void;
  label?: string;
  testId?: string;
}

export interface TicketListShellProps<T> {
  columns: TableProps<T>['columns'];
  dataSource: T[];
  loading?: boolean;
  /** 请求错误：渲染 Alert + 可选重试按钮（不传则不渲染错误区）。 */
  error?: string | null;
  onRetry?: () => void;
  /** 行点击（自动忽略按钮/链接/下拉/勾选等交互元素内部的点击）。 */
  onRowClick?: (record: T) => void;
  rowSelection?: TableProps<T>['rowSelection'];
  /** 页码分页透传；缺省 false（游标模式或纯展示）。 */
  pagination?: TableProps<T>['pagination'];
  onChange?: TableProps<T>['onChange'];
  scroll?: TableProps<T>['scroll'];
  size?: TableProps<T>['size'];
  rowClassName?: TableProps<T>['rowClassName'];
  onRow?: TableProps<T>['onRow'];
  emptyText?: string;
  /** 游标分页（服务端 nextCursor 模式）：渲染「加载更多」页脚。 */
  cursor?: TicketListCursor;
  getPopupContainer?: TableProps<T>['getPopupContainer'];
  testId?: string;
}

/** 行点击守卫：命中交互元素（按钮/链接/下拉/勾选）时不触发行点击。 */
const INTERACTIVE_SELECTOR = 'button,a,.ant-dropdown-trigger,.ant-checkbox-wrapper';

export default function TicketListShell<T extends { id: number | string }>({
  columns,
  dataSource,
  loading = false,
  error = null,
  onRetry,
  onRowClick,
  rowSelection,
  pagination = false,
  onChange,
  scroll,
  size,
  rowClassName,
  onRow,
  emptyText,
  cursor,
  getPopupContainer,
  testId,
}: TicketListShellProps<T>) {
  const mergedOnRow: TableProps<T>['onRow'] | undefined =
    onRow || onRowClick
      ? (record, index) => {
          const base = onRow?.(record, index) ?? {};
          return {
            ...base,
            onClick: (event: React.MouseEvent<HTMLElement>) => {
              (base.onClick as ((e: React.MouseEvent<HTMLElement>) => void) | undefined)?.(event);
              if (!onRowClick) return;
              const target = event.target as HTMLElement;
              if (target.closest(INTERACTIVE_SELECTOR)) return;
              onRowClick(record);
            },
          };
        }
      : undefined;

  return (
    <div data-testid={testId}>
      {error && (
        <Alert
          type="error"
          showIcon
          message={error}
          style={{ marginBottom: 16 }}
          action={
            onRetry ? (
              <Button size="small" onClick={() => onRetry()}>
                重试
              </Button>
            ) : undefined
          }
        />
      )}
      <Table<T>
        rowKey="id"
        columns={columns}
        dataSource={dataSource}
        loading={loading}
        pagination={pagination}
        onChange={onChange}
        rowSelection={rowSelection}
        scroll={scroll}
        size={size}
        rowClassName={rowClassName}
        onRow={mergedOnRow}
        getPopupContainer={getPopupContainer}
        locale={emptyText ? { emptyText } : undefined}
      />
      {cursor?.hasMore && (
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Button
            onClick={() => cursor.onLoadMore()}
            loading={cursor.loading}
            data-testid={cursor.testId}
          >
            {cursor.label ?? '加载更多'}
          </Button>
        </div>
      )}
    </div>
  );
}
