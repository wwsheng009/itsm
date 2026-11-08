import { httpClient } from '@/lib/api/http-client';
import type {
  CustomerBranch,
  ExternalContractReference,
  ServiceCustomer,
  SourceOrganization,
  SupportContract,
} from './emailIntakeService';

export type {
  CustomerBranch,
  ExternalContractReference,
  ServiceCustomer,
  SourceOrganization,
  SupportContract,
};

interface ListResponse<T> {
  items: T[];
  total: number;
}

/**
 * 客户主数据 API。
 *
 * 前端路由已从 `/email-intake/*` 归位到 `/master-data/*`；后端保留
 * `/api/v1/email-intake/*` 兼容别名，同时提供语义正确的
 * `/api/v1/master-data/*` 主数据面。
 */
class MasterDataService {
  private readonly baseUrl = '/api/v1/master-data';

  async customers(): Promise<ListResponse<ServiceCustomer>> {
    return httpClient.get(`${this.baseUrl}/customers`);
  }

  async createCustomer(
    payload: Omit<ServiceCustomer, 'id' | 'createdAt' | 'updatedAt'>
  ): Promise<ServiceCustomer> {
    return httpClient.post(`${this.baseUrl}/customers`, payload);
  }

  async updateCustomer(
    id: number,
    payload: Omit<ServiceCustomer, 'id' | 'createdAt' | 'updatedAt'>
  ): Promise<ServiceCustomer> {
    return httpClient.put(`${this.baseUrl}/customers/${id}`, payload);
  }

  async disableCustomer(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/customers/${id}`);
  }

  async branches(customerId?: number): Promise<ListResponse<CustomerBranch>> {
    return httpClient.get(
      `${this.baseUrl}/branches`,
      customerId ? { customerId } : undefined
    );
  }

  async createBranch(payload: Omit<CustomerBranch, 'id'>): Promise<CustomerBranch> {
    return httpClient.post(`${this.baseUrl}/branches`, payload);
  }

  async updateBranch(
    id: number,
    payload: { name: string; aliases?: string[]; customerId: number; status?: string }
  ): Promise<CustomerBranch> {
    return httpClient.put(`${this.baseUrl}/branches/${id}`, payload);
  }

  async disableBranch(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/branches/${id}`);
  }

  async contracts(): Promise<ListResponse<SupportContract>> {
    return httpClient.get(`${this.baseUrl}/support-contracts`);
  }

  async createContract(payload: Omit<SupportContract, 'id'>): Promise<SupportContract> {
    return httpClient.post(`${this.baseUrl}/support-contracts`, payload);
  }

  async updateContract(
    id: number,
    payload: Omit<SupportContract, 'id'>
  ): Promise<SupportContract> {
    return httpClient.put(`${this.baseUrl}/support-contracts/${id}`, payload);
  }

  async terminateContract(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/support-contracts/${id}`);
  }

  async sourceOrganizations(): Promise<ListResponse<SourceOrganization>> {
    return httpClient.get(`${this.baseUrl}/source-organizations`);
  }

  async createSourceOrganization(
    payload: Omit<SourceOrganization, 'id'>
  ): Promise<SourceOrganization> {
    return httpClient.post(`${this.baseUrl}/source-organizations`, payload);
  }

  async updateSourceOrganization(
    id: number,
    payload: Partial<SourceOrganization>
  ): Promise<SourceOrganization> {
    return httpClient.put(`${this.baseUrl}/source-organizations/${id}`, payload);
  }

  async disableSourceOrganization(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/source-organizations/${id}`);
  }

  async externalContractReferences(): Promise<ListResponse<ExternalContractReference>> {
    return httpClient.get(`${this.baseUrl}/external-contract-references`);
  }

  async createExternalContractReference(
    payload: Pick<
      ExternalContractReference,
      'sourceOrganizationId' | 'supportContractId' | 'externalContractNumber'
    >
  ): Promise<ExternalContractReference> {
    return httpClient.post(`${this.baseUrl}/external-contract-references`, payload);
  }

  async deleteExternalContractReference(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/external-contract-references/${id}`);
  }
}

export const masterDataService = new MasterDataService();
export default masterDataService;
