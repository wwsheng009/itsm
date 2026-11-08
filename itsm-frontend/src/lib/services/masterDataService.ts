import { httpClient } from '@/lib/api/http-client';
import type {
  CustomerBranch,
  CustomerContact,
  CustomerExternalID,
  CustomerTenantLink,
  ExternalContractReference,
  ServiceCustomer,
  SourceOrganization,
  SupportContract,
} from './emailIntakeService';

export type {
  CustomerBranch,
  CustomerContact,
  CustomerExternalID,
  CustomerTenantLink,
  ExternalContractReference,
  ServiceCustomer,
  SourceOrganization,
  SupportContract,
};

export interface PageResponse<T> {
  items: T[];
  total: number;
  page?: number;
  pageSize?: number;
  totalPages?: number;
}

export interface CustomerListParams {
  keyword?: string;
  tier?: string;
  status?: string;
  lifecycleStage?: string;
  accountManagerId?: number;
  industry?: string;
  hasTenantLink?: boolean;
  qualityBelow?: number;
  sortBy?: 'updatedAt' | 'qualityScore' | 'name';
  sortOrder?: 'asc' | 'desc';
  page?: number;
  pageSize?: number;
}

export interface CustomerDetailResponse {
  customer: ServiceCustomer;
  branches: CustomerBranch[];
  contacts: CustomerContact[];
  tenantLinks: CustomerTenantLink[];
  externalIds: CustomerExternalID[];
}

export interface CustomerWritePayload {
  name: string;
  code?: string;
  shortName?: string;
  legalName?: string;
  enName?: string;
  aliases?: string[];
  historicalNames?: string[];
  taxId?: string;
  dunsNumber?: string;
  registrationNo?: string;
  emailDomains?: string[];
  customerType?: string;
  industry?: string;
  subIndustry?: string;
  sizeClass?: string;
  ownershipType?: string;
  tier?: string;
  vipLevel?: number;
  creditRating?: string;
  website?: string;
  mainPhone?: string;
  serviceEmail?: string;
  address?: Record<string, unknown>;
  timezone?: string;
  language?: string;
  serviceTier?: string;
  slaDefinitionId?: number | null;
  supportHours?: string;
  coverageScope?: Record<string, unknown>;
  accountManagerId?: number | null;
  serviceOwnerId?: number | null;
  csManagerId?: number | null;
  oncallGroupId?: number | null;
  billingCurrency?: string;
  paymentTerms?: string;
  creditLimit?: number | null;
  invoiceTitle?: string;
  billingAddress?: Record<string, unknown>;
  lifecycleStage?: string;
  onboardedAt?: string | null;
  contractStartAt?: string | null;
  contractEndAt?: string | null;
  churnAt?: string | null;
  churnReason?: string;
  dataSource?: string;
  status?: string;
  linkedCustomerTenantId?: number | null;
  version?: number;
}

export interface BranchWritePayload {
  customerId: number;
  code?: string;
  name: string;
  branchType?: string;
  aliases?: string[];
  address?: Record<string, unknown>;
  timezone?: string;
  contactPhone?: string;
  emailDomains?: string[];
  serviceTier?: string;
  supportHours?: string;
  isPrimary?: boolean;
  linkedTenantId?: number | null;
  status?: string;
}

export interface CustomerContactWritePayload {
  branchId?: number | null;
  name: string;
  nameEn?: string;
  title?: string;
  department?: string;
  email?: string;
  mobile?: string;
  phone?: string;
  wechat?: string;
  contactRole?: string;
  isPrimary?: boolean;
  language?: string;
  timezone?: string;
  optIn?: boolean;
  linkedUserId?: number | null;
  source?: string;
  status?: string;
}

export interface CustomerTenantLinkWritePayload {
  linkedTenantId: number;
  linkType?: string;
  isBillingAnchor?: boolean;
  provisionStatus?: string;
  provisionedAt?: string | null;
  note?: string;
}

export interface CustomerExternalIDWritePayload {
  systemCode: string;
  externalId: string;
  externalUrl?: string;
}

