/**
 * MspProviderGate：跨租户数据面的「服务商上下文」门禁（hide 语义）。
 *
 * 与 PermissionGate 的区别：PermissionGate 只判定权限码；本组件叠加
 * 「当前租户上下文为服务商租户（msp_provider / 历史别名 msp）」条件。
 *
 * 为什么必须叠加（2026-10-13 客户可见性缺陷）：
 * 客户租户的角色克隆（IT总监/系统运维/运维总监/服务商租户管理员）会残留
 * 跨租户 `msp_*` 码，仅凭 `msp_ticket:read` 会把客户用户误放行进跨租户分区；
 * 后端 IsMSP 判定 = 租户类型 + msp_role（fail-closed），本组件按同向收敛：
 * 租户类型不符 → 不渲染（也不发请求）；类型符合再按操作码判定。
 */
import React from 'react';
import { PermissionGate } from '@/components/auth/PermissionGate';
import { useAuthStore } from '@/lib/store/auth-store';
import { isMspProviderTenant, MSP_WORKBENCH_PERMISSIONS } from '@/routes/area-permissions';

export interface MspProviderGateProps {
  children: React.ReactElement;
}

export const MspProviderGate: React.FC<MspProviderGateProps> = ({ children }) => {
  const tenantType = useAuthStore(state => state.currentTenant?.type);

  if (!isMspProviderTenant(tenantType)) {
    return null;
  }

  return <PermissionGate anyOf={MSP_WORKBENCH_PERMISSIONS}>{children}</PermissionGate>;
};

export default MspProviderGate;
