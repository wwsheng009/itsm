/**
 * useI18n Hook 测试
 */

import { renderHook, act } from '@testing-library/react';
import { useI18n } from '../useI18n';

// Mock user-preferences
const mockGet = jest.fn(() => ({ language: 'zh-CN' }));
const mockUpdate = jest.fn();
const mockSubscribe = jest.fn((_listener: (preferences: Record<string, unknown>) => void) =>
  jest.fn()
);

jest.mock('@/lib/user-preferences', () => ({
  userPreferences: {
    get: () => mockGet(),
    update: (updates: Record<string, unknown>) => mockUpdate(updates),
    subscribe: (listener: (preferences: Record<string, unknown>) => void) =>
      mockSubscribe(listener),
  },
}));

describe('useI18n', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockGet.mockReturnValue({ language: 'zh-CN' });
    mockSubscribe.mockReturnValue(jest.fn());
  });

  describe('initial state', () => {
    it('should initialize with zh-CN language by default', () => {
      const { result } = renderHook(() => useI18n());
      expect(result.current.language).toBe('zh-CN');
    });

    it('should initialize with en-US when preference is set', () => {
      mockGet.mockReturnValueOnce({ language: 'en-US' });

      const { result } = renderHook(() => useI18n());
      expect(result.current.language).toBe('en-US');
    });
  });

  describe('t() function', () => {
    it('should return translation for valid key', () => {
      const { result } = renderHook(() => useI18n());
      const translation = result.current.t('common.confirm');
      expect(typeof translation).toBe('string');
    });

    it('should return key when translation not found', () => {
      const { result } = renderHook(() => useI18n());
      const translation = result.current.t('nonexistent.key');
      expect(translation).toBe('nonexistent.key');
    });

    it('should return key when placeholder params provided for unknown key', () => {
      const { result } = renderHook(() => useI18n());
      // When translation key doesn't exist, returns the key itself
      const translation = result.current.t('nonexistent.key', { count: 5 });
      expect(translation).toBe('nonexistent.key');
    });

    it('should handle nested keys', () => {
      const { result } = renderHook(() => useI18n());
      const translation = result.current.t('menu.dashboard');
      expect(typeof translation).toBe('string');
    });
  });

  describe('t() placeholder substitution', () => {
    // P0 回归：部分翻译串使用 i18next/Vue 风格双花括号 {{name}}，历史上只替换单花括号
    // {name}，导致 Bot 运行看板 `最近 {{days}} 天` 渲染成 `最近 {7} 天`（外层花括号残留），
    // 分母 `{{completed}}=0` 渲染成 `{0}`。t() 现同时支持 {name} 与 {{name}}。
    it('replaces {{name}} (double-brace) without leaving stray braces', () => {
      const { result } = renderHook(() => useI18n());
      expect(result.current.t('aiBotMetrics.lastDays', { days: 7 })).toBe('最近 7 天');
      expect(result.current.t('aiBotMetrics.toolCounts', { total: 3, errors: 1 })).toBe(
        '调用 3 / 错误 1',
      );
      expect(
        result.current.t('aiBotMetrics.runCounts', { completed: 0, failed: 0, running: 0 }),
      ).toBe('完成 0 / 失败 0 / 进行中 0');
    });

    it('replaces single-brace {name} still works', () => {
      const { result } = renderHook(() => useI18n());
      // workflow.workflowOverviewDescription: '该工作流包含 {total} 个节点，其中 {approval} 个审批节点'
      expect(
        result.current.t('workflow.workflowOverviewDescription', { total: 5, approval: 2 }),
      ).toBe('该工作流包含 5 个节点，其中 2 个审批节点');
    });

    it('keeps a no-param key intact', () => {
      const { result } = renderHook(() => useI18n());
      expect(result.current.t('aiBotMetrics.title')).toBe('Bot 运行指标');
    });
  });

  describe('changeLanguage()', () => {
    it('should update language preference', () => {
      const { result } = renderHook(() => useI18n());

      act(() => {
        result.current.changeLanguage('en-US');
      });

      expect(mockUpdate).toHaveBeenCalledWith({ language: 'en-US' });
    });

    it('should switch to zh-CN', () => {
      mockGet.mockReturnValueOnce({ language: 'en-US' });

      const { result } = renderHook(() => useI18n());

      act(() => {
        result.current.changeLanguage('zh-CN');
      });

      expect(mockUpdate).toHaveBeenCalledWith({ language: 'zh-CN' });
    });
  });
});
