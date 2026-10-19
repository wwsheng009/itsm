/**
 * chunk-recovery 单测：
 * - chunk 加载失败错误识别（Chromium/Firefox/Safari/MIME 文案）
 * - 冷却防抖的整页刷新与 force 强制刷新
 * - vite:preloadError / unhandledrejection 全局兜底
 */

import {
  installChunkLoadErrorHandler,
  isChunkLoadError,
  recoverFromChunkLoadError,
} from '../chunk-recovery';

const RELOAD_GUARD_KEY = 'itsm:chunk-reload-at';

const reload = jest.fn();

beforeAll(() => {
  installChunkLoadErrorHandler();
});

beforeEach(() => {
  sessionStorage.clear();
  Object.defineProperty(window, 'location', {
    value: { href: 'http://localhost/', reload },
    writable: true,
    configurable: true,
  });
});

describe('isChunkLoadError', () => {
  it.each([
    'Failed to fetch dynamically imported module: http://localhost:8089/static/index-6WXFeVEG.js',
    'error loading dynamically imported module: http://localhost/static/index-1.js',
    'Importing a module script failed.',
    "Failed to load module script: Expected a JavaScript module script but the server responded with a MIME type of 'text/html'",
    'Unable to preload CSS for /static/index-abc.css',
  ])('识别 chunk 加载失败：%s', message => {
    expect(isChunkLoadError(new Error(message))).toBe(true);
  });

  it('普通错误与空值不误判', () => {
    expect(isChunkLoadError(new Error('Request failed with status code 500'))).toBe(false);
    expect(isChunkLoadError(undefined)).toBe(false);
    expect(isChunkLoadError(null)).toBe(false);
    expect(isChunkLoadError('')).toBe(false);
  });
});

describe('recoverFromChunkLoadError', () => {
  it('首次失败触发整页刷新并写入冷却标记', () => {
    expect(recoverFromChunkLoadError()).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(Number(sessionStorage.getItem(RELOAD_GUARD_KEY))).toBeGreaterThan(0);
  });

  it('冷却期内不重复刷新', () => {
    expect(recoverFromChunkLoadError()).toBe(true);
    expect(recoverFromChunkLoadError()).toBe(false);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('force 跳过冷却（用户显式点击刷新）', () => {
    recoverFromChunkLoadError();
    expect(recoverFromChunkLoadError({ force: true })).toBe(true);
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it('sessionStorage 不可用时仍刷新一次', () => {
    jest.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('sessionStorage denied');
    });
    expect(recoverFromChunkLoadError()).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
  });
});

describe('installChunkLoadErrorHandler', () => {
  it('vite:preloadError 触发刷新并阻止默认抛错', () => {
    const event = new Event('vite:preloadError', { cancelable: true });
    window.dispatchEvent(event);

    expect(reload).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(true);
  });

  it('冷却期内的 vite:preloadError 不刷新、不阻止默认行为', () => {
    recoverFromChunkLoadError();
    const event = new Event('vite:preloadError', { cancelable: true });
    window.dispatchEvent(event);

    expect(reload).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(false);
  });

  it('兜底 unhandledrejection 中的 chunk 加载错误', () => {
    const event = new Event('unhandledrejection') as Event & { reason?: unknown };
    event.reason = new Error(
      'Failed to fetch dynamically imported module: http://localhost/static/index-x.js'
    );
    window.dispatchEvent(event);

    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('无关 rejection 不刷新', () => {
    const event = new Event('unhandledrejection') as Event & { reason?: unknown };
    event.reason = new Error('some unrelated failure');
    window.dispatchEvent(event);

    expect(reload).not.toHaveBeenCalled();
  });
});
