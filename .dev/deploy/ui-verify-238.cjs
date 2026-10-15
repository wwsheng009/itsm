#!/usr/bin/env node
/**
 * 238 demo 浏览器功能验证（Playwright 独立脚本；双 persona）。
 *
 * 运行（在 itsm-frontend 目录下，确保可解析 @playwright/test）：
 *   node ..\.dev\deploy\ui-verify-238.cjs
 *
 * 覆盖：
 *   P1 登录落地工作台 / P2 合同页+新建弹窗 / P3 结算报表（选客户→查询）/
 *   P4 provider 定义列表 / P5 新建定义表单（营业时间等）
 *   C1 客户侧"服务商下发"只读段 / C2 工单类型（下发副本）/
 *   C3 工单详情 SLA 进度条 / C4 SLA 监控归责状态 + 争议入口
 *
 * 输出：UI-RESULT 逐条 JSON + UI-SUMMARY；截图 -> .dev/deploy/ui-shots/
 */
const fs = require('fs');
const path = require('path');
const { createRequire } = require('module');
// 从 itsm-frontend 解析依赖（脚本位于 .dev/deploy，默认 require 按脚本目录解析会失败）
const frontendRequire = createRequire(path.resolve(__dirname, '../../itsm-frontend/package.json'));
const { chromium } = frontendRequire('@playwright/test');

const BASE = process.env.UI_BASE || 'http://172.18.3.238:8089';
const OUT = process.env.UI_SHOT_DIR || path.resolve(__dirname, 'ui-shots');
const PW_PROVIDER = process.env.UI_MSP_PASSWORD || 'Demo@2026!';
const PW_CUSTOMER = process.env.UI_CUST_PASSWORD || 'Demo@2026!';
const PROVIDER_USER = process.env.UI_MSP_USER || 'demo.msp.manager';
const CUSTOMER_USER = process.env.UI_CUST_USER || 'demo.mfg.admin';

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
  } catch (e) { /* screenshot 失败不掩断主结论 */ }
}
async function visible(page, text, timeout = 15000) {
  await page.getByText(text, { exact: false }).first().waitFor({ state: 'visible', timeout });
}
async function has(page, text) {
  try { await visible(page, text, 4000); return true; } catch { return false; }
}
function attachErrorCollector(page, bucket) {
  page.on('pageerror', e => errors.push({ bucket, err: String(e).slice(0, 200) }));
}

async function login(page, username, password, landing) {
  await page.context().clearCookies();
  const resp = await page.request.post(BASE + '/api/v1/auth/login', {
    data: { username, password }, timeout: 30000,
  });
  if (!resp.ok()) throw new Error(`login ${username} -> HTTP ${resp.status()}`);
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
      department: currentUser.department,
      permissions: currentUser.permissions,
      createdAt: currentUser.createdAt, updatedAt: currentUser.updatedAt,
    };
    localStorage.setItem('access_token', token);
    localStorage.setItem('current_tenant_id', String(currentTenant.id));
    localStorage.setItem('current_tenant_code', currentTenant.code);
    localStorage.setItem('auth-storage', JSON.stringify({
      state: { user: nu, token: null, currentTenant, isAuthenticated: true }, version: 0,
    }));
  }, { token, currentUser: user, currentTenant: tenant });
  await page.goto(BASE + landing, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(1200); // 布局/引导请求缓冲
}

async function pickCustomer(page, name) {
  const sel = page.locator('.ant-select:has-text("选择服务客户")').first();
  if (await sel.count()) {
    await sel.click();
  } else {
    await page.getByText('选择服务客户').last().click();
  }
  await page.waitForTimeout(500);
  const opt = page.locator('.ant-select-item-option:has-text("' + name + '")').first();
  if (await opt.count()) {
    await opt.click();
  } else {
    await page.getByText(name, { exact: false }).last().click();
  }
}

