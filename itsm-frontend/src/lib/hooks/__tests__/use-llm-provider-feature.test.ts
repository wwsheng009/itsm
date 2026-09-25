import { act, renderHook, waitFor } from '@testing-library/react';

/**
 * FE-3 回归：`useLLMProviderFeature` 决定「LLM 模型」页签与会话选择器是否渲染。
 *
 * 契约（§3.6 / D9、QA-3）：
 *  - 非 `system:write` → 不打任何新端点（零请求），enabled=false；
 *  - 灰度开关关闭（整组路由 404）→ fail-closed，enabled=false，不抛异常；
 *  - 探测成功 → enabled=true，providers/preference 就绪；
 *  - 个人默认写失败可被调用方感知（await 抛出）。
 */

const mockState = {
  permissions: [] as string[],
};

const mockListAvailable = jest.fn();
const mockGetUserPreference = jest.fn();
const mockSetUserPreference = jest.fn();

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: Object.assign(
    jest.fn(() => ({
      user: { permissions: mockState.permissions },
    })),
    {
      getState: () => ({
        hasPermission: (permission: string) => mockState.permissions.includes(permission),
      }),
    }
  ),
}));

jest.mock('@/lib/api/llm-provider-api', () => ({
  LLMProviderApi: {
    listAvailable: (...args: unknown[]) => mockListAvailable(...args),
    getUserPreference: (...args: unknown[]) => mockGetUserPreference(...args),
    setUserPreference: (...args: unknown[]) => mockSetUserPreference(...args),
  },
}));

import { useLLMProviderFeature } from '../use-llm-provider-feature';

const providerA = {
  key: 'gateway-a',
  displayName: '兼容网关 A',
  protocol: 'openai_chat_completions',
  variant: '',
  model: 'gpt-4o-mini',
  supportsStream: true,
  supportsTools: true,
  supportsReasoning: false,
  implemented: true,
  isDefault: true,
};

const preferenceB = {
  providerKey: 'ollama-b',
  effectiveProviderKey: 'ollama-b',
  source: 'user',
};

const disabledError = Object.assign(new Error('Not Found'), { httpStatus: 404 });

describe('useLLMProviderFeature', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockState.permissions = [];
    mockListAvailable.mockResolvedValue([providerA]);
    mockGetUserPreference.mockResolvedValue({
      providerKey: '',
      effectiveProviderKey: 'gateway-a',
      source: 'tenant',
    });
    mockSetUserPreference.mockResolvedValue(preferenceB);
  });

  it('非系统管理员：不渲染探测、零请求（QA-3）', async () => {
    mockState.permissions = ['ticket:read'];

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.ready).toBe(true));

    expect(result.current.enabled).toBe(false);
    expect(result.current.providers).toEqual([]);
    expect(mockListAvailable).not.toHaveBeenCalled();
    expect(mockGetUserPreference).not.toHaveBeenCalled();
  });

  it('开关关闭（404 且无 errorCode）：fail-closed，不抛异常', async () => {
    mockState.permissions = ['system:write'];
    mockListAvailable.mockRejectedValue(disabledError);

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.ready).toBe(true));

    expect(result.current.enabled).toBe(false);
    expect(result.current.providers).toEqual([]);
    expect(result.current.preference).toBeNull();
  });

  it('开关开启且探测成功：返回可用实例与个人偏好', async () => {
    mockState.permissions = ['system:write'];
    mockGetUserPreference.mockResolvedValue(preferenceB);

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.enabled).toBe(true));

    expect(result.current.providers).toEqual([providerA]);
    expect(result.current.preference).toEqual(preferenceB);
    expect(mockListAvailable).toHaveBeenCalledTimes(1);
  });

  it('偏好端点失败但实例列表可用：仍视为开启（偏好降级为 null）', async () => {
    mockState.permissions = ['system:write'];
    mockGetUserPreference.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.enabled).toBe(true));

    expect(result.current.preference).toBeNull();
    expect(result.current.providers).toEqual([providerA]);
  });

  it('setPreference 调用 PUT 并刷新列表', async () => {
    mockState.permissions = ['system:write'];
    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.enabled).toBe(true));

    await act(async () => {
      await result.current.setPreference('ollama-b');
    });

    expect(mockSetUserPreference).toHaveBeenCalledWith('ollama-b');
    expect(mockListAvailable).toHaveBeenCalledTimes(2);
  });
});