export interface Customer360Overview {
  customerId: number;
  code: string;
  name: string;
  tier: string;
  lifecycleStage: string;
  status: string;
  dataQualityScore: number;
  accountManagerId?: number | null;
  accountManagerName: string;
  industry: string;
  customerType: string;
  branchCount: number;
  contactCount: number;
  tenantLinkCount: number;
  contractCount: number;
}

export interface Customer360SLAMetric {
  metricType: string;
  metricName: string;
  value: number;
  unit: string;
  measuredAt: string;
}

export interface Customer360ServiceHealth {
  complianceRate?: number;
  violations30d: number;
  openViolations: number;
  metrics: Customer360SLAMetric[];
}

export interface Customer360WorkItem {
  kind: 'ticket' | 'incident';
  id: number;
  number: string;
  title: string;
  status: string;
  priority: string;
  createdAt: string;
}

export interface Customer360TicketSection {
  ticketTotal: number;
  ticketOpen: number;
  ticketOverdue: number;
  incidentTotal: number;
  incidentOpen: number;
  incidentOverdue: number;
  p1p2: number;
  priorityDistribution: Record<string, number>;
  recent: Customer360WorkItem[];
}

export interface Customer360Contract {
  id: number;
  contractNumber: string;
  status: string;
  startAt?: string;
  endAt?: string;
  remainingDays?: number;
}

export interface Customer360ContractSection {
  activeCount: number;
  expiringSoon: number;
  renewalAlert: boolean;
  current?: Customer360Contract;
  items: Customer360Contract[];
}

export interface Customer360Project {
  id: number;
  code: string;
  name: string;
  type: string;
  status: string;
  health: string;
  progress: number;
  openRisks: number;
  milestoneRisks: number;
}

export interface Customer360ProjectSection {
  total: number;
  inFlight: number;
  red: number;
  yellow: number;
  openRisks: number;
  milestoneRisks: number;
  items: Customer360Project[];
}

export interface Customer360AssetSection {
  assetCount: number;
  ciCount: number;
  statusDistribution: Record<string, number>;
  keyAssets: string[];
  keyCIs: string[];
}

export interface Customer360Contact {
  id: number;
  name: string;
  title: string;
  email: string;
  mobile: string;
  phone: string;
  contactRole: string;
}

export interface Customer360ContactSection {
  total: number;
  primary?: Customer360Contact;
  escalation?: Customer360Contact;
  items: Customer360Contact[];
}

export interface Customer360Relation {
  customer: RelationCustomerSummary;
  role: string;
  relationType: string;
  direction: string;
}

export interface Customer360RelationshipSection {
  total: number;
  parents: number;
  children: number;
  partners: number;
  others: number;
  items: Customer360Relation[];
}

export interface Customer360Tenant {
  linkId: number;
  linkedTenantId: number;
  tenantName: string;
  tenantCode: string;
  linkType: string;
  isBillingAnchor: boolean;
  provisionStatus: string;
}

export interface Customer360TenantSection {
  total: number;
  primary: number;
  sandbox: number;
  archive: number;
  provisioning: number;
  items: Customer360Tenant[];
}

export interface Customer360ExternalID {
  id: number;
  systemCode: string;
  externalId: string;
  externalUrl?: string;
}

export interface Customer360 {
  generatedAt: string;
  windowDays: number;
  overview: Customer360Overview;
  serviceHealth: Customer360ServiceHealth;
  tickets: Customer360TicketSection;
  contracts: Customer360ContractSection;
  projects: Customer360ProjectSection;
  assets: Customer360AssetSection;
  contacts: Customer360ContactSection;
  relationships: Customer360RelationshipSection;
  tenants: Customer360TenantSection;
  externalIds: Customer360ExternalID[];
}

export interface RelationCustomerSummary {
  id: number;
  code: string;
  name: string;
  shortName: string;
  tier: string;
  lifecycleStage: string;
  status: string;
}

