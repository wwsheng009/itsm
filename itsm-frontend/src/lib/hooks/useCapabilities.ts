
import { useQuery } from '@tanstack/react-query';
import {
  capabilityAllows,
  getCapabilities,
  type ProductCapabilityState,
} from '@/lib/api/capability-api';
import { useAuthStore } from '@/lib/store/auth-store';

export const useCapabilities = () => {
  // IP-P0-8：能力位查询按租户分键（能力/菜单随作用域变化）。
  const tenantId = useAuthStore(state => state.currentTenant?.id ?? 0);
  const query = useQuery({
    queryKey: ['product-capabilities', tenantId],
    queryFn: getCapabilities,
    staleTime: 60_000,
    retry: 1,
  });
  const capabilities = query.data?.items || [];

  return {
    ...query,
    capabilities,
    allows: (key: string, action = 'read') => capabilityAllows(capabilities, key, action),
    find: (key: string): ProductCapabilityState | undefined =>
      capabilities.find(item => item.key === key),
  };
};