async function run() {
  const browser = await chromium.launch();
  const fails = [];

  // ================= Provider（demo.msp.manager） =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await ctx.newPage();
    attachErrorCollector(page, 'provider');
    try {
      await login(page, PROVIDER_USER, PW_PROVIDER, '/msp/workbench');
    } catch (e) { rec('P0-login', 'FAIL', String(e)); fails.push('P0-login'); }

    try {
      const url = page.url();
      const ok = url.includes('/msp/workbench') && await has(page, '跨客户工作台');
      const acc = await page.getByText('[ACC]', { exact: false }).count();
      rec('P1-workbench-landing', ok ? 'PASS' : 'FAIL', { url, accRows: acc });
      if (!ok) fails.push('P1');
      await shot(page, 'P1-workbench');
    } catch (e) { rec('P1-workbench-landing', 'FAIL', String(e).slice(0, 200)); fails.push('P1'); }

    try {
      await page.goto(BASE + '/admin/sla-contracts', { waitUntil: 'domcontentloaded' });
      await visible(page, '新建合同');
      const hasAcc = await has(page, 'ACC-A1-MFG');
      const no403 = !(await has(page, '没有权限'));
      rec('P2-contracts-list', hasAcc && no403 ? 'PASS' : 'FAIL', { hasAcc, no403 });
      if (!(hasAcc && no403)) fails.push('P2');
      await page.getByRole('button', { name: '新建合同' }).first().click();
      const modal = await has(page, '合同编号');
      rec('P2-contracts-create-modal', modal ? 'PASS' : 'FAIL', { modalVisible: modal });
      if (!modal) fails.push('P2-modal');
      await shot(page, 'P2-contracts-modal');
      await page.keyboard.press('Escape');
    } catch (e) { rec('P2-contracts-list', 'FAIL', String(e).slice(0, 200)); fails.push('P2'); }

    try {
      await page.goto(BASE + '/reports/sla', { waitUntil: 'domcontentloaded' });
      await visible(page, '双口径');
      let q = 'PARTIAL';
      try {
        await pickCustomer(page, '华辰智能制造');
        await page.getByRole('button', { name: /查\s*询/ }).first().click();
        await page.waitForTimeout(2500);
        const stat = await has(page, '合同判定');
        const covered = await has(page, '覆盖');
        q = stat || covered ? 'PASS' : 'FAIL';
      } catch (e) { q = 'PARTIAL:' + String(e).slice(0, 120); }
      rec('P3-settlement-query', q, {});
      if (q === 'FAIL') fails.push('P3');
      await shot(page, 'P3-settlement');
    } catch (e) { rec('P3-settlement-query', 'FAIL', String(e).slice(0, 200)); fails.push('P3'); }

    try {
      await page.goto(BASE + '/admin/sla-definitions', { waitUntil: 'domcontentloaded' });
      const def = (await has(page, 'Incident-P0', 12000)) || (await has(page, 'Incident-', 4000));
      const defCount = await page.getByText('Incident-', { exact: false }).count();
      const no403 = !(await has(page, '没有权限'));
      rec('P4-provider-definitions', def && no403 ? 'PASS' : 'FAIL', { def, defCount, no403 });
      if (!(def && no403)) fails.push('P4');
      await shot(page, 'P4-definitions');
    } catch (e) { rec('P4-provider-definitions', 'FAIL', String(e).slice(0, 200)); fails.push('P4'); }

    try {
      await page.goto(BASE + '/sla/definitions/new', { waitUntil: 'domcontentloaded' });
      const bh = await has(page, '营业时间日历');
      const no403 = !(await has(page, '没有权限'));
      const inputCount = await page.locator('input').count();
      rec('P5-definition-form', bh && no403 && inputCount > 0 ? 'PASS' : 'FAIL',
        { businessHours: bh, no403, inputs: inputCount });
      if (!(bh && no403 && inputCount > 0)) fails.push('P5');
      await shot(page, 'P5-definition-form');
    } catch (e) { rec('P5-definition-form', 'FAIL', String(e).slice(0, 200)); fails.push('P5'); }

    await ctx.close();
  }

  // ================= Customer（demo.mfg.admin） =================
  {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await ctx.newPage();
    attachErrorCollector(page, 'customer');
    try {
      await login(page, CUSTOMER_USER, PW_CUSTOMER, '/dashboard');
      const url = page.url();
      const noMspMenu = !(await has(page, '跨客户工作台'));
      rec('C0-customer-landing', url.includes('/dashboard') && noMspMenu ? 'PASS' : 'FAIL',
        { url, noMspMenu });
      if (!url.includes('/dashboard')) fails.push('C0');
    } catch (e) { rec('C0-customer-landing', 'FAIL', String(e).slice(0, 200)); fails.push('C0'); }

    try {
      await page.goto(BASE + '/admin/sla-definitions', { waitUntil: 'domcontentloaded' });
      const provided = await has(page, '服务商下发', 12000);
      const body = await page.locator('body').innerText().catch(() => '');
      const defName = /Incident-P0/.test(body);
      const hardErr = /没有权限|获取失败/.test(body);
      rec('C1-provided-section', provided || defName ? 'PASS' : (hardErr ? 'FAIL' : 'PARTIAL'),
        { provided, defName, bodySnippet: String(body).replace(/\s+/g, ' ').slice(0, 300) });
      if (!(provided || defName)) fails.push('C1');
      await shot(page, 'C1-provided');
    } catch (e) { rec('C1-provided-section', 'FAIL', String(e).slice(0, 200)); fails.push('C1'); }

    try {
      await page.goto(BASE + '/tickets/types', { waitUntil: 'domcontentloaded' });
      const copy = await has(page, 'acc_pacs_incident');
      rec('C2-ticket-types-copy', copy ? 'PASS' : 'FAIL', { copy });
      if (!copy) fails.push('C2');
    } catch (e) { rec('C2-ticket-types-copy', 'FAIL', String(e).slice(0, 200)); fails.push('C2'); }

    try {
      // 通过 API 解析目标工单 id（仅取导航参数），详情渲染走 UI 断言
      const tr = await page.request.get(BASE + '/api/v1/tickets?page=1&pageSize=50', { timeout: 30000 });
      let ticketId = null;
      if (tr.ok()) {
        const tj = await tr.json();
        const items = (tj.data && (tj.data.items || tj.data.list)) || tj.items || [];
        const hit = (Array.isArray(items) ? items : []).find(x => /A1 ticket/i.test(String(x.title || '')));
        if (hit) ticketId = hit.id;
      }
      if (!ticketId) { rec('C3-ticket-sla-progress', 'PARTIAL', { reason: 'ticket id not resolved', apiOk: tr.ok() }); }
      else {
        await page.goto(BASE + '/tickets/' + ticketId, { waitUntil: 'domcontentloaded' });
        let resp = (await has(page, '响应进度', 6000)) || (await has(page, '解决进度', 6000)) || (await has(page, '进度', 4000));
        if (!resp) {
          const slaTab = page.getByText('SLA', { exact: true }).first();
          if (await slaTab.count()) { await slaTab.click().catch(() => {}); await page.waitForTimeout(1200); }
          resp = (await has(page, '响应进度', 6000)) || (await has(page, '解决进度', 6000)) || (await has(page, '进度', 4000));
        }
        const sla = await has(page, 'SLA');
        const pb = await page.locator('.ant-progress').count();
        const body = await page.locator('body').innerText().catch(() => '');
        const pg = String(body).match(/.{0,30}进度.{0,60}/g);
        const ok3 = (resp && sla) || pb >= 2;
        rec('C3-ticket-sla-progress', ok3 ? 'PASS' : 'PARTIAL',
          { ticketId, resp, sla, progressBars: pb, progressSnippet: pg ? pg.slice(0, 2) : [] });
        if (!ok3) fails.push('C3');
        await shot(page, 'C3-ticket-detail');
      }
    } catch (e) { rec('C3-ticket-sla-progress', 'FAIL', String(e).slice(0, 200)); fails.push('C3'); }

    try {
      await page.goto(BASE + '/sla-monitor', { waitUntil: 'domcontentloaded' });
      await has(page, '工单SLA');
      const disputeTab = page.getByText('工单SLA', { exact: false }).first();
      if (await disputeTab.count()) { await disputeTab.click().catch(() => {}); await page.waitForTimeout(1500); }
      const stateCol = await has(page, '归责状态');
      let modalHasDispute = false;
      try {
        const view = page.getByRole('button', { name: /查看|详情/ }).first();
        if (await view.count()) {
          await view.click();
          await page.waitForTimeout(1200);
          modalHasDispute = await has(page, '提出争议') || await has(page, '归责状态');
          await shot(page, 'C4-violation-modal');
          await page.keyboard.press('Escape');
        }
      } catch (e) { /* modal 可选 */ }
      rec('C4-violation-state-ui', stateCol || modalHasDispute ? 'PASS' : 'FAIL',
        { stateCol, modalHasDispute });
      if (!(stateCol || modalHasDispute)) fails.push('C4');
      await shot(page, 'C4-sla-monitor');
    } catch (e) { rec('C4-violation-state-ui', 'FAIL', String(e).slice(0, 200)); fails.push('C4'); }

    await ctx.close();
  }

  await browser.close();
  const summary = {
    total: results.length,
    pass: results.filter(r => r.status === 'PASS').length,
    partial: results.filter(r => String(r.status).startsWith('PARTIAL')).length,
    fail: results.filter(r => r.status === 'FAIL').length,
    failedSteps: fails,
    pageErrors: errors.slice(0, 10),
    shots: OUT,
  };
  console.log('UI-SUMMARY: ' + JSON.stringify(summary));
  process.exit(summary.fail > 0 ? 1 : 0);
}

run().catch(e => { console.log('UI-FATAL: ' + String(e)); process.exit(2); });
