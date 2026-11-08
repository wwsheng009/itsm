import { httpClient } from '@/lib/api/http-client';

export type IntakeStatus =
  | 'PROCESSING'
  | 'NEED_INFORMATION'
  | 'WAITING_CUSTOMER'
  | 'MANUAL_REVIEW'
  | 'VERIFIED'
  | 'INCIDENT_CREATED'
  | 'REJECTED';

export interface EmailConversation {
  id: number;
  conversationToken: string;
  status: IntakeStatus;
  customerId?: number;
  customerName?: string;
  branchId?: number;
  branchName?: string;
  supportContractId?: number;
  contractNumber?: string;
  incidentId?: number;
  incidentNumber?: string;
  confidence: number;
  missingFields: string[];
  version: number;
  lastMessageAt: string;
  createdAt: string;
}

export interface InboundEmailMessage {
  id: number;
  externalMessageId: string;
  fromAddress: string;
  toAddresses: string[];
  subject: string;
  plainText: string;
  sanitizedHtml: string;
  processingStatus: string;
  lastError?: string;
  receivedAt: string;
}

export interface IntakeAnalysis {
  id: number;
  provider: string;
  model: string;
  promptVersion: string;
  result: Record<string, unknown>;
  confidence: number;
  status: string;
  validationError?: string;
  corrections: Record<string, unknown>;
  createdAt: string;
}

export interface CustomerMatchCandidate {
  id: number;
  code: string;
  name: string;
  tier: string;
  lifecycleStage: string;
  status: string;
  level: string;
  confidence: number;
  reason: string;
}

export interface EmailConversationDetail extends EmailConversation {
  canonicalData: Record<string, unknown>;
  fieldSources: Record<string, unknown>;
  messages: InboundEmailMessage[];
  analyses: IntakeAnalysis[];
  outboundMessages: Array<{
    id: number;
    replyType: string;
    revision: number;
    toAddress: string;
    subject: string;
    status: string;
    attempts: number;
    lastError?: string;
    sentAt?: string;
  }>;
}

export interface ServiceCustomer {
  id: number;
  code: string;
  name: string;
  normalizedName?: string;
  shortName: string;
  legalName?: string;
  enName?: string;
  aliases: string[];
  historicalNames: string[];
  taxId?: string;
  normalizedTaxId?: string;
  dunsNumber?: string;
  registrationNo?: string;
  emailDomains: string[];
  customerType: string;
  industry?: string;
  subIndustry?: string;
  sizeClass?: string;
  ownershipType?: string;
  tier: string;
  vipLevel: number;
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
  accountManagerName?: string;
  serviceOwnerId?: number | null;
  csManagerId?: number | null;
  oncallGroupId?: number | null;
  billingCurrency?: string;
  paymentTerms?: string;
  creditLimit?: number | null;
  invoiceTitle?: string;
  billingAddress?: Record<string, unknown>;
  lifecycleStage: string;
  onboardedAt?: string;
  contractStartAt?: string;
  contractEndAt?: string;
  churnAt?: string;
  churnReason?: string;
  dataSource?: string;
  dataQualityScore?: number;
  mergedIntoId?: number | null;
  version: number;
  status: string;
  linkedCustomerTenantId?: number | null;
  branchCount?: number;
  contractCount?: number;
  contactCount?: number;
  tenantLinkCount?: number;
  createdAt: string;
  updatedAt: string;
}

