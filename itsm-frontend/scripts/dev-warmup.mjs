#!/usr/bin/env node
/**
 * dev server 路由预热脚本（2026-09-22 重写）。
 *
 * 背景：Next dev 是「按需编译」——每个路由只有第一次被请求时才编译。本机实测
 * （VMware VM / Xeon E5-2686 v4 8vCPU / 虚拟磁盘 / Defender 实时防护）：
 *   - Turbopack 单路由冷编译 60~200s（/login 198.1s、/tickets/create 134.7s）
 *   - Turbopack 15.5 stable 无持久化缓存（experimental.turbopackPersistentCaching 会抛 CanaryOnlyError），
 *     所以 **每次重启 dev server 都要重新付一遍**。
 *   - 首次点击菜单项 ⇒ 浏览器等待服务端编译 ⇒ 表现为「点了很久没反应」。
 * 本脚本把这些编译挪到后台，让点击命中已编译路由。
 *
 * 用法：
 *   node scripts/dev-warmup.mjs                          # 默认端口 3000，预热默认路由表
 *   node scripts/dev-warmup.mjs /dashboard /tickets      # 自定义路由
 *   node scripts/dev-warmup.mjs --port 3001 --concurrency 2
 *   DEV_WARMUP_COOKIE="access_token=...; refresh_token=..." node scripts/dev-warmup.mjs
 *   node scripts/dev-warmup.mjs --cookie-file ../.dev/warmup-cookie.txt [--refresh]
 *   DEV_WARMUP_ROUTES=/login,/dashboard node scripts/dev-warmup.mjs
 *
 * 受保护路由（/dashboard、/tickets…）会被 middleware 校验 access_token 后重定向到 /login，
 * 未带 Cookie 时 **不会触发页面编译**，因此预热受保护路由必须提供 Cookie：
 *   1) 浏览器 DevTools → Network → 任意文档请求 → Request Headers → cookie 整行复制；
 *   2) 存成文件（推荐 ../.dev/warmup-cookie.txt，仓库已 .gitignore 整个 .dev/）；
 *   3) 或设置 DEV_WARMUP_COOKIE 环境变量。
 * 注意：access_token 只有 ~15 分钟有效期。加 --refresh 会让脚本 POST /api/v1/auth/refresh 换新 token，
 * 但后端 refresh token 会轮换（rotation），可能把浏览器里的会话挤掉 → 默认关闭，仅在明确知道后果时使用。
 * 更稳的做法：需要长时间预热时，隔一段时间从浏览器 DevTools 重新复制一次 cookie 再跑。
 * 脚本启动时会先用 JWT 的 exp 自检 access_token：已过期直接报错退出，避免「跑完一圈但全部 307 跳过、
 * 页面根本没编译」的假成功（cookie 过期时受保护路由会在 middleware 就被重定向，不会触发编译）。
 *
 * 默认串行（concurrency=1）：并发编译会互相抢 CPU，反而更慢；CPU 富余时可用 --concurrency 提升。
 */

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(__dirname, '..');

// 侧边栏/高频入口（按用户实际点击路径挑选）。用 -- 参数或 DEV_WARMUP_ROUTES 覆盖。
const DEFAULT_ROUTES = [
  '/login',
  '/',
  '/dashboard',
  '/tickets',
  '/my-requests',
  '/approvals',
  '/service-catalog',
  '/knowledge',
  '/incidents',
  '/problems',
  '/changes',
  '/cmdb',
  '/assets',
  '/sla',
];

