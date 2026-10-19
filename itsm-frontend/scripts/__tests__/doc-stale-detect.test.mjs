/**
 * H1-03 detect-doc-stale 集成测试（node:test）：
 * 以合成 base/head 清单跑完整脚本（docs 映射取真实 docs/help），校验三类提议动作。
 * 运行：node --test scripts/__tests__/doc-stale-detect.test.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const FRONTEND_ROOT = path.resolve(__dirname, '..', '..');
const REPO_ROOT = path.resolve(FRONTEND_ROOT, '..');
const DETECTOR = path.join(FRONTEND_ROOT, 'scripts', 'detect-doc-stale.mjs');
const HEAD_MANIFEST = path.join(REPO_ROOT, 'itsm-frontend', 'src', 'generated', 'page-manifest.json');

const page = (pageKey, extra = {}) => ({
  pageKey,
  pattern: `/${pageKey}`,
  title: `标题-${pageKey}`,
  entrypoint: 'chat',
  scene: null,
  suggestions: [],
  ...extra,
});

function runDetector(args, cwd = FRONTEND_ROOT) {
  return execFileSync(process.execPath, [DETECTOR, ...args], { cwd, encoding: 'utf8' });
}

test('detect-doc-stale：改/增/删 → 提议动作与关联文档正确', () => {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'doc-stale-e2e-'));
  const baseFile = path.join(tmpDir, 'base-manifest.json');
  const headFile = path.join(tmpDir, 'head-manifest.json');
  const outFile = path.join(tmpDir, 'proposals.json');

  try {
    // base：dashboard（旧标题）+ sla.monitor（随后在 head 删除）；head：dashboard（新标题）+ brand.new.page（无文档）。
    fs.writeFileSync(
      baseFile,
      JSON.stringify({ generatedAt: 'test-base', pages: [page('dashboard'), page('sla.monitor')] }),
      'utf8'
    );
    fs.writeFileSync(
      headFile,
      JSON.stringify({
        generatedAt: 'test-head',
        pages: [page('dashboard', { title: '仪表盘（新）' }), page('brand.new.page')],
      }),
      'utf8'
    );

    runDetector([
      '--base-manifest',
      baseFile,
      '--head-manifest',
      headFile,
      '--out',
      outFile,
    ]);

    const report = JSON.parse(fs.readFileSync(outFile, 'utf8'));
    const byKey = new Map(report.proposals.map(item => [item.pageKey, item]));

    assert.equal(report.summary.changed, 1);
    assert.equal(report.summary.added, 1);
    assert.equal(report.summary.removed, 1);

    const changed = byKey.get('dashboard');
    assert.equal(changed.action, 'review_update');
    assert.deepEqual(changed.reasons, ['manifest_changed:title']);
    assert.ok(changed.docs.length > 0, 'dashboard 有文档 → review_update 应带关联文档');

    const created = byKey.get('brand.new.page');
    assert.equal(created.action, 'create_doc');
    assert.deepEqual(created.docs, []);

    const removed = byKey.get('sla.monitor');
    assert.equal(removed.action, 'remove_reference');
    assert.ok(removed.docs.length > 0, 'sla.monitor 有文档 → remove_reference 应带关联文档');

    // Markdown 摘要同步产出（Issue/Step Summary 用）。
    const md = fs.readFileSync(outFile.replace(/\.json$/, '.md'), 'utf8');
    assert.match(md, /dashboard/);
    assert.match(md, /brand\.new\.page/);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('detect-doc-stale：同一清单（无变化）→ 0 提议且 --fail 仍退出 0', () => {
  const manifest = JSON.parse(fs.readFileSync(HEAD_MANIFEST, 'utf8'));
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'doc-stale-e2e-clean-'));
  const baseFile = path.join(tmpDir, 'base-manifest.json');
  const outFile = path.join(tmpDir, 'proposals.json');

  try {
    fs.writeFileSync(baseFile, JSON.stringify({ generatedAt: 'test', pages: manifest.pages }), 'utf8');
    runDetector([
      '--base-manifest',
      baseFile,
      '--head-manifest',
      HEAD_MANIFEST,
      '--out',
      outFile,
      '--fail',
    ]);
    const report = JSON.parse(fs.readFileSync(outFile, 'utf8'));
    assert.equal(report.summary.proposals, 0);
    assert.equal(report.summary.changed + report.summary.added + report.summary.removed, 0);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});