/**
 * DeepTenantReturn —— 深度切换「返回工作台」入口（MSW-4 / WB-A6）。
 *
 * 背景（2026-11 功能缺口）：
 * - `CustomerFilter` 受 provider 门禁（`canUseMspWorkbench`）保护，切到客户租户后
 *   整体不渲染（避免请求必然 403 的 `/msp/*` 数据面）；
 * - 原「当前客户：X（深度操作中）+ 返回工作台」位于 `CustomerFilter` 内部，
 *   于是深度态下入口随之消失，用户无路切回 provider。
 *
 * 本组件与数据面过滤器解耦：
 * - 只读 auth store，不发起任何 `/msp/*` 请求；
 * - 仅在「服务商员工 + 当前会话已切到非归属客户租户」时渲染；
 * - 返回语义 = 先 `switchTenant(homeTenantId)` 切回 provider 作用域，成功后再导航。
 *   仅导航会让 `/msp/workbench` 在客户作用域被 RBAC 拒绝（MSW-4 / Q4=A）。
 */
import React, { useCallback, useState } from 'react';
import { useNavigate } from 'react-router';
import { Button, Tag, message } from 'antd';
import { Undo2 } from 'lucide-react';
import { useAuthStore } from '@/lib/store/auth-store';
import { MSP_WORKBENCH_PATH } from '@/lib/api/msp-workbench-api';
import { canReturnToProviderTenant } from '@/routes/area-permissions';

export interface DeepTenantReturnProps {
  /** 预留：外部容器定制（Header 集成不需要传参）。 */
  className?: string;
}

export const DeepTenantReturn: React.FC<DeepTenantReturnProps> = ({ className }) => {
  const navigate = useNavigate();
  const currentTenant = useAuthStore(state => state.currentTenant);
  const user = useAuthStore(state => state.user);
  const switchTenant = useAuthStore(state => state.switchTenant);
  const [backLoading, setBackLoading] = useState(false);

  // homeTenantId 为权威字段（MSW-4 契约：/auth/me 的 homeTenantId=归属租户）。
  // 不能用 user.tenantId 兜底：切换后它已是目标客户租户。
  const homeTenantId = user?.homeTenantId ?? null;

  const visible = canReturnToProviderTenant({
    currentTenantId: currentTenant?.id,
    currentTenantType: currentTenant?.type,
    homeTenantId,
    mspRole: user?.mspRole,
  });

  const handleBackToWorkbench = useCallback(async () => {
    if (!homeTenantId) {
      message.error('无法确定服务商租户，请重新登录后再试');
      return;
    }
    setBackLoading(true);
    try {
      // MSW-4 / Q4=A：必须先切回 provider 作用域，再导航到工作台。
      await switchTenant(homeTenantId);
      navigate(MSP_WORKBENCH_PATH);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '返回工作台失败');
    } finally {
      setBackLoading(false);
    }
  }, [homeTenantId, navigate, switchTenant]);

  if (!visible) return null;

  return (
    <span className={className} style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <Tag color="orange" data-testid="deep-tenant-indicator" style={{ margin: 0 }}>
        当前客户：{currentTenant?.name || '客户'}（深度操作中）
      </Tag>
      <Button
        size="small"
        icon={<Undo2 size={14} />}
        loading={backLoading}
        onClick={handleBackToWorkbench}
        data-testid="back-to-workbench"
        title="切回服务商作用域并打开工作台"
      >
        返回工作台
      </Button>
    </span>
  );
};

export default DeepTenantReturn;
