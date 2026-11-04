/**
 * 侧栏默认折叠状态的外部控制参数。
 *
 * 用途：桌面客户端/嵌入 WebView 打开服务端页面时，可通过
 * `?sidebar=collapsed` 避免默认展开左侧菜单；普通浏览器不传参数时保持原行为。
 *
 * 参数只决定「初始默认值」，用户随后仍可用 Header 的折叠按钮展开/收起。
 */

const COLLAPSED_VALUES = new Set([
  'collapsed',
  'collapse',
  'closed',
  'hidden',
  '1',
  'true',
  'on',
  'yes',
]);

const EXPANDED_VALUES = new Set([
  'expanded',
  'expand',
  'open',
  'visible',
  '0',
  'false',
  'off',
  'no',
]);

/**
 * 解析 `sidebar` 查询参数。
 *
 * @returns true=默认收起，false=默认展开，null=未指定/非法值（沿用当前 Store 行为）
 */
export function readSidebarPreference(search: string): boolean | null {
  const value = new URLSearchParams(search).get('sidebar')?.trim().toLowerCase();
  if (!value) return null;
  if (COLLAPSED_VALUES.has(value)) return true;
  if (EXPANDED_VALUES.has(value)) return false;
  return null;
}

/** 从当前 URL 读取侧栏默认偏好；非浏览器环境返回 null。 */
export function readSidebarPreferenceFromUrl(): boolean | null {
  if (typeof window === 'undefined') return null;
  return readSidebarPreference(window.location.search);
}
