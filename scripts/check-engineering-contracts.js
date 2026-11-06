#!/usr/bin/env node

'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

const REPO_ROOT = path.resolve(__dirname, '..');

const CONTRACTS = [
  {
    id: 'browser-api-same-origin',
    file: 'itsm-frontend/src/lib/api/api-config.ts',
    // Vite 迁移后：构建期注入 import.meta.env.VITE_*（见 docs/plan/vite-migration-plan.md §5.7）
    require: [/API_BASE_URL\s*=\s*import\.meta\.env\.VITE_API_URL\s*\|\|\s*''/],
    forbid: [/localhost:8090/, /process\.env\.ITSM_BACKEND_URL/],
  },
  {
    id: 'production-env-api-base',
    file: '.env.prod.example',
    require: [/^VITE_API_URL=\s*$/m, /^ITSM_BACKEND_URL=http:\/\/itsm-backend:8090\s*$/m],
    forbid: [/^VITE_API_URL=\/api\s*$/m, /^VITE_API_URL=https?:\/\//m],
  },
  {
    id: 'frontend-docker-env-isolation',
    file: 'itsm-frontend/.dockerignore',
    require: [/^\.env\.\*\s*$/m],
    forbid: [],
  },
  {
    id: 'production-compose-runtime-contract',
    file: 'docker-compose.prod.yml',
    require: [
      /ITSM_CORS_ALLOWED_ORIGINS=/,
      /http:\/\/127\.0\.0\.1:3000/,
      /http:\/\/127\.0\.0\.1\/health/,
      /127\.0\.0\.1:8090:8090/,
      /image: itsm-backend:\$\{VERSION:\?VERSION must be set\}/,
    ],
    forbid: [
      /^\s*- CORS_ALLOWED_ORIGINS=/m,
      /^\s*- "8090:8090"/m,
      /^\s*- "443:443"/m,
      /image: itsm-backend:\$\{VERSION:-latest\}/,
    ],
  },
  {
    id: 'security-scans-block-release',
    file: '.github/workflows/security.yml',
    require: [/severity: 'CRITICAL,HIGH'/, /exit-code: '1'/],
    forbid: [/-no-fail/, /severity: 'CRITICAL'\s*\n\s*exit-code: '1'/],
  },
  {
    id: 'frontend-ci-same-origin',
    file: '.github/workflows/frontend-ci.yml',
    require: [/VITE_API_URL:\s*''/],
    forbid: [/VITE_API_URL:\s*https?:\/\//],
  },
  {
    id: 'release-same-origin',
    file: '.github/workflows/release.yml',
    require: [/VITE_API_URL:\s*''/],
    forbid: [/VITE_API_URL:\s*https?:\/\//],
  },
  {
    id: 'reproducible-docs',
    file: '.github/workflows/docs.yml',
    require: [/pip install --requirement requirements-docs\.txt/],
    forbid: [/pip install mkdocs==/],
  },
  {
    // 上游 heidsoft/main 仍为 Next.js（next build + src/app/**）；本地已迁 Vite。
    // 吸收上游时任何"顺手带文件/带依赖"都会把前端拖回旧架构 → 见
    // docs/plan/upstream-absorption-plan-2026-11-06.md §2 L1。
    id: 'absorption-no-nextjs-regression',
    file: 'itsm-frontend/package.json',
    require: [/"build":\s*"[^"]*vite build/],
    forbid: [/"next":\s*"/, /"next-\w*":\s*"/],
  },
];

// ── 吸收上游成果的「面不减」护栏 ──────────────────────────────────────────
// 口径与基线出处：docs/plan/upstream-absorption-plan-2026-11-06.md 附录 A（2026-11-06 实测）。
//   本地 HEAD            routes=999 rbacKeys=1123 entSchemas=172 sqlMigrations=129 frontendTests=375
//   上游 heidsoft/main   routes=745 rbacKeys= 891 entSchemas=136 sqlMigrations= 75 frontendTests=239
// 这些数字不是增长目标，而是"整块合并上游 = 倒退"的机器可证拦截线：
// acl-gate 只看现存路由是否受保护、test-coverage-guard 只看改动是否配套测试，
// 两者都放行"删除"动作，故此处补下限。
const SURFACE_GUARDS = [
  {
    id: 'absorption-backend-routes',
    kind: 'regex-in-dir',
    dir: 'itsm-backend/router',
    fileFilter: /\.go$/,
    pattern: /\.(GET|POST|PUT|DELETE|PATCH)\(/,
    min: 999,
    why: '路由注册数不得因合并/移植缩水',
  },
  {
    id: 'absorption-rbac-precheck-keys',
    kind: 'regex-in-file',
    file: 'itsm-backend/middleware/rbac_precheck_gen.go',
    pattern: /"\/api\/v1[^"]*":\s*\{/,
    min: 1123,
    why: '不得用上游生成物覆盖本地权限面',
  },
  {
    id: 'absorption-ent-schemas',
    kind: 'file-count',
    dir: 'itsm-backend/ent/schema',
    fileFilter: /\.go$/,
    min: 172,
    why: 'ent 模型不得被上游精简版覆盖',
  },
  {
    id: 'absorption-sql-migrations',
    kind: 'file-count',
    dir: 'itsm-backend/migrations',
    fileFilter: /\.sql$/,
    min: 129,
    why: '版本化迁移历史不得丢失',
  },
  {
    id: 'absorption-frontend-tests',
    kind: 'walk-count',
    dir: 'itsm-frontend/src',
    fileFilter: /\.(test|spec)\.(ts|tsx)$/,
    min: 375,
    why: '前端测试文件不得被替换删除（coverage-guard 不禁止整删测试文件）',
  },
  {
    // 上限而非零：本地历史上已入库 42 个 itsm-cli/dist 文件，上游为 144 个。
    // 目标是"吸收不得增多产物"；清理既有 42 个另立任务（见 §6 风险登记）。
    id: 'absorption-vendored-cli-dist',
    kind: 'tracked-file-count',
    dir: 'itsm-cli/dist',
    max: 42,
    why: '吸收上游 CLI 不得带入其构建产物',
  },
];

function evaluateContracts(root = REPO_ROOT, contracts = CONTRACTS) {
  const failures = [];
  const passed = [];

  for (const contract of contracts) {
    const absolutePath = path.join(root, contract.file);
    if (!fs.existsSync(absolutePath)) {
      failures.push({ id: contract.id, file: contract.file, reason: 'file is missing' });
      continue;
    }

    const content = fs.readFileSync(absolutePath, 'utf8');
    const missing = contract.require.filter(pattern => !pattern.test(content));
    const forbidden = contract.forbid.filter(pattern => pattern.test(content));

    if (missing.length || forbidden.length) {
      failures.push({
        id: contract.id,
        file: contract.file,
        reason: [
          ...missing.map(pattern => `missing ${pattern}`),
          ...forbidden.map(pattern => `forbidden ${pattern}`),
        ].join('; '),
      });
    } else {
      passed.push({ id: contract.id, file: contract.file });
    }
  }

  return { passed, failures };
}

function countMatches(haystack, pattern) {
  const global = new RegExp(pattern.source, pattern.flags.includes('g') ? pattern.flags : `${pattern.flags}g`);
  let n = 0;
  while (global.exec(haystack) !== null) n += 1;
  return n;
}

function listFiles(root, dir, { recursive = false, fileFilter = null } = {}) {
  const abs = path.join(root, dir);
  if (!fs.existsSync(abs) || !fs.statSync(abs).isDirectory()) return null;
  const out = [];
  const walk = (d) => {
    for (const entry of fs.readdirSync(d, { withFileTypes: true })) {
      const child = path.join(d, entry.name);
      if (entry.isDirectory()) {
        if (recursive) walk(child);
        continue;
      }
      if (!fileFilter || fileFilter.test(entry.name)) out.push(child);
    }
  };
  walk(abs);
  return out;
}

function defaultGitLsFiles(root, rel) {
  try {
    return execFileSync('git', ['-C', root, 'ls-files', rel], { encoding: 'utf8' })
      .split('\n')
      .filter(Boolean);
  } catch {
    return null;
  }
}

function evaluateSurfaceGuards(root = REPO_ROOT, guards = SURFACE_GUARDS, deps = {}) {
  const gitLsFiles = deps.gitLsFiles || ((rel) => defaultGitLsFiles(root, rel));
  const failures = [];
  const passed = [];

  for (const guard of guards) {
    const target = guard.file || guard.dir;
    let actual = null;
    let error = null;

    if (guard.kind === 'regex-in-dir') {
      const files = listFiles(root, guard.dir, { fileFilter: guard.fileFilter });
      if (files === null) error = `directory ${guard.dir} is missing`;
      else actual = files.reduce((n, f) => n + countMatches(fs.readFileSync(f, 'utf8'), guard.pattern), 0);
    } else if (guard.kind === 'regex-in-file') {
      const abs = path.join(root, guard.file);
      if (!fs.existsSync(abs)) error = `file ${guard.file} is missing`;
      else actual = countMatches(fs.readFileSync(abs, 'utf8'), guard.pattern);
    } else if (guard.kind === 'file-count' || guard.kind === 'walk-count') {
      const files = listFiles(root, guard.dir, {
        fileFilter: guard.fileFilter,
        recursive: guard.kind === 'walk-count',
      });
      if (files === null) error = `directory ${guard.dir} is missing`;
      else actual = files.length;
    } else if (guard.kind === 'tracked-file-count') {
      const listed = gitLsFiles(guard.dir);
      if (listed === null) {
        passed.push({ id: guard.id, file: target, note: 'git unavailable, measure skipped' });
        continue;
      }
      actual = listed.length;
    } else {
      error = `unknown guard kind ${guard.kind}`;
    }

    if (error) {
      failures.push({ id: guard.id, file: target, reason: error });
      continue;
    }

    const breached = guard.min !== undefined ? actual < guard.min : actual > guard.max;
    if (breached) {
      const bound = guard.min !== undefined ? `min ${guard.min}` : `max ${guard.max}`;
      failures.push({
        id: guard.id,
        file: target,
        reason: `count ${actual} violates ${bound} (${guard.why})`,
      });
    } else {
      passed.push({ id: guard.id, file: target, note: `count ${actual}` });
    }
  }

  return { passed, failures };
}

function main() {
  const contracts = evaluateContracts();
  const surface = evaluateSurfaceGuards();
  const result = {
    passed: [...contracts.passed, ...surface.passed],
    failures: [...contracts.failures, ...surface.failures],
  };
  const json = process.argv.includes('--json');

  if (json) {
    process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
  } else {
    for (const item of result.passed) {
      console.log(`PASS ${item.id} (${item.file})`);
    }
    for (const item of result.failures) {
      console.error(`FAIL ${item.id} (${item.file}): ${item.reason}`);
    }
    console.log(`Engineering contracts: ${result.passed.length} passed, ${result.failures.length} failed`);
  }

  process.exitCode = result.failures.length ? 1 : 0;
}

if (require.main === module) {
  main();
}

module.exports = { CONTRACTS, SURFACE_GUARDS, evaluateContracts, evaluateSurfaceGuards };
