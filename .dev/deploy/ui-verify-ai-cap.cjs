#!/usr/bin/env node
/**
 * 238 demo 浏览器验证：能力开关迁移（system-config AI 页签）+ MCP 页迁移提示 + URL 页签联动。
 *
 * 运行（在 itsm-frontend 目录下）：
 *   UI_ADMIN_PASSWORD=... node ..\.dev\deploy\ui-verify-ai-cap.cjs
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
const errors = [];
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
  const fails = [];
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  page.on('pageerror', e => errors.push(String(e).slice(0, 200)));

  try {
    await login(page);
    await page.goto(BASE + '/admin/system-config?tab=ai', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(2500);
  } catch (e) {
    rec('A0-login', 'FAIL', String(e).slice(0, 200)); fails.push('A0');
  }

  // A1: 深链 ?tab=ai → AI 页签选中 + 能力卡片可见
  try {
    const tab = page.getByRole('tab', { name: 'AI 配置' });
    const tabCount = await tab.count();
    const selected = tabCount ? await tab.first().getAttribute('aria-selected') : null;
    const card = await page.getByTestId('ai-capability-card').count();
    const info = await page.getByTestId('ai-capabilities-info').count();
    const ok = tabCount > 0 && selected === 'true' && card > 0;
    rec('A1-deeplink-ai-tab', ok ? 'PASS' : 'FAIL', { tabCount, selected, card, info });
    if (!ok) fails.push('A1');
    await shot(page, 'CAP-1-system-config-ai-tab');
  } catch (e) { rec('A1-deeplink-ai-tab', 'FAIL', String(e).slice(0, 200)); fails.push('A1'); }

  // A2: 三个开关 + 来源徽标 + 保存/恢复按钮
  try {
    const sw = {};
    for (const k of ['mcp.enabled', 'mcp.write_enabled', 'bot.enabled']) {
      sw[k] = await page.getByTestId(`ai-capability-switch-${k}`).count();
    }
    const src = await page.getByTestId('ai-capability-source-bot.enabled').count();
    const save = await page.getByTestId('ai-capability-save').count();
    const reset = await page.getByTestId('ai-capability-reset').count();
    const title = await page.getByText('AI 能力开关', { exact: false }).count();
    const ok = Object.values(sw).every(v => v > 0) && src > 0 && save > 0 && reset > 0;
    rec('A2-switches-rendered', ok ? 'PASS' : 'FAIL', { sw, src, save, reset, title });
    if (!ok) fails.push('A2');
  } catch (e) { rec('A2-switches-rendered', 'FAIL', String(e).slice(0, 200)); fails.push('A2'); }

  // A3: 无参数进入 system-config → 点「AI 配置」→ URL 回写 tab=ai
  try {
    await page.goto(BASE + '/admin/system-config', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(2000);
    await page.getByRole('tab', { name: 'AI 配置' }).first().click();
    await page.waitForTimeout(1500);
    const url = page.url();
    const card = await page.getByTestId('ai-capability-card').count();
    const ok = url.includes('tab=ai') && card > 0;
    rec('A3-tab-url-sync', ok ? 'PASS' : 'FAIL', { url, card });
    if (!ok) fails.push('A3');
    await shot(page, 'CAP-2-tab-url-sync');
  } catch (e) { rec('A3-tab-url-sync', 'FAIL', String(e).slice(0, 200)); fails.push('A3'); }

  // B1: MCP 页迁移提示 + 旧卡片消失
  try {
    await page.goto(BASE + '/admin/mcp-servers', { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(2500);
    const moved = await page.getByTestId('mcp-capabilities-moved').count();
    const oldCard = await page.getByTestId('mcp-capability-card').count();
    const aiCard = await page.getByTestId('ai-capability-card').count();
    const body = await page.locator('body').innerText().catch(() => '');
    const textOk = /能力开关已迁移/.test(body);
    const ok = moved > 0 && oldCard === 0 && aiCard === 0;
    rec('B1-mcp-moved-notice', ok ? 'PASS' : 'FAIL', { moved, oldCard, aiCard, textOk });
    if (!ok) fails.push('B1');
    await shot(page, 'CAP-3-mcp-moved');
  } catch (e) { rec('B1-mcp-moved-notice', 'FAIL', String(e).slice(0, 200)); fails.push('B1'); }

  // B2: 迁移提示里的链接点击 → 跳到 AI 页签且卡片可见
  try {
    const link = page.getByRole('link', { name: '前往 AI 配置' });
    const linkCount = await link.count();
    let ok = false, url = page.url(), card = 0;
    if (linkCount > 0) {
      await link.first().click();
      await page.waitForTimeout(2000);
      url = page.url();
      card = await page.getByTestId('ai-capability-card').count();
      ok = url.includes('/admin/system-config') && url.includes('tab=ai') && card > 0;
    }
    rec('B2-moved-link-nav', ok ? 'PASS' : 'FAIL', { linkCount, url, card });
    if (!ok) fails.push('B2');
    await shot(page, 'CAP-4-moved-link-nav');
  } catch (e) { rec('B2-moved-link-nav', 'FAIL', String(e).slice(0, 200)); fails.push('B2'); }

  await ctx.close();
  await browser.close();

  const summary = {
    total: results.length,
    pass: results.filter(r => r.status === 'PASS').length,
    fail: results.filter(r => r.status === 'FAIL').length,
    failedSteps: fails,
    pageErrors: errors.slice(0, 10),
    shots: OUT,
  };
  console.log('UI-SUMMARY: ' + JSON.stringify(summary));
  process.exit(summary.fail > 0 ? 1 : 0);
}

run().catch(e => { console.log('UI-FATAL: ' + String(e)); process.exit(2); });
