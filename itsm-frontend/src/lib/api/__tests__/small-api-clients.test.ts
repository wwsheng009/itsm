/**
 * 低覆盖小模块补测（cab-api / ticketTypeApi / password-policy-api）。
 *
 * 背景：全量 `npm test`（含覆盖率门槛）复核时 global statements = 79.96%，
 * 距 80% 门槛差 0.04pp；本文件补齐三个 0% 小模块，并顺带锁住其 URL/载荷契约。
 */
import { httpClient } from '@/lib/api/http-client';
import CabApi from '@/lib/api/cab-api';
import { TicketTypeApi } from '@/lib/api/ticketTypeApi';
import {
  DEFAULT_PASSWORD_POLICY,
  clearPasswordPolicyCache,
  describePasswordPolicy,
  getPasswordPolicy,
} from '@/lib/api/password-policy-api';

jest.mock('@/lib/api/http-client', () => ({
  ...jest.requireActual('@/lib/api/http-client'),
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    delete: jest.fn(),
  },
}));

const mockedHttp = httpClient as unknown as {
  get: jest.Mock;
  post: jest.Mock;
  put: jest.Mock;
  delete: jest.Mock;
};

beforeEach(() => {
  jest.clearAllMocks();
  clearPasswordPolicyCache();
});

describe('CabApi', () => {
  it('成员名册四处端点的 URL 与谓词', async () => {
    mockedHttp.get.mockResolvedValueOnce([]);
    await CabApi.getMembers();
    expect(mockedHttp.get).toHaveBeenCalledWith('/api/v1/cab/members', { type: 'CAB' });

    mockedHttp.post.mockResolvedValueOnce({});
    await CabApi.addMember({ userId: 1 } as never);
    expect(mockedHttp.post).toHaveBeenCalledWith('/api/v1/cab/members', { userId: 1 });

    mockedHttp.put.mockResolvedValueOnce({});
    await CabApi.updateMember(7, { role: 'member' } as never);
    expect(mockedHttp.put).toHaveBeenCalledWith('/api/v1/cab/members/7', { role: 'member' });

    mockedHttp.delete.mockResolvedValueOnce({ deleted: 1 });
    await CabApi.removeMember(7);
    expect(mockedHttp.delete).toHaveBeenCalledWith('/api/v1/cab/members/7');
  });
});

describe('TicketTypeApi', () => {
  it('类型管理端点映射（含启停/恢复/克隆/预置）', async () => {
    mockedHttp.get.mockResolvedValue({});
    await TicketTypeApi.list({ status: 'active' });
    expect(mockedHttp.get).toHaveBeenCalledWith('/api/v1/ticket-types', { status: 'active' });
    await TicketTypeApi.get(3);
    expect(mockedHttp.get).toHaveBeenCalledWith('/api/v1/ticket-types/3');

    mockedHttp.post.mockResolvedValue({});
    await TicketTypeApi.create({ code: 'incident' } as never);
    expect(mockedHttp.post).toHaveBeenCalledWith('/api/v1/ticket-types', { code: 'incident' });

    mockedHttp.put.mockResolvedValue({});
    await TicketTypeApi.update(3, { name: '事件' } as never);
    expect(mockedHttp.put).toHaveBeenCalledWith('/api/v1/ticket-types/3', { name: '事件' });

    await TicketTypeApi.setEnabled(3, true);
    expect(mockedHttp.post).toHaveBeenCalledWith('/api/v1/ticket-types/3/enable', {});
    await TicketTypeApi.setEnabled(3, false);
    expect(mockedHttp.post).toHaveBeenCalledWith('/api/v1/ticket-types/3/disable', {});

    await TicketTypeApi.restore(3);
    expect(mockedHttp.post).toHaveBeenCalledWith('/api/v1/ticket-types/3/restore', {});
    await TicketTypeApi.clone(3, 'incident-copy', '事件副本');
    expect(mockedHttp.post).toHaveBeenCalledWith('/api/v1/ticket-types/3/clone', {
      code: 'incident-copy',
      name: '事件副本',
    });

    await TicketTypeApi.listPresets();
    expect(mockedHttp.get).toHaveBeenCalledWith('/api/v1/ticket-type-presets');
    await TicketTypeApi.installPreset('preset-1');
    expect(mockedHttp.post).toHaveBeenCalledWith('/api/v1/ticket-type-presets/preset-1/install', {});
  });
});

describe('password-policy-api', () => {
  it('成功路径：合并默认值 + 进程内缓存只发一次请求', async () => {
    mockedHttp.get.mockResolvedValue({ minLength: 10 });
    const first = await getPasswordPolicy();
    expect(first.minLength).toBe(10);
    expect(first.maxLength).toBe(DEFAULT_PASSWORD_POLICY.maxLength);

    const second = await getPasswordPolicy();
    expect(second).toBe(first);
    expect(mockedHttp.get).toHaveBeenCalledTimes(1);

    clearPasswordPolicyCache();
    mockedHttp.get.mockResolvedValueOnce({ minLength: 8 });
    await getPasswordPolicy();
    expect(mockedHttp.get).toHaveBeenCalledTimes(2);
  });

  it('失败回退默认策略且不固化缓存（可重试）', async () => {
    mockedHttp.get.mockRejectedValueOnce(new Error('network'));
    const fallback = await getPasswordPolicy();
    expect(fallback).toEqual(DEFAULT_PASSWORD_POLICY);

    mockedHttp.get.mockResolvedValueOnce({ minLength: 12 });
    const retried = await getPasswordPolicy();
    expect(retried.minLength).toBe(12);
    expect(mockedHttp.get).toHaveBeenCalledTimes(2);
  });

  it('describePasswordPolicy 文案分支（区间/下限/空规则）', () => {
    expect(describePasswordPolicy({ ...DEFAULT_PASSWORD_POLICY })).toBe(
      '长度 8-128 位，需包含大写字母、小写字母、数字'
    );
    expect(
      describePasswordPolicy({
        minLength: 6,
        maxLength: 0,
        requireUppercase: false,
        requireLowercase: false,
        requireNumbers: false,
        requireSpecialChars: false,
      })
    ).toBe('长度不少于 6 位');
    expect(
      describePasswordPolicy({
        minLength: 0,
        maxLength: 0,
        requireUppercase: false,
        requireLowercase: false,
        requireNumbers: false,
        requireSpecialChars: false,
      })
    ).toBe('');
  });
});
