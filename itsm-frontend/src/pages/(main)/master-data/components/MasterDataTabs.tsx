import { Tabs } from 'antd';
import { useLocation, useNavigate } from 'react-router';
import { useCan } from '@/components/auth/PermissionGate';

/**
 * 主数据模块的二级导航。
 *
 * 路由保持扁平（便于权限门禁与路由清单校验），页面通过本组件共享同一套
 * 模块入口；权限不足的页签不渲染。
 */
export function MasterDataTabs() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const canCustomers = useCan(['customer_master:read']);
  const canContracts = useCan(['support_contract:read']);
  const canSources = useCan(['customer_master:read']);

  const items = [
    canCustomers ? { key: '/master-data/customers', label: '客户主数据' } : null,
    canContracts ? { key: '/master-data/contracts', label: '支持合同' } : null,
    canSources ? { key: '/master-data/sources', label: '来源组织' } : null,
  ].filter((item): item is { key: string; label: string } => item !== null);

  if (items.length === 0) return null;

  return (
    <Tabs
      activeKey={pathname}
      items={items}
      onChange={key => navigate(key)}
      className="master-data-tabs"
    />
  );
}

export default MasterDataTabs;