/** @param {string[]} argv */
function parseArgs(argv) {
  const routes = [];
  let port = Number(process.env.PORT || 3000);
  let host = process.env.DEV_WARMUP_HOST || '127.0.0.1';
  let concurrency = 1;
  let cookie = process.env.DEV_WARMUP_COOKIE || '';
  let cookieFile = process.env.DEV_WARMUP_COOKIE_FILE || path.join(projectRoot, '..', '.dev', 'warmup-cookie.txt');
  let refresh = false;
  let timeoutMs = 30 * 60 * 1000;

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--port') {
      port = Number(argv[i + 1]);
      i += 1;
    } else if (arg === '--host') {
      host = argv[i + 1];
      i += 1;
    } else if (arg === '--concurrency') {
      concurrency = Math.max(1, Number(argv[i + 1]) || 1);
      i += 1;
    } else if (arg === '--cookie') {
      cookie = argv[i + 1] || '';
      i += 1;
    } else if (arg === '--cookie-file') {
      cookieFile = argv[i + 1] || cookieFile;
      i += 1;
    } else if (arg === '--refresh') {
      refresh = true;
    } else if (arg === '--timeout') {
      timeoutMs = Number(argv[i + 1]) || timeoutMs;
      i += 1;
    } else if (arg === '--help' || arg === '-h') {
      console.log(fs.readFileSync(fileURLToPath(import.meta.url), 'utf8').split('*/')[0]);
      process.exit(0);
    } else if (arg.startsWith('/')) {
      routes.push(arg);
    }
  }

  if (routes.length === 0 && process.env.DEV_WARMUP_ROUTES) {
    routes.push(
      ...process.env.DEV_WARMUP_ROUTES.split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    );
  }

  if (!cookie && cookieFile && fs.existsSync(cookieFile)) {
    cookie = fs.readFileSync(cookieFile, 'utf8').trim();
  }

  return { routes: routes.length > 0 ? routes : DEFAULT_ROUTES, port, host, concurrency, cookie, cookieFile, refresh, timeoutMs };
}

function cookieHeaderToMap(raw) {
  /** @type {Record<string,string>} */
  const map = {};
  for (const part of raw.split(';')) {
    const idx = part.indexOf('=');
    if (idx <= 0) continue;
    const key = part.slice(0, idx).trim();
    const value = part.slice(idx + 1).trim();
    if (key) map[key] = value;
  }
  return map;
}

function cookieMapToHeader(map) {
  return Object.entries(map)
    .map(([k, v]) => `${k}=${v}`)
    .join('; ');
}

/**
 * 用 refresh_token 换新的 access_token（access_token 只有 ~15 分钟有效期）。
 * 风险：后端 refresh token 轮换会作废浏览器当前会话的 refresh_token，非必要不要打开。
 */
async function refreshAccessToken(baseUrl, cookieMap) {
  try {
    console.warn('[warmup] --refresh 会轮换 refresh token，可能导致浏览器会话被登出（dev 风险自负）');
    const res = await fetch(`${baseUrl}/api/v1/auth/refresh`, {
      method: 'POST',
      headers: { cookie: cookieMapToHeader(cookieMap) },
      redirect: 'manual',
    });
    const setCookies = typeof res.headers.getSetCookie === 'function' ? res.headers.getSetCookie() : [];
    let updated = false;
    for (const sc of setCookies) {
      const [pair] = sc.split(';');
      const idx = pair.indexOf('=');
      if (idx <= 0) continue;
      const key = pair.slice(0, idx).trim();
      const value = pair.slice(idx + 1).trim();
      if (value) {
        cookieMap[key] = value;
        updated = true;
      }
    }
    console.log(`[warmup] refresh-token: HTTP ${res.status}${updated ? '，已更新 access_token' : '（未返回 Set-Cookie）'}`);
    return res.status < 400 && updated;
  } catch (error) {
    console.warn(`[warmup] refresh-token 失败：${error instanceof Error ? error.message : String(error)}`);
    return false;
  }
}

