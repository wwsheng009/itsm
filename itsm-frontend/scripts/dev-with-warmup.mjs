#!/usr/bin/env node
/**
 * 启动 next dev，并在 server 就绪后自动在后台预热高频路由。
 *
 * 背景：Turbopack dev 无持久化缓存（15.5 stable），每次重启都要重新付冷编译
 * （/login 198s、/tickets/create 134.7s → 首次点击表现为「很久没反应」）。
 * 本脚本把这段等待挪到后台，用户第一次点击即命中已编译路由。
 *
 * 用法：
 *   npm run dev:warmup                 # Turbopack（与 npm run dev 相同参数）
 *   npm run dev:warmup -- --webpack    # 改用 webpack 引擎（有 .next/cache/webpack 持久缓存，冷编译更慢）
 *   npm run dev:warmup -- --no-warm    # 只起 server，不预热
 *
 * 预热 Cookie（受保护路由必须，否则被 307 到 /login 不会编译页面）：
 *   DEV_WARMUP_COOKIE="access_token=…; refresh_token=…" npm run dev:warmup
 *   或把整行 cookie 写入 ../.dev/warmup-cookie.txt（仓库已忽略 .dev/）
 *   只有 refresh_token 时脚本会自动调用 /api/v1/auth/refresh 换新的 access_token。
 */
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(__dirname, '..');
const nextBin = path.join(projectRoot, 'node_modules', 'next', 'dist', 'bin', 'next');
const warmupScript = path.join(__dirname, 'dev-warmup.mjs');

const argv = process.argv.slice(2);
const useWebpack = argv.includes('--webpack');
const skipWarm = argv.includes('--no-warm');
const port = Number(process.env.PORT || 3000);

const devArgs = ['dev', ...(useWebpack ? [] : ['--turbopack']), '--hostname', '0.0.0.0'];

console.log(`[dev] next ${devArgs.join(' ')}（${useWebpack ? 'webpack' : 'turbopack'} 引擎）`);
const dev = spawn(process.execPath, [nextBin, ...devArgs], { cwd: projectRoot, stdio: 'inherit' });

let warmup = null;
let stopping = false;

function stopAll(code = 0) {
  if (stopping) return;
  stopping = true;
  if (warmup && !warmup.killed) warmup.kill();
  if (!dev.killed) dev.kill();
  process.exit(code);
}

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => stopAll(0));
}
dev.on('exit', (code) => {
  console.log(`[dev] dev server 退出（code=${code}）`);
  stopAll(code ?? 0);
});

async function waitForReady(timeoutMs = 10 * 60 * 1000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (stopping) return false;
    try {
      const res = await fetch(`http://127.0.0.1:${port}/login`, { method: 'HEAD' });
      if (res.status < 500) return true;
    } catch {
      /* not ready */
    }
    await new Promise((resolve) => setTimeout(resolve, 3000));
  }
  return false;
}

if (!skipWarm) {
  const ready = await waitForReady();
  if (!ready) {
    console.warn('[dev] 等待 dev server 就绪超时，跳过预热');
  } else {
    const cookieFile = process.env.DEV_WARMUP_COOKIE_FILE || path.join(projectRoot, '..', '.dev', 'warmup-cookie.txt');
    if (!process.env.DEV_WARMUP_COOKIE && fs.existsSync(cookieFile)) {
      console.log(`[dev] 使用 Cookie 文件：${cookieFile}`);
    } else if (!process.env.DEV_WARMUP_COOKIE) {
      console.log('[dev] 未提供 Cookie：受保护路由不会被真正预热（见 dev-warmup.mjs 注释）');
    }
    console.log('[dev] 后台预热开始（可忽略，不影响 dev server）');
    // 默认不轮换 refresh token（会影响浏览器会话）；需要时显式 DEV_WARMUP_REFRESH=1
    const refreshArgs = process.env.DEV_WARMUP_REFRESH === '1' ? ['--refresh'] : [];
    warmup = spawn(process.execPath, [warmupScript, ...refreshArgs], { cwd: projectRoot, stdio: 'inherit' });
    warmup.on('exit', (code) => console.log(`[dev] 预热结束（code=${code}）`));
  }
}
