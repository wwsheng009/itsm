jest.mock('@/lib/api/http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    delete: jest.fn(),
  },
}));

import { httpClient } from '@/lib/api/http-client';
import masterDataService from '../masterDataService';

const mockGet = httpClient.get as jest.Mock;
const mockPost = httpClient.post as jest.Mock;
const mockPut = httpClient.put as jest.Mock;
const mockDelete = httpClient.delete as jest.Mock;

describe('masterDataService P0 contract', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('客户列表使用服务端分页筛选参数', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20, totalPages: 0 });

    await masterDataService.customers({
      keyword: '华辰',
      tier: 'A',
      lifecycleStage: 'active',
      hasTenantLink: true,
      page: 1,
      pageSize: 20,
    });

    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers', {
      keyword: '华辰',
      tier: 'A',
      lifecycleStage: 'active',
      hasTenantLink: true,
      page: 1,
      pageSize: 20,
    });
  });

  it('客户详情、联系人、租户映射路径稳定', async () => {
    mockGet.mockResolvedValue({ customer: { id: 1 }, branches: [], contacts: [], tenantLinks: [] });
    mockPost.mockResolvedValue({ id: 2 });
    mockPut.mockResolvedValue({ id: 3 });
    mockDelete.mockResolvedValue(undefined);

    await masterDataService.customerDetail(1);
    await masterDataService.createContact(1, { name: '张三', email: 'a@example.com' });
    await masterDataService.updateTenantLink(1, 9, { linkedTenantId: 3, linkType: 'primary' });
    await masterDataService.deleteTenantLink(1, 9);
    await masterDataService.deleteContact(1, 2);

    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers/1');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/customers/1/contacts', {
      name: '张三',
      email: 'a@example.com',
    });
    expect(mockPut).toHaveBeenCalledWith('/api/v1/master-data/customers/1/tenant-links/9', {
      linkedTenantId: 3,
      linkType: 'primary',
    });
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/master-data/customers/1/tenant-links/9');
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/master-data/customers/1/contacts/2');
  });

  it('客户和服务点写接口保持 camelCase payload', async () => {
    mockPost.mockResolvedValue({ id: 1 });
    mockPut.mockResolvedValue({ id: 1 });
    mockDelete.mockResolvedValue(undefined);

    await masterDataService.createCustomer({
      name: 'HTTP 客户',
      legalName: 'HTTP 客户股份有限公司',
      taxId: '9131',
      emailDomains: ['http.example'],
      tier: 'A',
    });
    await masterDataService.updateCustomer(1, {
      name: 'HTTP 客户（更新）',
      tier: 'S',
      version: 1,
    });
    await masterDataService.updateBranch(9, {
      customerId: 1,
      name: '上海总部',
      branchType: 'hq',
      isPrimary: true,
    });

    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/customers', {
      name: 'HTTP 客户',
      legalName: 'HTTP 客户股份有限公司',
      taxId: '9131',
      emailDomains: ['http.example'],
      tier: 'A',
    });
    expect(mockPut).toHaveBeenCalledWith('/api/v1/master-data/customers/1', {
      name: 'HTTP 客户（更新）',
      tier: 'S',
      version: 1,
    });
    expect(mockPut).toHaveBeenCalledWith('/api/v1/master-data/branches/9', {
      customerId: 1,
      name: '上海总部',
      branchType: 'hq',
      isPrimary: true,
    });
  });
});

describe('masterDataService P1 contract', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('客户关系、关系树和项目关联路径稳定', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0 });
    mockPost.mockResolvedValue({ id: 1 });
    mockPut.mockResolvedValue({ id: 2 });
    mockDelete.mockResolvedValue(undefined);

    await masterDataService.relationships(1);
    await masterDataService.createRelationship(1, {
      toCustomerId: 2,
      relationType: 'parent',
      isBidirectional: true,
    });
    await masterDataService.updateRelationship(1, 9, { status: 'ended', note: '到期' });
    await masterDataService.deleteRelationship(1, 9);
    await masterDataService.relationsTree(1, 5);
    await masterDataService.customerProjects(1);
    await masterDataService.createCustomerProjectLink(1, {
      projectId: 3,
      relationType: 'delivery',
      isPrimary: true,
    });
    await masterDataService.deleteCustomerProjectLink(1, 4);
    await masterDataService.projectCustomerLinks(3);
    await masterDataService.createProjectCustomerLink(3, { customerId: 1, relationType: 'support' });
    await masterDataService.deleteProjectCustomerLink(3, 4);

    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers/1/relationships');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/customers/1/relationships', {
      toCustomerId: 2,
      relationType: 'parent',
      isBidirectional: true,
    });
    expect(mockPut).toHaveBeenCalledWith(
      '/api/v1/master-data/customers/1/relationships/9',
      { status: 'ended', note: '到期' }
    );
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/master-data/customers/1/relationships/9');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers/1/relations-tree', {
      depth: 5,
    });
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers/1/projects');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/customers/1/projects', {
      projectId: 3,
      relationType: 'delivery',
      isPrimary: true,
    });
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/master-data/customers/1/projects/4');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/customer-links');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/projects/3/customer-links', {
      customerId: 1,
      relationType: 'support',
    });
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/projects/3/customer-links/4');
  });

  it('合并、合并历史和撤销合并路径稳定', async () => {
    mockPost.mockResolvedValue({ sourceId: 1, targetId: 2 });
    mockGet.mockResolvedValue({ items: [], total: 0 });

    await masterDataService.mergeCustomer(1, 2);
    await masterDataService.customerMergeHistory(2);
    await masterDataService.undoCustomerMerge(1);

    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/customers/1/merge', {
      targetCustomerId: 2,
    });
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers/2/merge-history');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/customers/1/merge/undo', {});
  });
});

