'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const assert = require('node:assert/strict');

const { evaluateContracts } = require('../check-engineering-contracts');

function fixture(content) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'engineering-contract-'));
  fs.writeFileSync(path.join(root, 'contract.txt'), content);
  return root;
}

const contract = {
  id: 'fixture',
  file: 'contract.txt',
  require: [/same-origin/],
  forbid: [/localhost:8090/],
};

test('passes when required policy exists and forbidden value is absent', () => {
  const result = evaluateContracts(fixture('same-origin'), [contract]);
  assert.equal(result.passed.length, 1);
  assert.deepEqual(result.failures, []);
});

test('reports missing and forbidden policy drift', () => {
  const result = evaluateContracts(fixture('localhost:8090'), [contract]);
  assert.equal(result.failures.length, 1);
  assert.match(result.failures[0].reason, /missing/);
  assert.match(result.failures[0].reason, /forbidden/);
});

test('reports a missing contract file', () => {
  const result = evaluateContracts(os.tmpdir(), [{ ...contract, file: 'missing-file' }]);
  assert.equal(result.failures[0].reason, 'file is missing');
});

// ── 吸收上游成果的护栏自证（P0 要求：删除类倒退必须让门禁变红）──────────────
// 背景：acl-gate 只校验现存路由是否受保护，test-coverage-guard 只校验改动是否配套测试，
// 两者都放行「删除」动作。这里的用例就是在证明新护栏确实拦得住删除。
const { CONTRACTS, SURFACE_GUARDS, evaluateSurfaceGuards } = require('../check-engineering-contracts');

function surfaceFixture(files) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'surface-guard-'));
  for (const [rel, content] of Object.entries(files)) {
    const abs = path.join(root, rel);
    fs.mkdirSync(path.dirname(abs), { recursive: true });
    fs.writeFileSync(abs, content);
  }
  return root;
}

const routeGuard = {
  id: 'guard-routes',
  kind: 'regex-in-dir',
  dir: 'router',
  fileFilter: /\.go$/,
  pattern: /\.(GET|POST|PUT|DELETE|PATCH)\(/,
  min: 3,
  why: 'routes shrank',
};

test('route floor turns red when routes are deleted', () => {
  const ok = surfaceFixture({ 'router/a.go': 'g.GET("/x"); g.POST("/y"); g.PUT("/z");' });
  assert.deepEqual(evaluateSurfaceGuards(ok, [routeGuard]).failures, []);

  const shrunk = surfaceFixture({ 'router/a.go': 'g.GET("/x"); g.POST("/y");' });
  const result = evaluateSurfaceGuards(shrunk, [routeGuard]);
  assert.equal(result.failures.length, 1);
  assert.match(result.failures[0].reason, /count 2 violates min 3/);
});

test('removing the whole directory fails loudly instead of silently passing', () => {
  const empty = fs.mkdtempSync(path.join(os.tmpdir(), 'surface-empty-'));
  const result = evaluateSurfaceGuards(empty, [routeGuard]);
  assert.equal(result.failures.length, 1);
  assert.match(result.failures[0].reason, /directory router is missing/);
});

test('frontend test floor turns red when test files are deleted wholesale', () => {
  const guard = {
    id: 'guard-frontend-tests',
    kind: 'walk-count',
    dir: 'src',
    fileFilter: /\.(test|spec)\.(ts|tsx)$/,
    min: 2,
    why: 'tests shrank',
  };
  const full = surfaceFixture({
    'src/a.test.ts': 'x', 'src/nested/b.spec.tsx': 'x', 'src/decoy/helper.ts': 'x',
  });
  assert.deepEqual(evaluateSurfaceGuards(full, [guard]).failures, []);

  const gutted = surfaceFixture({ 'src/decoy/helper.ts': 'x' });
  const result = evaluateSurfaceGuards(gutted, [guard]);
  assert.match(result.failures[0].reason, /count 0 violates min 2/);
});

test('vendored dist ceiling rejects absorbing upstream build artifacts', () => {
  const guard = { id: 'guard-dist', kind: 'tracked-file-count', dir: 'itsm-cli/dist', max: 42, why: 'vendored' };
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'surface-dist-'));
  const stub = (n) => ({ gitLsFiles: () => Array.from({ length: n }, (_, i) => `itsm-cli/dist/f${i}.js`) });

  assert.deepEqual(evaluateSurfaceGuards(root, [guard], stub(42)).failures, []);

  const upstreamSized = evaluateSurfaceGuards(root, [guard], stub(144));
  assert.equal(upstreamSized.failures.length, 1);
  assert.match(upstreamSized.failures[0].reason, /count 144 violates max 42/);
});

test('tracked-file guard still reports a result when git is unavailable', () => {
  const guard = { id: 'guard-dist2', kind: 'tracked-file-count', dir: 'itsm-cli/dist', max: 1, why: 'x' };
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'surface-nogit-'));
  const result = evaluateSurfaceGuards(root, [guard], { gitLsFiles: () => null });
  assert.deepEqual(result.failures, []);
  assert.match(result.passed[0].note, /git unavailable/);
});

test('Next.js regression in the frontend manifest is forbidden', () => {
  const real = CONTRACTS.find(c => c.id === 'absorption-no-nextjs-regression');
  assert.ok(real, 'absorption-no-nextjs-regression contract must exist');

  const probe = (content) => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'nextjs-guard-'));
    fs.writeFileSync(path.join(root, 'package.json'), content);
    return evaluateContracts(root, [{ ...real, file: 'package.json' }]);
  };

  assert.deepEqual(probe('{"scripts":{"build":"tsc -b && vite build"}}').failures, []);
  const regressed = probe('{"scripts":{"build":"next build"},"devDependencies":{"next":"^15.5.28"}}');
  assert.ok(regressed.failures.length >= 1);
  assert.match(regressed.failures[0].reason, /missing/);
  assert.match(regressed.failures[0].reason, /forbidden/);
});

test('surface guard set is present and internally consistent', () => {
  assert.ok(SURFACE_GUARDS.length >= 6, 'expected at least 6 surface guards');
  for (const guard of SURFACE_GUARDS) {
    assert.match(guard.id, /^absorption-/, `guard id must be namespaced: ${guard.id}`);
    const bounds = [guard.min, guard.max].filter(v => v !== undefined);
    assert.equal(bounds.length, 1, `guard ${guard.id} must set exactly one of min/max`);
    assert.ok(guard.why, `guard ${guard.id} must explain which regression it blocks`);
  }
});