async function waitForServer(baseUrl, timeoutMs = 300000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseUrl}/login`, { method: 'HEAD' });
      if (res.status < 500) return true;
    } catch {
      /* server not up yet */
    }
    await new Promise((resolve) => setTimeout(resolve, 2000));
  }
  return false;
}

async function warmRoute(baseUrl, route, cookieHeader, timeoutMs) {
  const url = `${baseUrl}${route}${route.includes('?') ? '&' : '?'}__warmup=1`;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const started = Date.now();
  try {
    const res = await fetch(url, {
      redirect: 'manual',
      headers: cookieHeader ? { cookie: cookieHeader } : undefined,
      signal: controller.signal,
    });
    // 必须消费响应体，否则 dev server 的响应流不会真正走完
    await res.arrayBuffer();
    return { route, status: res.status, ms: Date.now() - started };
  } catch (error) {
    return { route, status: 0, ms: Date.now() - started, error: error instanceof Error ? error.message : String(error) };
  } finally {
    clearTimeout(timer);
  }
}

/** 解析 JWT payload 的 exp（秒）；解析失败返回 null（不阻断预热）。 */
function jwtExp(token) {
  try {
    const payload = String(token).split('.')[1];
    if (!payload) return null;
    const json = JSON.parse(Buffer.from(payload.replace(/-/g, '+').replace(/_/g, '/'), 'base64').toString('utf8'));
    return typeof json.exp === 'number' ? json.exp : null;
  } catch {
    return null;
  }
}

/**
 * access_token 过期自检：过期时 middleware 会 307 重定向到 /login，路由不会被编译，
 * 预热会「全部跳过」却看似成功，用户随后点菜单仍然要等冷编译。
 */
function assertAccessTokenFresh(cookieMap, cookieFile) {
  const exp = cookieMap.access_token ? jwtExp(cookieMap.access_token) : null;
  if (!exp) return;
  const remainMs = exp * 1000 - Date.now();
  if (remainMs <= 0) {
    console.error(
      `[warmup] access_token 已于 ${new Date(exp * 1000).toLocaleString()} 过期，受保护路由会被 307 跳过且不会编译。`
    );
    console.error(`[warmup] 请从浏览器 DevTools 复制最新 Cookie 覆盖 ${cookieFile}；`);
    console.error('[warmup]   取法：DevTools → Network → 任一 /api/v1/* 请求 → Request Headers → 复制整行 cookie；');
    console.error(`[warmup] 或加 --refresh 用 refresh_token 换新（会轮换 refresh token，可能挤掉浏览器会话）。`);
    process.exit(1);
  }
  console.log(`[warmup] access_token 剩余有效期约 ${Math.max(1, Math.round(remainMs / 60000))} 分钟`);
}

const { routes, port, host, concurrency, cookie, cookieFile, refresh, timeoutMs } = parseArgs(process.argv.slice(2));
const baseUrl = `http://${host}:${port}`;
const cookieMap = cookie ? cookieHeaderToMap(cookie) : {};

// 先做纯本地自检，避免等 dev server 就绪几十秒后才发现 cookie 过期
if (!refresh) {
  assertAccessTokenFresh(cookieMap, cookieFile);
}

console.log(`[warmup] 等待 dev server 就绪：${baseUrl}`);
if (!(await waitForServer(baseUrl))) {
  console.error('[warmup] 等待超时：dev server 未就绪，跳过预热');
  process.exit(1);
}

if (cookieMap.refresh_token && refresh) {
  await refreshAccessToken(baseUrl, cookieMap);
  assertAccessTokenFresh(cookieMap, cookieFile);
}
const cookieHeader = cookieMapToHeader(cookieMap);

if (!cookieMap.access_token) {
  console.log(`[warmup] 未提供 access_token：受保护路由会被 307 重定向，不会真正预热。`);
  console.log(`[warmup] 可写 Cookie 到：${cookieFile}（或设置 DEV_WARMUP_COOKIE / --cookie-file）`);
} else {
  console.log(`[warmup] 已带 Cookie 预热（access_token 长度 ${cookieMap.access_token.length}）`);
}

console.log(`[warmup] 开始预热 ${routes.length} 个路由，并发 ${concurrency}（首个路由可能耗时数十秒~数分钟）`);
const queue = [...routes];
let slowest = { route: '', ms: 0 };
let ok = 0;
let skipped = 0;
let failed = 0;

async function worker() {
  for (;;) {
    const route = queue.shift();
    if (!route) return;
    const result = await warmRoute(baseUrl, route, cookieHeader, timeoutMs);
    const secs = (result.ms / 1000).toFixed(1);
    if (result.error) {
      failed += 1;
      console.warn(`⚠️  ${route.padEnd(20)} 请求失败：${result.error}`);
    } else if (result.status >= 300 && result.status < 400) {
      skipped += 1;
      console.log(`⏭️  ${route.padEnd(20)} ${result.status}  ${secs}s（被重定向，未编译页面）`);
    } else if (result.status >= 400) {
      failed += 1;
      console.warn(`⚠️  ${route.padEnd(20)} ${result.status}  ${secs}s`);
    } else {
      ok += 1;
      console.log(`✓  ${route.padEnd(20)} ${result.status}  ${secs}s`);
    }
    if (result.ms > slowest.ms) slowest = { route, ms: result.ms };
  }
}

await Promise.all(Array.from({ length: Math.min(concurrency, routes.length) }, () => worker()));

console.log(
  `[warmup] 完成：成功 ${ok}，跳过 ${skipped}，失败 ${failed}；最慢 ${slowest.route} ${(slowest.ms / 1000).toFixed(1)}s`
);
if (skipped > 0 && ok === 0) {
  console.warn(
    '[warmup] 全部路由被重定向、未编译任何页面：通常是 access_token 失效（15 分钟有效期）→ 更新 cookie 后重跑。'
  );
}
