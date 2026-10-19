/**
 * 懒加载 chunk 失效恢复（stale chunk / 部署后旧资源被清理）。
 *
 * 现象：控制台报
 *   "Failed to fetch dynamically imported module: http://host:8089/static/index-XXXX.js"
 * 页面被 ErrorBoundary 兜底成「页面出现错误」。
 *
 * 原因：产物使用内容哈希文件名，/static/ 又被设置为
 * `Cache-Control: public, max-age=31536000, immutable`（见 itsm-frontend/nginx/default.conf）。
 * 发布新版本后旧哈希文件从服务器删除，仍停留在旧 index.html 的标签页在跳转
 * 未访问过的懒加载路由时请求旧 chunk → 404（Vite 官方文档 “Load Error Handling”）。
 *
 * 处理：检测到该类错误时整页 reload，换取最新 index.html 与 chunk；
 * 用 sessionStorage 时间戳做冷却防抖，避免服务端异常/离线时无限刷新。
 */

const RELOAD_GUARD_KEY = 'itsm:chunk-reload-at';

/** 两次自动刷新之间的最小间隔；冷却期内的再次失败交给 ErrorBoundary 展示。 */
const RELOAD_COOLDOWN_MS = 30_000;

const CHUNK_LOAD_ERROR_PATTERNS = [
  'Failed to fetch dynamically imported module', // Chromium
  'error loading dynamically imported module', // Firefox
  'Importing a module script failed', // Safari
  'Failed to load module script', // MIME / 网络错误
  'Unable to preload CSS', // 依赖 chunk 的样式预加载失败
];

/** 判断错误是否属于「动态 import 的 chunk 加载失败」。 */
export function isChunkLoadError(error: unknown): boolean {
  const message =
    error instanceof Error ? error.message : typeof error === 'string' ? error : '';
  if (!message) {
    return false;
  }
  return CHUNK_LOAD_ERROR_PATTERNS.some(pattern => message.includes(pattern));
}

/**
 * 整页刷新以获取新版本资源。
 *
 * @param options.force 用户显式点击「刷新/重试」时传 true，跳过冷却期检查。
 * @returns 是否已触发刷新；false 表示处于冷却期被拦截。
 */
export function recoverFromChunkLoadError({ force = false }: { force?: boolean } = {}): boolean {
  if (typeof window === 'undefined') {
    return false;
  }

  try {
    const last = Number(window.sessionStorage.getItem(RELOAD_GUARD_KEY)) || 0;
    if (!force && Date.now() - last < RELOAD_COOLDOWN_MS) {
      return false;
    }
    window.sessionStorage.setItem(RELOAD_GUARD_KEY, String(Date.now()));
  } catch {
    // sessionStorage 不可用（隐私模式等）：无持久化防抖，仍执行本次刷新。
  }

  window.location.reload();
  return true;
}

/**
 * 安装全局兜底，应在应用入口最早调用一次：
 * - `vite:preloadError`：Vite 在动态导入（含依赖预加载）失败时派发；
 * - `unhandledrejection`：兜底未被 Vite 预加载包装的裸 `import()` 失败。
 */
export function installChunkLoadErrorHandler(): void {
  if (typeof window === 'undefined') {
    return;
  }

  window.addEventListener('vite:preloadError', event => {
    if (recoverFromChunkLoadError()) {
      // 已决定整页刷新，阻止 Vite 再抛出原始错误造成噪音。
      event.preventDefault();
    }
  });

  window.addEventListener('unhandledrejection', event => {
    if (isChunkLoadError(event.reason)) {
      recoverFromChunkLoadError();
    }
  });
}