export interface CustomerBranch {
  id: number;
  customerId: number;
  code?: string;
  name: string;
  normalizedName?: string;
  branchType?: string;
  aliases: string[];
  address?: Record<string, unknown>;
  timezone?: string;
  contactPhone?: string;
  emailDomains?: string[];
  serviceTier?: string;
  supportHours?: string;
  isPrimary?: boolean;
  linkedTenantId?: number | null;
  status: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface CustomerContact {
  id: number;
  customerId: number;
  branchId?: number | null;
  name: string;
  nameEn?: string;
  title?: string;
  department?: string;
  email?: string;
  normalizedEmail?: string;
  mobile?: string;
  phone?: string;
  wechat?: string;
  contactRole: string;
  isPrimary: boolean;
  language?: string;
  timezone?: string;
  optIn: boolean;
  linkedUserId?: number | null;
  source?: string;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface CustomerTenantLink {
  id: number;
  customerId: number;
  linkedTenantId: number;
  linkType: string;
  isBillingAnchor: boolean;
  provisionStatus: string;
  provisionedAt?: string;
  note?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CustomerExternalID {
  id: number;
  customerId: number;
  systemCode: string;
  externalId: string;
  externalUrl?: string;
  createdAt: string;
}

export interface SupportContract {
  id: number;
  customerId: number;
  branchId?: number;
  contractNumber: string;
  contractName?: string;
  contractType?: string;
  status:
    | 'draft'
    | 'pending_sign'
    | 'pending'
    | 'active'
    | 'expiring'
    | 'expired'
    | 'terminated'
    | 'renewed';
  startAt?: string;
  endAt?: string;
  effectiveFrom?: string;
  effectiveUntil?: string;
  signedAt?: string;
  amount?: number;
  currency?: string;
  parentContractId?: number;
  renewalOfContractId?: number;
  amendmentCount?: number;
  slaDefinitionId?: number;
  autoRenew?: boolean;
  terminatedAt?: string;
  terminationReason?: string;
  version?: number;
  displayStatus?: string;
  remainingDays?: number;
  effectiveAmount?: number;
  allocatedAmount?: number;
  usedAmount?: number;
  remainingAmount?: number;
  availableAmount?: number;
  createdAt?: string;
  updatedAt?: string;
}

export interface SourceOrganization {
  id: number;
  name: string;
  emailAddresses: string[];
  emailDomains: string[];
  status: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ExternalContractReference {
  id: number;
  sourceOrganizationId: number;
  supportContractId: number;
  customerId: number;
  branchId?: number;
  externalContractNumber: string;
  createdAt: string;
}

export interface OnCallSchedule {
  id: number;
  groupId: number;
  name: string;
  timezone: string;
  status: string;
}

export interface OnCallShift {
  id: number;
  scheduleId: number;
  userId: number;
  startAt: string;
  endAt: string;
  createdAt?: string;
}

interface ListResponse<T> {
  items: T[];
  total: number;
  page?: number;
  pageSize?: number;
  totalPages?: number;
}

class EmailIntakeService {
  private readonly baseUrl = '/api/v1/email-intake';

  async conversations(status?: string): Promise<ListResponse<EmailConversation>> {
    return httpClient.get(`${this.baseUrl}/conversations`, status ? { status } : undefined);
  }

  async conversation(id: number): Promise<EmailConversationDetail> {
    return httpClient.get(`${this.baseUrl}/conversations/${id}`);
  }

  async revalidate(id: number, version: number): Promise<EmailConversation> {
    return httpClient.post(`${this.baseUrl}/conversations/${id}/revalidate`, { version });
  }

  async confirm(id: number, version: number): Promise<EmailConversation> {
    return httpClient.post(`${this.baseUrl}/conversations/${id}/confirm`, { version });
  }

  async reject(id: number, version: number): Promise<EmailConversation> {
    return httpClient.post(`${this.baseUrl}/conversations/${id}/reject`, { version });
  }

  async retry(id: number, version: number): Promise<EmailConversation> {
    return httpClient.post(`${this.baseUrl}/conversations/${id}/retry`, { version });
  }

  async correct(
    id: number,
    version: number,
    fields: Record<string, unknown>
  ): Promise<EmailConversation> {
    return httpClient.post(`${this.baseUrl}/conversations/${id}/corrections`, { version, fields });
  }

  async override(id: number, version: number, reason: string): Promise<EmailConversation> {
    return httpClient.post(`${this.baseUrl}/conversations/${id}/override`, {
      version,
      reason,
      confirmed: true,
    });
  }

  async customers(params?: { page?: number; pageSize?: number }): Promise<ListResponse<ServiceCustomer>> {
    return httpClient.get(`${this.baseUrl}/customers`, params);
  }

  async createCustomer(
    payload: Partial<Omit<ServiceCustomer, 'id' | 'createdAt' | 'updatedAt'>> & { name: string }
  ): Promise<ServiceCustomer> {
    return httpClient.post(`${this.baseUrl}/customers`, payload);
  }
  async updateCustomer(
    id: number,
    payload: Partial<Omit<ServiceCustomer, 'id' | 'createdAt' | 'updatedAt'>> & { name: string }
  ): Promise<ServiceCustomer> {
    return httpClient.put(`${this.baseUrl}/customers/${id}`, payload);
  }
  async disableCustomer(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/customers/${id}`);
  }

  async branches(customerId?: number): Promise<ListResponse<CustomerBranch>> {
    return httpClient.get(`${this.baseUrl}/branches`, customerId ? { customerId } : undefined);
  }

  async createBranch(
    payload: Partial<Omit<CustomerBranch, 'id'>> & { customerId: number; name: string }
  ): Promise<CustomerBranch> {
    return httpClient.post(`${this.baseUrl}/branches`, payload);
  }

  async contracts(): Promise<ListResponse<SupportContract>> {
    return httpClient.get(`${this.baseUrl}/support-contracts`);
  }

  async createContract(payload: Omit<SupportContract, 'id'>): Promise<SupportContract> {
    return httpClient.post(`${this.baseUrl}/support-contracts`, payload);
  }
  async updateContract(id: number, payload: Omit<SupportContract, 'id'>): Promise<SupportContract> {
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

  async schedules(): Promise<ListResponse<OnCallSchedule>> {
    return httpClient.get(`${this.baseUrl}/on-call/schedules`);
  }

  async createSchedule(payload: Omit<OnCallSchedule, 'id'>): Promise<OnCallSchedule> {
    return httpClient.post(`${this.baseUrl}/on-call/schedules`, payload);
  }

  async createShift(payload: {
    scheduleId: number;
    userId: number;
    startAt: string;
    endAt: string;
  }): Promise<void> {
    await httpClient.post(`${this.baseUrl}/on-call/shifts`, payload);
  }

  async shifts(scheduleId?: number): Promise<ListResponse<OnCallShift>> {
    return httpClient.get(
      `${this.baseUrl}/on-call/shifts`,
      scheduleId ? { scheduleId } : undefined
    );
  }

  async updateShift(
    id: number,
    payload: { scheduleId: number; userId: number; startAt: string; endAt: string }
  ): Promise<OnCallShift> {
    return httpClient.put(`${this.baseUrl}/on-call/shifts/${id}`, payload);
  }

  async deleteShift(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/on-call/shifts/${id}`);
  }
  async updateBranch(
    id: number,
    payload: Partial<Omit<CustomerBranch, 'id'>> & { customerId: number; name: string }
  ): Promise<CustomerBranch> {
    return httpClient.put(`${this.baseUrl}/branches/${id}`, payload);
  }
  async disableBranch(id: number): Promise<void> {
    await httpClient.delete(`${this.baseUrl}/branches/${id}`);
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

  async currentOnCall(
    groupId: number
  ): Promise<{
    scheduleId: number;
    shiftId: number;
    groupId: number;
    userId: number;
    startAt: string;
    endAt: string;
  } | null> {
    return httpClient.get(`${this.baseUrl}/on-call/current`, { groupId });
  }
}

export const emailIntakeService = new EmailIntakeService();
