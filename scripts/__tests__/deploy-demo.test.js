'use strict';

const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const assert = require('node:assert/strict');

const ROOT = path.resolve(__dirname, '../..');
const read = (relative) => fs.readFileSync(path.join(ROOT, relative), 'utf8');

test('demo deploy codifies backup, init gate, ACL repair and smoke test', () => {
  const script = read('scripts/deploy-demo.sh');
  assert.match(script, /pg_dump/);
  assert.match(script, /--force-recreate --no-deps itsm-init-demo/);
  assert.match(script, /setfacl -R/);
  assert.match(script, /setfacl -d -m/);
  assert.match(script, /smoke_attachment_upload\.py/);
  assert.match(script, /wait_for_container_health/);
  assert.doesNotMatch(script, /passw0rd/i);
  assert.doesNotMatch(script, /Demo@2026/);
});

test('demo deploy requires an explicit AI build or reuse strategy', () => {
  const script = read('scripts/deploy-demo.sh');
  assert.match(script, /--with-ai/);
  assert.match(script, /--reuse-ai/);
  assert.match(script, /AI image missing for \$TARGET_VERSION/);
});

test('attachment smoke reads the password from the environment and cleans up', () => {
  const script = read('scripts/demo/smoke_attachment_upload.py');
  assert.match(script, /password-env/);
  assert.match(script, /os\.environ\.get\(args\.password_env/);
  assert.match(script, /usage.*comment_attachment/);
  assert.match(script, /sha256/);
  assert.match(script, /DELETE/);
  assert.doesNotMatch(script, /Demo@2026/);
  assert.doesNotMatch(script, /passw0rd/);
});

test('Makefile exposes the demo deployment entry points', () => {
  const makefile = read('Makefile');
  for (const target of [
    'demo-preflight',
    'demo-deploy',
    'demo-permissions',
    'demo-smoke',
    'demo-status',
  ]) {
    assert.match(makefile, new RegExp(`^${target}:`, 'm'));
  }
});