describe('masterDataService P2 contract', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('外部标识与客户 360 路径稳定', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0 });
    mockPost.mockResolvedValue({ id: 1 });
    mockPut.mockResolvedValue({ id: 2 });
    mockDelete.mockResolvedValue(undefined);

    await masterDataService.externalIds(1);
    await masterDataService.createCustomerExternalID(1, {
      systemCode: 'erp',
      externalId: 'EXT-1',
      externalUrl: 'https://erp.example/1',
    });
    await masterDataService.updateCustomerExternalID(1, 9, {
      systemCode: 'crm',
      externalId: 'CRM-1',
    });
    await masterDataService.deleteCustomerExternalID(1, 9);
    await masterDataService.customer360(1);

    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers/1/external-ids');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/customers/1/external-ids', {
      systemCode: 'erp',
      externalId: 'EXT-1',
      externalUrl: 'https://erp.example/1',
    });
    expect(mockPut).toHaveBeenCalledWith(
      '/api/v1/master-data/customers/1/external-ids/9',
      { systemCode: 'crm', externalId: 'CRM-1' }
    );
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/master-data/customers/1/external-ids/9');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/customers/1/360');
  });
});

describe('masterDataService C1 contract aggregate', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('合同聚合根、服务项、变更单、用量与项目链接路径稳定', async () => {
    mockGet.mockResolvedValue({ items: [], total: 0 });
    mockPost.mockResolvedValue({ id: 1 });
    mockPut.mockResolvedValue({ id: 1 });
    mockDelete.mockResolvedValue(undefined);

    await masterDataService.contractDetail(1);
    await masterDataService.contractItems(1);
    await masterDataService.createContractItem(1, {
      itemName: '运维工时',
      billingUnit: 'hour',
      unitPrice: 1000,
      quotaTotal: 100,
    });
    await masterDataService.updateContractItem(1, 9, { itemName: '运维工时（更新）' });
    await masterDataService.deleteContractItem(1, 9);
    await masterDataService.contractAmendments(1);
    await masterDataService.createContractAmendment(1, {
      amendmentNo: 'A1',
      amendType: 'amount',
      amountDelta: 100,
    });
    await masterDataService.effectContractAmendment(1, 8);
    await masterDataService.contractUsage(1);
    await masterDataService.recordContractUsage(1, {
      serviceItemId: 9,
      sourceType: 'timesheet',
      sourceRef: 'TS-1',
      quantityDelta: 2,
    });
    await masterDataService.contractBalance(1);
    await masterDataService.contractProjects(1);
    await masterDataService.createContractProjectLink(1, {
      projectId: 3,
      relationType: 'billing',
      serviceItemId: 9,
      allocatedHours: 10,
    });
    await masterDataService.deleteContractProjectLink(1, 7);
    await masterDataService.projectContractLinks(3);
    await masterDataService.signContract(1, '2026-11-08T10:00:00Z');
    await masterDataService.terminateContractWithReason(1, '客户退订');
    await masterDataService.renewContract(1, {
      contractNumber: 'HT-2027-001',
      carryProjectIds: [3],
    });

    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1/items');
    expect(mockPost).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1/items', {
      itemName: '运维工时',
      billingUnit: 'hour',
      unitPrice: 1000,
      quotaTotal: 100,
    });
    expect(mockPut).toHaveBeenCalledWith(
      '/api/v1/master-data/support-contracts/1/items/9',
      { itemName: '运维工时（更新）' }
    );
    expect(mockDelete).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1/items/9');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1/amendments');
    expect(mockPost).toHaveBeenCalledWith(
      '/api/v1/master-data/support-contracts/1/amendments/8/effect',
      {}
    );
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1/usage');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1/balance');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/master-data/support-contracts/1/projects');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/projects/3/contract-links');
    expect(mockPost).toHaveBeenCalledWith(
      '/api/v1/master-data/support-contracts/1/sign',
      { signedAt: '2026-11-08T10:00:00Z' }
    );
    expect(mockPost).toHaveBeenCalledWith(
      '/api/v1/master-data/support-contracts/1/terminate',
      { reason: '客户退订', terminatedAt: undefined }
    );
  });
});
