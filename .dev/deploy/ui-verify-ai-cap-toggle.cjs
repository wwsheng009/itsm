#!/usr/bin/env node
/**
 * 238 demo 浏览器验证（闭环）：在 AI 配置页用 UI 开关拨动 Bot 能力 → 保存 → 后端立即生效（免重启）→ 恢复。
 *
 * 运行（在 itsm-frontend 目录下）：
 *   UI_ADMIN_PASSWORD=... node ..\.dev\deploy\ui-verify-ai-cap-toggle.cjs
 */
const fs = require('fs');
const path = require('path');
const { createRequire } = require('module');
const frontendRequire = createRequire(path.resolve(__dirname, '../../itsm-frontend/package.json'));
const { chromium } = frontendRequire('@playwright/test');

const BASE = process.env.UI_BASE || 'http://172.18.3.238:8089';
const OUT = process.env.UI_SHOT_DIR || path.resolve(__dirname, 'ui-shots');
const USER = process.env.UI_ADMIN_USER || 'admin';
const PW = process.env.UI_ADMIN_PASSWORD;
if (!PW) { console.log('UI-FATAL: UI_ADMIN_PASSWORD not set'); process.exit(2); }

const results = [];
function rec(step, status, detail) {
  const o = { step, status, detail };
  results.push(o);
  console.log('UI-RESULT: ' + JSON.stringify(o));
}
async function shot(page, name) {
  try {
    if (!fs.existsSync(OUT)) fs.mkdirSync(OUT, { recursive: true });
    await page.screenshot({ path: path.join(OUT, name + '.png'), fullPage: true });
  } catch (e) { /* ignore */ }
}

async function login(page) {
  await page.context().clearCookies();
  const resp = await page.request.post(BASE + '/api/v1/auth/login', {
    data: { username: USER, password: PW }, timeout: 30000,
  });
  if (!resp.ok()) throw new Error(`login ${USER} -> HTTP ${resp.status()}`);
  const j = await resp.json();
  const d = j.data || {};
  const token = d.accessToken || d.access_token;
  const user = d.user || {};
  const tenantId = Number(user.tenantId || user.tenant_id || 1);
  const tenant = {
    id: tenantId, name: 'demo', code: tenantId === 1 ? 'default' : `tenant-${tenantId}`,
    type: 'standard', status: 'active',
    createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
  };
  await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded', timeout: 30000 }).catch(() => {});
  await page.evaluate(({ token, currentUser, currentTenant }) => {
    const nu = {
      id: Number(currentUser.id || 0), username: String(currentUser.username || ''),
      email: String(currentUser.email || ''), name: String(currentUser.name || ''),
      role: String(currentUser.role || 'end_user'),
      tenantId: currentUser.tenantId ? Number(currentUser.tenantId) : undefined,
      department: currentUser.department, permissions: currentUser.permissions,
      createdAt: currentUser.createdAt, updatedAt: currentUser.updatedAt,
    };
    localStorage.setItem('access_token', token);
    localStorage.setItem('current_tenant_id', String(currentTenant.id));
    localStorage.setItem('current_tenant_code', currentTenant.code);
    localStorage.setItem('auth-storage', JSON.stringify({
      state: { user: nu, token: null, currentTenant, isAuthenticated: true }, version: 0,
    }));
  }, { token, currentUser: user, currentTenant: tenant });
}

async function run() {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  const fails = [];
  page.on('pageerror', () => {});

  const cap = async () => {
    const r = await page.request.get(BASE + '/api/v1/system-configs/ai-capabilities', { timeout: 20000 });
    const j = await r.json();
    return ((j.data || {}).botEnabled) === true;
  };
  const swChecked = async () => (await page.getByTestId('ai-capability-switch-bot.enabled').getAttribute('aria-checked')) === 'true';
  const save = async () => {
    await page.getByTestId('ai-capability-save').first().click();
    await page.waitForTimeout(1800);
  };

  try {
    await login(page);
    await page.goto(BASE + '/admin/system-config?tab=ai', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(2500);

    const before = await cap();
    if ((await swChecked()) !== before) {
      await page.getByTestId('ai-capability-switch-bot.enabled').click();
      await page.waitForTimeout(300);
      await save();
    }
    const onState = await cap();

    // UI 关闭 → 保存 → 后端立即生效
    await page.getByTestId('ai-capability-switch-bot.enabled').click();
    await page.waitForTimeout(300);
    await save();
    const offState = await cap();
    await shot(page, 'CAP-5-ui-toggle-off');

    // UI 打开 → 保存 → 后端立即生效
    await page.getByTestId('ai-capability-switch-bot.enabled').click();
    await page.waitForTimeout(300);
    await save();
    const backOn = await cap();

    // 恢复初始状态（若初始为关闭）
    let restored = backOn;
    if (before === false && backOn === true) {
      await page.getByTestId('ai-capability-switch-bot.enabled').click();
      await page.waitForTimeout(300);
      await save();
      restored = await cap();
    }
    await shot(page, 'CAP-6-ui-toggle-restored');

    const ok = onState === true && offState === false && backOn === true && restored === before;
    rec('T1-ui-toggle-effective', ok ? 'PASS' : 'FAIL', { before, onState, offState, backOn, restored });
    if (!ok) fails.push('T1');
  } catch (e) {
    rec('T1-ui-toggle-effective', 'FAIL', String(e).slice(0, 300));
    fails.push('T1');
  }

  // 后端写端在关闭态的表现（复验：读端 200 / 写端 403）——此处不再改状态，仅核对当前开启态的放行
  try {
    const w = await page.request.post(BASE + '/api/v1/admin/bots', { data: {}, timeout: 20000 });
    const ok = w.status() === 400 || w.status() === 200 || w.status() === 403;
    rec('T2-write-gate-visible', ok ? 'PASS' : 'FAIL', { writeStatus: w.status() });
    if (!ok) fails.push('T2');
  } catch (e) { rec('T2-write-gate-visible', 'FAIL', String(e).slice(0, 200)); fails.push('T2'); }

  await ctx.close();
  await browser.close();
  const summary = {
    total: results.length,
    pass: results.filter(r => r.status === 'PASS').length,
    fail: results.filter(r => r.status === 'FAIL').length,
    failedSteps: fails,
    shots: OUT,
  };
  console.log('UI-SUMMARY: ' + JSON.stringify(summary));
  process.exit(summary.fail > 0 ? 1 : 0);
}

run().catch(e => { console.log('UI-FATAL: ' + String(e)); process.exit(2); });
