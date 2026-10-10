import React from 'react';
import { Tooltip } from 'antd';
import { useAuthStore } from '@/lib/store/auth-store';
import { permissionGranted } from '@/routes/route-permissions';

/**
 * 入口级权限门禁（FE 权限收敛的入口层，与路由级 route-permissions.ts 同一权限源与判定语义）。
 *
 * 用法：
 *   <PermissionGate anyOf={['knowledge:write']}>
 *     <Button ...>新建文章</Button>
 *   </PermissionGate>
 *
 * 语义：
 * - 默认 fail-closed：无权限不渲染（fallback 可指定替代内容，默认 null）；
 * - `mode="disable"`：保留占位但禁用（适合入口不宜消失的场景），悬浮提示「无权限执行此操作」；
 * - 判定与后端 `middleware/rbac.go checkPermissionMatch` 对齐（`*` / `resource:*` /
 *   `resource:admin` 蕴含；MCP 例外），并与路由级门禁共用同一实现，避免双源漂移。
 */

/** 判定当前用户是否命中任一权限码（响应 store 变更）。 */
export function useCan(anyOf: string[]): boolean {
  const permissions = useAuthStore(state => state.user?.permissions);
  return anyOf.some(code => permissionGranted(permissions, code));
}

interface PermissionGateProps {
  /** 命中任意一个权限码即放行。 */
  anyOf: string[];
  /** 受控元素（hide 模式不渲染；disable 模式克隆为 disabled）。 */
  children: React.ReactElement;
  /** hide 模式下无权限时的替代内容（默认 null，即隐藏）。 */
  fallback?: React.ReactNode;
  mode?: 'hide' | 'disable';
  disabledTip?: string;
}

export const PermissionGate: React.FC<PermissionGateProps> = ({
  anyOf,
  children,
  fallback = null,
  mode = 'hide',
  disabledTip = '无权限执行此操作',
}) => {
  const allowed = useCan(anyOf);

  if (allowed) return <>{children}</>;

  if (mode === 'disable') {
    return (
      <Tooltip title={disabledTip}>
        {React.cloneElement(children as React.ReactElement<{ disabled?: boolean }>, {
          disabled: true,
        })}
      </Tooltip>
    );
  }

  return <>{fallback}</>;
};

export default PermissionGate;