export interface CustomerRelationship {
  id: number;
  fromCustomerId: number;
  toCustomerId: number;
  relationType: string;
  relationRole: string;
  isBidirectional: boolean;
  effectiveFrom?: string;
  effectiveTo?: string;
  note: string;
  status: string;
  direction: 'outbound' | 'inbound';
  fromCustomer: RelationCustomerSummary;
  toCustomer: RelationCustomerSummary;
  createdAt: string;
  updatedAt: string;
}

export interface RelationshipTreeNode {
  customer: RelationCustomerSummary;
  relationType: string;
  depth: number;
  children: RelationshipTreeNode[];
}

export interface CustomerRelationsTree {
  customer: RelationCustomerSummary;
  parents: RelationshipTreeNode[];
  children: RelationshipTreeNode[];
  directRelations: CustomerRelationship[];
}

export interface ProjectSummary {
  id: number;
  code: string;
  name: string;
  type: string;
  status: string;
  progress: number;
}

export interface ProjectCustomerLink {
  id: number;
  projectId: number;
  customerId: number;
  branchId?: number | null;
  relationType: string;
  isPrimary: boolean;
  note: string;
  project?: ProjectSummary;
  customer?: RelationCustomerSummary;
  branch?: { id: number; code: string; name: string };
  createdAt: string;
  updatedAt: string;
}

export interface RelationshipWritePayload {
  toCustomerId: number;
  relationType: string;
  isBidirectional?: boolean;
  effectiveFrom?: string | null;
  effectiveTo?: string | null;
  note?: string;
}

export interface RelationshipUpdatePayload {
  effectiveFrom?: string | null;
  effectiveTo?: string | null;
  note?: string;
  status?: string;
}

export interface ProjectCustomerLinkWritePayload {
  projectId?: number;
  customerId?: number;
  branchId?: number | null;
  relationType?: string;
  isPrimary?: boolean;
  note?: string;
}

export interface CustomerMergeHistoryItem {
  sourceId: number;
  sourceCode: string;
  sourceName: string;
  targetId: number;
  mergedAt: string;
  undoExpiresAt: string;
  canUndo: boolean;
}

export interface ContractServiceItem {
  id: number;
  contractId: number;
  catalogItemId?: number | null;
  itemCode: string;
  itemName: string;
  spec: string;
  billingUnit: string;
  quantity: number;
  periodCount: number;
  unitPrice: number;
  amount: number;
  quotaTotal?: number | null;
  quotaUsed: number;
  quotaReserved: number;
  availableToAllocate?: number | null;
  note: string;
  slaDefinitionId?: number | null;
  responseTargetMin?: number | null;
  resolveTargetMin?: number | null;
  sortOrder: number;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface ContractServiceItemWritePayload {
  catalogItemId?: number | null;
  itemCode?: string;
  itemName: string;
  spec?: string;
  billingUnit?: string;
  quantity?: number;
  periodCount?: number;
  unitPrice?: number;
  amount?: number | null;
  quotaTotal?: number | null;
  note?: string;
  slaDefinitionId?: number | null;
  responseTargetMin?: number | null;
  resolveTargetMin?: number | null;
  sortOrder?: number;
  status?: string;
}

export interface ContractAmendment {
  id: number;
  contractId: number;
  amendmentNo: string;
  amendType: string;
  effectiveFrom: string;
  amountDelta: number;
  termDeltaDays?: number | null;
  diff: Record<string, unknown>;
  reason: string;
  status: string;
  signedAt?: string;
  createdAt: string;
}

export interface ContractAmendmentWritePayload {
  amendmentNo: string;
  amendType: string;
  effectiveFrom?: string;
  amountDelta?: number;
  termDeltaDays?: number | null;
  diff?: Record<string, unknown>;
  reason?: string;
  signedAt?: string;
}

export interface ContractUsageEntry {
  id: number;
  contractId: number;
  serviceItemId: number;
  sourceType: string;
  sourceRef: string;
  quantityDelta: number;
  amountDelta: number;
  occurredAt: string;
  note: string;
  createdAt: string;
}

export interface ContractUsageWritePayload {
  serviceItemId: number;
  sourceType: string;
  sourceRef: string;
  quantityDelta?: number;
  amountDelta?: number;
  occurredAt?: string;
  note?: string;
}

export interface ContractProjectLink {
  id: number;
  contractId: number;
  contractNumber: string;
  contractName: string;
  projectId: number;
  projectCode: string;
  projectName: string;
  relationType: string;
  serviceItemId?: number | null;
  serviceItemName: string;
  allocatedAmount?: number | null;
  allocatedHours?: number | null;
  allocationPct?: number | null;
  isPrimary: boolean;
  note: string;
  effectiveFrom?: string;
  effectiveTo?: string;
  status: string;
  createdAt: string;
}

export interface ContractProjectLinkWritePayload {
  projectId: number;
  relationType?: string;
  serviceItemId?: number | null;
  allocatedAmount?: number | null;
  allocatedHours?: number | null;
  allocationPct?: number | null;
  isPrimary?: boolean;
  effectiveFrom?: string;
  effectiveTo?: string;
  note?: string;
}

export interface ContractBalance {
  contractId: number;
  amount?: number | null;
  effectiveAmount?: number | null;
  allocatedAmount: number;
  usedAmount: number;
  remainingAmount?: number | null;
  availableAmount?: number | null;
  serviceItems: ContractServiceItem[];
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

