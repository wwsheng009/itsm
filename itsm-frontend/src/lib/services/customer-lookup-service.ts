import { httpClient } from '@/lib/api/http-client';

/**
 * 业务单据客户选择的轻量查询服务。
 *
 * 与主数据管理页的 `/api/v1/master-data/customers` 不同，该接口只返回
 * id/name/code 等展示字段，供所有已认证租户用户选择「所属客户」，
 * 不要求 `customer_master:read` 维护权限。
 */
export interface CustomerOption {
  id: number;
  name: string;
  code?: string;
  shortName?: string;
  tier?: string;
}

interface CustomerOptionListResponse {
  items?: CustomerOption[];
}

class CustomerLookupService {
  async listCustomers(keyword?: string): Promise<CustomerOption[]> {
    const params = keyword?.trim() ? { keyword: keyword.trim() } : undefined;
    const data = await httpClient.get<CustomerOptionListResponse>(
      '/api/v1/lookups/customers',
      params
    );
    return data?.items ?? [];
  }
}

export const customerLookupService = new CustomerLookupService();
export default customerLookupService;