/**
 * T5-05：助手灰度策略 hook（fail-safe + 免重启读取）。
 */
import { renderHook, waitFor } from '@testing-library/react';

import { getCapabilities } from '@/lib/api/capability-api';
import {
  DEFAULT_ASSISTANT_CAPABILITY_POLICY,
  parseAssistantCapabilityPolicy,
  useAssistantCapabilityPolicy,
} from '../use-assistant-capability-policy';

jest.mock('@/lib/api/capability-api', () => ({
  __esModule: true,
  getCapabilities: jest.fn(),
}));

const mockedCapabilities = getCapabilities as jest.MockedFunction<typeof getCapabilities>;

describe('useAssistantCapabilityPolicy（T5-05）', () => {
  beforeEach(() => {
    mockedCapabilities.mockReset();
  });

  it('字段缺省 → 全部按产品默认开启（fail-safe）', () => {
    expect(parseAssistantCapabilityPolicy(undefined)).toEqual(
      DEFAULT_ASSISTANT_CAPABILITY_POLICY
    );
    expect(
      parseAssistantCapabilityPolicy({ items: [], acceptanceVersion: 'test' })
    ).toEqual(DEFAULT_ASSISTANT_CAPABILITY_POLICY);
  });

  it('显式 false → Dock / 帮助 / 记忆分别关闭', () => {
    expect(
      parseAssistantCapabilityPolicy({
        items: [],
        acceptanceVersion: 'test',
        assistant: { dockEnabled: false, helpEnabled: false, memoryEnabled: false },
      })
    ).toEqual({ dockEnabled: false, helpEnabled: false, memoryEnabled: false });
  });

  it('挂载后读取能力面并应用租户策略；读取失败保持默认开启', async () => {
    mockedCapabilities.mockResolvedValueOnce({
      items: [],
      acceptanceVersion: 'test',
      assistant: { dockEnabled: false, helpEnabled: false, memoryEnabled: true },
    } as never);

    const { result } = renderHook(() => useAssistantCapabilityPolicy(true));
    await waitFor(() => expect(result.current.dockEnabled).toBe(false));
    expect(result.current.helpEnabled).toBe(false);
    expect(result.current.memoryEnabled).toBe(true);

    mockedCapabilities.mockRejectedValueOnce(new Error('capability unavailable'));
    const failed = renderHook(() => useAssistantCapabilityPolicy(true));
    await waitFor(() => expect(mockedCapabilities).toHaveBeenCalledTimes(2));
    expect(failed.result.current).toEqual(DEFAULT_ASSISTANT_CAPABILITY_POLICY);
  });

  it('enabled=false 时不发请求', () => {
    renderHook(() => useAssistantCapabilityPolicy(false));
    expect(mockedCapabilities).not.toHaveBeenCalled();
  });
});