  async customers(params?: CustomerListParams): Promise<PageResponse<ServiceCustomer>> {
    return httpClient.get(`${this.baseUrl}/customers`, params);
  }

  async customerDetail(id: number): Promise<CustomerDetailResponse> {
    return httpClient.get(`${this.baseUrl}/customers/${id}`);
  }

  async createCustomer(payload: CustomerWritePayload): Promise<ServiceCustomer> {
    return httpClient.post(`${this.baseUrl}/customers`, payload);
  }

  async updateCustomer(id: number, payload: CustomerWritePayload): Promise<ServiceCustomer> {
    return httpClient.put(`${this.baseUrl}/customers/${id}`, payload);
  }

  async disableCustomer(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/customers/${id}`);
  }

  async branches(customerId?: number): Promise<PageResponse<CustomerBranch>> {
    return httpClient.get(
      `${this.baseUrl}/branches`,
      customerId ? { customerId } : undefined
    );
  }

  async createBranch(payload: BranchWritePayload): Promise<CustomerBranch> {
    return httpClient.post(`${this.baseUrl}/branches`, payload);
  }

  async updateBranch(id: number, payload: BranchWritePayload): Promise<CustomerBranch> {
    return httpClient.put(`${this.baseUrl}/branches/${id}`, payload);
  }

  async disableBranch(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/branches/${id}`);
  }

