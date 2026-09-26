import { act, renderHook, waitFor } from '@testing-library/react';

/**
 * FE-3 回归：`useLLMProviderFeature` 决定「LLM 模型」页签与会话选择器是否渲染。
 *
 * 契约（§3.6 / D9 + P1 权限放开）：
 *  - P1 起读端点（available / user-preference）降为 ai:read → **所有已登录用户**都发起探测，
 *    不再有前端权限门控；探测成功 → enabled=true，providers/preference 就绪；
 *  - 探测被拒（403，无 ai:read）→ fail-closed，enabled=false，不抛异常；
 *  - 灰度开关关闭（整组路由 404）→ fail-closed，enabled=false，不抛异常；
 *  - 个人默认写失败可被调用方感知（await 抛出）。
 */

const mockListAvailable = jest.fn();
const mockGetUserPreference = jest.fn();
const mockSetUserPreference = jest.fn();

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
const forbiddenError = Object.assign(new Error('Forbidden'), { httpStatus: 403 });

describe('useLLMProviderFeature', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockListAvailable.mockResolvedValue([providerA]);
    mockGetUserPreference.mockResolvedValue({
      providerKey: '',
      effectiveProviderKey: 'gateway-a',
      source: 'tenant',
    });
    mockSetUserPreference.mockResolvedValue(preferenceB);
  });

  it('普通用户同样发起探测（P1）：探测成功即可用（无权限门控）', async () => {
    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.ready).toBe(true));

    expect(mockListAvailable).toHaveBeenCalledTimes(1);
    expect(result.current.enabled).toBe(true);
    expect(result.current.providers).toEqual([providerA]);
    expect(result.current.preference).toEqual({
      providerKey: '',
      effectiveProviderKey: 'gateway-a',
      source: 'tenant',
    });
  });

  it('探测被拒（403，无 ai:read）：fail-closed，不抛异常', async () => {
    mockListAvailable.mockRejectedValue(forbiddenError);

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.ready).toBe(true));

    expect(result.current.enabled).toBe(false);
    expect(result.current.providers).toEqual([]);
    expect(result.current.preference).toBeNull();
  });

  it('开关关闭（404 且无 errorCode）：fail-closed，不抛异常', async () => {
    mockListAvailable.mockRejectedValue(disabledError);

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.ready).toBe(true));

    expect(result.current.enabled).toBe(false);
    expect(result.current.providers).toEqual([]);
    expect(result.current.preference).toBeNull();
  });

  it('开关开启且探测成功：返回可用实例与个人偏好', async () => {
    mockGetUserPreference.mockResolvedValue(preferenceB);

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.enabled).toBe(true));

    expect(result.current.providers).toEqual([providerA]);
    expect(result.current.preference).toEqual(preferenceB);
    expect(mockListAvailable).toHaveBeenCalledTimes(1);
  });

  it('偏好端点失败但实例列表可用：仍视为开启（偏好降级为 null）', async () => {
    mockGetUserPreference.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.enabled).toBe(true));

    expect(result.current.preference).toBeNull();
    expect(result.current.providers).toEqual([providerA]);
  });

  it('setPreference 调用 PUT 并刷新列表', async () => {
    const { result } = renderHook(() => useLLMProviderFeature());
    await waitFor(() => expect(result.current.enabled).toBe(true));

    await act(async () => {
      await result.current.setPreference('ollama-b');
    });

    expect(mockSetUserPreference).toHaveBeenCalledWith('ollama-b');
    expect(mockListAvailable).toHaveBeenCalledTimes(2);
  });
});