  async contacts(customerId: number): Promise<PageResponse<CustomerContact>> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/contacts`);
  }

  async createContact(
    customerId: number,
    payload: CustomerContactWritePayload
  ): Promise<CustomerContact> {
    return httpClient.post(`${this.baseUrl}/customers/${customerId}/contacts`, payload);
  }

  async updateContact(
    customerId: number,
    contactId: number,
    payload: CustomerContactWritePayload
  ): Promise<CustomerContact> {
    return httpClient.put(
      `${this.baseUrl}/customers/${customerId}/contacts/${contactId}`,
      payload
    );
  }

  async deleteContact(customerId: number, contactId: number): Promise<void> {
    await httpClient.delete(
      `${this.baseUrl}/customers/${customerId}/contacts/${contactId}`
    );
  }

  async tenantLinks(customerId: number): Promise<PageResponse<CustomerTenantLink>> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/tenant-links`);
  }

  async createTenantLink(
    customerId: number,
    payload: CustomerTenantLinkWritePayload
  ): Promise<CustomerTenantLink> {
    return httpClient.post(`${this.baseUrl}/customers/${customerId}/tenant-links`, payload);
  }

  async updateTenantLink(
    customerId: number,
    linkId: number,
    payload: CustomerTenantLinkWritePayload
  ): Promise<CustomerTenantLink> {
    return httpClient.put(
      `${this.baseUrl}/customers/${customerId}/tenant-links/${linkId}`,
      payload
    );
  }

  async deleteTenantLink(customerId: number, linkId: number): Promise<void> {
    await httpClient.delete(
      `${this.baseUrl}/customers/${customerId}/tenant-links/${linkId}`
    );
  }

  async externalIds(customerId: number): Promise<PageResponse<CustomerExternalID>> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/external-ids`);
  }

  async createCustomerExternalID(
    customerId: number,
    payload: CustomerExternalIDWritePayload
  ): Promise<CustomerExternalID> {
    return httpClient.post(`${this.baseUrl}/customers/${customerId}/external-ids`, payload);
  }

  async updateCustomerExternalID(
    customerId: number,
    externalId: number,
    payload: CustomerExternalIDWritePayload
  ): Promise<CustomerExternalID> {
    return httpClient.put(
      `${this.baseUrl}/customers/${customerId}/external-ids/${externalId}`,
      payload
    );
  }

  async deleteCustomerExternalID(customerId: number, externalId: number): Promise<void> {
    await httpClient.delete(
      `${this.baseUrl}/customers/${customerId}/external-ids/${externalId}`
    );
  }

  async customer360(customerId: number): Promise<Customer360> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/360`);
  }

  async relationships(customerId: number): Promise<PageResponse<CustomerRelationship>> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/relationships`);
  }

  async createRelationship(
    customerId: number,
    payload: RelationshipWritePayload
  ): Promise<{ id: number }> {
    return httpClient.post(`${this.baseUrl}/customers/${customerId}/relationships`, payload);
  }

  async updateRelationship(
    customerId: number,
    relationId: number,
    payload: RelationshipUpdatePayload
  ): Promise<{ id: number }> {
    return httpClient.put(
      `${this.baseUrl}/customers/${customerId}/relationships/${relationId}`,
      payload
    );
  }

  async deleteRelationship(customerId: number, relationId: number): Promise<void> {
    await httpClient.delete(
      `${this.baseUrl}/customers/${customerId}/relationships/${relationId}`
    );
  }

  async relationsTree(customerId: number, depth = 2): Promise<CustomerRelationsTree> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/relations-tree`, { depth });
  }

  async customerProjects(customerId: number): Promise<PageResponse<ProjectCustomerLink>> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/projects`);
  }

  async createCustomerProjectLink(
    customerId: number,
    payload: ProjectCustomerLinkWritePayload
  ): Promise<ProjectCustomerLink> {
    return httpClient.post(`${this.baseUrl}/customers/${customerId}/projects`, payload);
  }

  async deleteCustomerProjectLink(customerId: number, linkId: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/customers/${customerId}/projects/${linkId}`);
  }

  async projectCustomerLinks(projectId: number): Promise<PageResponse<ProjectCustomerLink>> {
    return httpClient.get(`/api/v1/projects/${projectId}/customer-links`);
  }

  async createProjectCustomerLink(
    projectId: number,
    payload: ProjectCustomerLinkWritePayload
  ): Promise<ProjectCustomerLink> {
    return httpClient.post(`/api/v1/projects/${projectId}/customer-links`, payload);
  }

  async deleteProjectCustomerLink(projectId: number, linkId: number): Promise<void> {
    await httpClient.delete(`/api/v1/projects/${projectId}/customer-links/${linkId}`);
  }

  async mergeCustomer(sourceId: number, targetCustomerId: number): Promise<Record<string, unknown>> {
    return httpClient.post(`${this.baseUrl}/customers/${sourceId}/merge`, { targetCustomerId });
  }

  async undoCustomerMerge(sourceId: number): Promise<Record<string, unknown>> {
    return httpClient.post(`${this.baseUrl}/customers/${sourceId}/merge/undo`, {});
  }

  async customerMergeHistory(
    customerId: number
  ): Promise<PageResponse<CustomerMergeHistoryItem>> {
    return httpClient.get(`${this.baseUrl}/customers/${customerId}/merge-history`);
  }

  async contracts(): Promise<PageResponse<SupportContract>> {
    return httpClient.get(`${this.baseUrl}/support-contracts`);
  }

  async contractDetail(id: number): Promise<SupportContract> {
    return httpClient.get(`${this.baseUrl}/support-contracts/${id}`);
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

  async signContract(id: number, signedAt?: string): Promise<SupportContract> {
    return httpClient.post(`${this.baseUrl}/support-contracts/${id}/sign`, { signedAt });
  }

  async terminateContractWithReason(
    id: number,
    reason: string,
    terminatedAt?: string
  ): Promise<SupportContract> {
    return httpClient.post(`${this.baseUrl}/support-contracts/${id}/terminate`, {
      reason,
      terminatedAt,
    });
  }

  async renewContract(
    id: number,
    payload: {
      contractNumber: string;
      effectiveFrom?: string;
      effectiveUntil?: string;
      amount?: number;
      currency?: string;
      carryProjectIds?: number[];
    }
  ): Promise<SupportContract> {
    return httpClient.post(`${this.baseUrl}/support-contracts/${id}/renew`, payload);
  }

  async contractItems(contractId: number): Promise<PageResponse<ContractServiceItem>> {
    return httpClient.get(`${this.baseUrl}/support-contracts/${contractId}/items`);
  }

  async createContractItem(
    contractId: number,
    payload: ContractServiceItemWritePayload
  ): Promise<ContractServiceItem> {
    return httpClient.post(`${this.baseUrl}/support-contracts/${contractId}/items`, payload);
  }

  async updateContractItem(
    contractId: number,
    itemId: number,
    payload: ContractServiceItemWritePayload
  ): Promise<ContractServiceItem> {
    return httpClient.put(
      `${this.baseUrl}/support-contracts/${contractId}/items/${itemId}`,
      payload
    );
  }

  async deleteContractItem(contractId: number, itemId: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/support-contracts/${contractId}/items/${itemId}`);
  }

  async contractAmendments(contractId: number): Promise<PageResponse<ContractAmendment>> {
    return httpClient.get(`${this.baseUrl}/support-contracts/${contractId}/amendments`);
  }

  async createContractAmendment(
    contractId: number,
    payload: ContractAmendmentWritePayload
  ): Promise<ContractAmendment> {
    return httpClient.post(`${this.baseUrl}/support-contracts/${contractId}/amendments`, payload);
  }

  async effectContractAmendment(
    contractId: number,
    amendmentId: number
  ): Promise<ContractAmendment> {
    return httpClient.post(
      `${this.baseUrl}/support-contracts/${contractId}/amendments/${amendmentId}/effect`,
      {}
    );
  }

  async contractUsage(contractId: number): Promise<PageResponse<ContractUsageEntry>> {
    return httpClient.get(`${this.baseUrl}/support-contracts/${contractId}/usage`);
  }

  async recordContractUsage(
    contractId: number,
    payload: ContractUsageWritePayload
  ): Promise<ContractUsageEntry> {
    return httpClient.post(`${this.baseUrl}/support-contracts/${contractId}/usage`, payload);
  }

  async contractBalance(contractId: number): Promise<ContractBalance> {
    return httpClient.get(`${this.baseUrl}/support-contracts/${contractId}/balance`);
  }

  async contractProjects(contractId: number): Promise<PageResponse<ContractProjectLink>> {
    return httpClient.get(`${this.baseUrl}/support-contracts/${contractId}/projects`);
  }

  async createContractProjectLink(
    contractId: number,
    payload: ContractProjectLinkWritePayload
  ): Promise<ContractProjectLink> {
    return httpClient.post(`${this.baseUrl}/support-contracts/${contractId}/projects`, payload);
  }

  async deleteContractProjectLink(contractId: number, linkId: number): Promise<void> {
    await httpClient.delete(
      `${this.baseUrl}/support-contracts/${contractId}/projects/${linkId}`
    );
  }

  async projectContractLinks(projectId: number): Promise<PageResponse<ContractProjectLink>> {
    return httpClient.get(`/api/v1/projects/${projectId}/contract-links`);
  }

  async sourceOrganizations(): Promise<PageResponse<SourceOrganization>> {
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

  async externalContractReferences(): Promise<PageResponse<ExternalContractReference>> {
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
