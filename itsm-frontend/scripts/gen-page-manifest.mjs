#!/usr/bin/env node
/**
 * H0-02 Page Manifest + Doc Ledger 生成器（方案 §5.2 / §5.3）。
 *
 * 数据源：
 *  1) 前端页面注册表（src/lib/ai/page-context/registry.ts）与路由表（src/routes/route-paths.ts）
 *     —— 经 esbuild 现场打包为 CJS 后加载（TS 源无需预编译，也不依赖 ts-node 配置）；
 *  2) 帮助文档 docs/help/**\/*.md 的 frontmatter（page_keys / last_reviewed / audience / version / owner）。
 *
 * 产物：
 *  - itsm-frontend/src/generated/page-manifest.json（页面清单：路由/入口/场景/文档/状态）
 *  - docs/help/_ledger.json（覆盖率账本：ok / missing / stale / orphan + coverage）
 *
 * 门禁（ratchet，对齐根目录 test-coverage-guard.json 的"只减不增"思路）：
 *  - `--check`（CI 默认）：missing/stale/orphan 数量不得高于 docs/help/_coverage-baseline.json；
 *  - `--write-baseline`：更新基线（与账本一并提交）。
 *
 * 用法：
 *  - npm run docs:manifest                 # 生成产物并打印摘要
 *  - npm run docs:manifest -- --write-baseline   # 首次/显式放宽基线（需评审）
 *  - npm run docs:ledger:check             # CI 门禁（ratchet）
 */
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const FRONTEND_ROOT = path.resolve(__dirname, '..');
const REPO_ROOT = path.resolve(FRONTEND_ROOT, '..');
const HELP_DIR = path.join(REPO_ROOT, 'docs', 'help');
const LEDGER_PATH = path.join(HELP_DIR, '_ledger.json');
const BASELINE_PATH = path.join(HELP_DIR, '_coverage-baseline.json');
const MANIFEST_PATH = path.join(FRONTEND_ROOT, 'src', 'generated', 'page-manifest.json');
// 后端内嵌副本（go:embed）：Help Agent 工具与 /ai/help/* 端点零文件系统依赖。
const BACKEND_HELP_DIR = path.join(REPO_ROOT, 'itsm-backend', 'internal', 'helpindex');
// D1-01：助手上下文注册表内嵌副本（/ai/assistant/context 的过滤数据源）。
const BACKEND_ASSISTANT_DIR = path.join(REPO_ROOT, 'itsm-backend', 'internal', 'assistantcontext');
const FRESHNESS_DAYS = 90;
const DOC_TEXT_MAX_CHARS = 20_000;

const args = new Set(process.argv.slice(2));
const CHECK = args.has('--check');
const WRITE_BASELINE = args.has('--write-baseline');

/** I0-02：注册表按 tasks/questions 分组；生成物统一扁平化为 suggestions（任务卡在前）。 */
const flattenPageActions = page => [
  ...(page.tasks ?? []).map(action => ({ ...action, kind: 'task' })),
  ...(page.questions ?? []).map(action => ({ ...action, kind: 'question' })),
];

/** 打包并加载 TS 数据源（registry + route-paths 均为纯数据模块）。 */
async function loadPageSources() {
  const esbuild = require('esbuild');
  const result = await esbuild.build({
    stdin: {
      contents: [
        "export { PAGE_ASSISTANT_REGISTRY } from './src/lib/ai/page-context/registry';",
        "export { ROUTE_PATHS } from './src/routes/route-paths';",
      ].join('\n'),
      resolveDir: FRONTEND_ROOT,
      sourcefile: 'page-sources-entry.ts',
      loader: 'ts',
    },
    bundle: true,
    write: false,
    platform: 'node',
    format: 'cjs',
    logLevel: 'silent',
  });
  const tmpDir = path.join(FRONTEND_ROOT, 'node_modules', '.tmp');
  fs.mkdirSync(tmpDir, { recursive: true });
  const tmpFile = path.join(tmpDir, 'page-sources.cjs');
  fs.writeFileSync(tmpFile, result.outputFiles[0].text, 'utf8');
  const mod = require(tmpFile);
  return {
    registry: mod.PAGE_ASSISTANT_REGISTRY ?? [],
    routePaths: mod.ROUTE_PATHS ?? new Set(),
  };
}

/** 解析 frontmatter（受控子集：`key: value` 与行内/块级列表）。 */
export function parseFrontmatter(raw) {
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(raw);
  if (!match) return { data: {}, body: raw };
  const data = {};
  const lines = match[1].split(/\r?\n/);
  let listKey = null;
  for (const line of lines) {
    if (!line.trim() || line.trim().startsWith('#')) continue;
    const listItem = /^\s*-\s+(.+)$/.exec(line);
    if (listItem && listKey) {
      data[listKey].push(stripQuotes(listItem[1].trim()));
      continue;
    }
    const kv = /^([A-Za-z_]+):\s*(.*)$/.exec(line.trim());
    if (!kv) continue;
    const [, key, rawValue] = kv;
    const value = rawValue.trim();
    if (value === '') {
      data[key] = [];
      listKey = key;
      continue;
    }
    listKey = null;
    if (value.startsWith('[') && value.endsWith(']')) {
      data[key] = value
        .slice(1, -1)
        .split(',')
        .map(item => stripQuotes(item.trim()))
        .filter(Boolean);
    } else {
      data[key] = stripQuotes(value);
    }
  }
  return { data, body: raw.slice(match[0].length) };
}

function stripQuotes(value) {
  return value.replace(/^['"]|['"]$/g, '');
}

function walkMarkdown(dir) {
  if (!fs.existsSync(dir)) return [];
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...walkMarkdown(full));
      continue;
    }
    if (!entry.name.endsWith('.md')) continue;
    if (entry.name.startsWith('_')) continue; // _ledger.json 等生成物/内部文件
    out.push(full);
  }
  return out.sort();
}

function isFresh(lastReviewed) {
  if (typeof lastReviewed !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(lastReviewed)) return false;
  const reviewed = Date.parse(`${lastReviewed}T00:00:00Z`);
  if (Number.isNaN(reviewed)) return false;
  const ageDays = (Date.now() - reviewed) / 86_400_000;
  return ageDays <= FRESHNESS_DAYS;
}

async function main() {
  const { registry } = await loadPageSources();
  const registryKeys = new Set(registry.map(page => page.pageKey));

  const docs = walkMarkdown(HELP_DIR).map(file => {
    const raw = fs.readFileSync(file, 'utf8');
    const { data, body } = parseFrontmatter(raw);
    const pageKeys = Array.isArray(data.page_keys)
      ? data.page_keys
      : typeof data.page_keys === 'string' && data.page_keys
        ? [data.page_keys]
        : [];
    const titleMatch = /^#\s+(.+)$/m.exec(body);
    return {
      file,
      relative: path.relative(REPO_ROOT, file).replace(/\\/g, '/'),
      pageKeys,
      lastReviewed: data.last_reviewed,
      fresh: isFresh(data.last_reviewed),
      audience: data.audience,
      owner: data.owner,
      version: data.version,
      title: titleMatch ? titleMatch[1].trim() : path.basename(file, '.md'),
      text: body.trim().slice(0, DOC_TEXT_MAX_CHARS),
    };
  });

  const pages = {};
  const manifestPages = [];
  let okCount = 0;
  let missingCount = 0;
  let staleCount = 0;

  for (const page of registry) {
    const linked = docs.filter(doc => doc.pageKeys.includes(page.pageKey));
    let status = 'missing';
    if (linked.length > 0) {
      status = linked.some(doc => doc.fresh) ? 'ok' : 'stale';
    }
    if (status === 'ok') okCount += 1;
    else if (status === 'missing') missingCount += 1;
    else staleCount += 1;

    pages[page.pageKey] = {
      title: page.title,
      pattern: page.pattern,
      entrypoint: page.entrypoint ?? 'chat',
      scene: page.scene,
      docs: linked.map(doc => doc.relative),
      status,
    };
    manifestPages.push({
      pageKey: page.pageKey,
      pattern: page.pattern,
      title: page.title,
      entrypoint: page.entrypoint ?? 'chat',
      scene: page.scene ?? null,
      docs: linked.map(doc => doc.relative),
      status,
      suggestions: flattenPageActions(page).map(action => ({
        id: action.id,
        kind: action.kind,
        readOnly: action.readOnly,
      })),
    });
  }

  // orphan：文档声明的 page_key 不在注册表内（漂移/改名未同步）。
  const orphanDocs = docs
    .filter(doc => doc.pageKeys.some(key => !registryKeys.has(key)))
    .map(doc => ({ doc: doc.relative, unknownKeys: doc.pageKeys.filter(key => !registryKeys.has(key)) }));

  const total = registry.length;
  const coverage = total === 0 ? 0 : Number((okCount / total).toFixed(4));
  const generatedAt = new Date().toISOString();

  const ledger = {
    generatedAt,
    coverage,
    totals: {
      pages: total,
      ok: okCount,
      missing: missingCount,
      stale: staleCount,
      orphan: orphanDocs.length,
    },
    pages,
    orphanDocs,
  };

  const manifest = {
    generatedAt,
    note: 'H0-02 生成物：由 itsm-frontend/scripts/gen-page-manifest.mjs 从页面注册表 + 帮助文档生成，禁止手改。',
    pages: manifestPages,
  };

  // D1-01：助手上下文注册表（含 label/prompt 全文与可选 tools 依赖声明）。
  const assistantRegistry = {
    generatedAt,
    note: 'D1-01 生成物：由 itsm-frontend/scripts/gen-page-manifest.mjs 从页面注册表生成，禁止手改。',
    pages: registry.map(page => ({
      pageKey: page.pageKey,
      pattern: page.pattern,
      title: page.title,
      scene: page.scene ?? null,
      entrypoint: page.entrypoint ?? 'chat',
      suggestions: flattenPageActions(page).map(action => ({
        id: action.id,
        kind: action.kind,
        label: action.label,
        prompt: action.prompt,
        readOnly: action.readOnly,
        ...(Array.isArray(action.tools) && action.tools.length > 0 ? { tools: action.tools } : {}),
      })),
    })),
  };

  fs.mkdirSync(path.dirname(MANIFEST_PATH), { recursive: true });
  fs.writeFileSync(MANIFEST_PATH, `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');
  fs.writeFileSync(LEDGER_PATH, `${JSON.stringify(ledger, null, 2)}\n`, 'utf8');

  // H0-03：后端内嵌副本（help-index 供检索/阅读；manifest+ledger 供 gaps/覆盖状态）。
  const helpIndex = {
    generatedAt,
    docs: docs
      .filter(doc => doc.pageKeys.length > 0)
      .map(doc => ({
        path: doc.relative,
        title: doc.title,
        pageKeys: doc.pageKeys,
        text: doc.text,
      })),
  };
  fs.mkdirSync(BACKEND_HELP_DIR, { recursive: true });
  fs.writeFileSync(
    path.join(BACKEND_HELP_DIR, 'help-index.json'),
    `${JSON.stringify(helpIndex, null, 2)}\n`,
    'utf8'
  );
  fs.writeFileSync(
    path.join(BACKEND_HELP_DIR, 'page-manifest.json'),
    `${JSON.stringify(manifest, null, 2)}\n`,
    'utf8'
  );
  fs.writeFileSync(
    path.join(BACKEND_HELP_DIR, 'doc-ledger.json'),
    `${JSON.stringify(ledger, null, 2)}\n`,
    'utf8'
  );

  fs.mkdirSync(BACKEND_ASSISTANT_DIR, { recursive: true });
  fs.writeFileSync(
    path.join(BACKEND_ASSISTANT_DIR, 'assistant-registry.json'),
    `${JSON.stringify(assistantRegistry, null, 2)}\n`,
    'utf8'
  );

  console.log(
    `[docs-ledger] pages=${total} ok=${okCount} missing=${missingCount} stale=${staleCount} orphan=${orphanDocs.length} coverage=${(coverage * 100).toFixed(1)}%`
  );
  console.log(`[docs-ledger] wrote ${path.relative(REPO_ROOT, MANIFEST_PATH).replace(/\\/g, '/')}`);
  console.log(`[docs-ledger] wrote ${path.relative(REPO_ROOT, LEDGER_PATH).replace(/\\/g, '/')}`);
  console.log(
    `[docs-ledger] wrote ${path.relative(REPO_ROOT, BACKEND_HELP_DIR).replace(/\\/g, '/')}/{help-index,page-manifest,doc-ledger}.json + ${path.relative(REPO_ROOT, BACKEND_ASSISTANT_DIR).replace(/\\/g, '/')}/assistant-registry.json`
  );

  if (WRITE_BASELINE) {
    const baseline = {
      updatedAt: generatedAt,
      note: 'ratchet 基线：missing/stale/orphan 不得高于此值（--write-baseline 显式放宽需评审）。',
      missing: missingCount,
      stale: staleCount,
      orphan: orphanDocs.length,
      coverage,
    };
    fs.writeFileSync(BASELINE_PATH, `${JSON.stringify(baseline, null, 2)}\n`, 'utf8');
    console.log(`[docs-ledger] baseline updated ${path.relative(REPO_ROOT, BASELINE_PATH).replace(/\\/g, '/')}`);
    return;
  }

  if (!CHECK) return;

  if (!fs.existsSync(BASELINE_PATH)) {
    console.error(`[docs-ledger] FAIL: 缺少基线 ${path.relative(REPO_ROOT, BASELINE_PATH).replace(/\\/g, '/')}（首次请运行 --write-baseline 并提交）`);
    process.exit(1);
  }
  const baseline = JSON.parse(fs.readFileSync(BASELINE_PATH, 'utf8'));
  const regressions = [];
  if (missingCount > (baseline.missing ?? 0)) regressions.push(`missing ${baseline.missing} → ${missingCount}`);
  if (staleCount > (baseline.stale ?? 0)) regressions.push(`stale ${baseline.stale} → ${staleCount}`);
  if (orphanDocs.length > (baseline.orphan ?? 0)) regressions.push(`orphan ${baseline.orphan} → ${orphanDocs.length}`);

  if (regressions.length > 0) {
    console.error('[docs-ledger] FAIL: 文档覆盖率回退（ratchet）：');
    for (const line of regressions) console.error(`  - ${line}`);
    console.error('  修复：补齐/更新 docs/help 下对应当前页面的文档；确需放宽请先评审再 --write-baseline。');
    process.exit(1);
  }

  // D1-01：内嵌助手注册表漂移检查（页面注册表是唯一真源；改动必须重跑生成并提交）。
  const embeddedRegistryPath = path.join(BACKEND_ASSISTANT_DIR, 'assistant-registry.json');
  if (!fs.existsSync(embeddedRegistryPath)) {
    console.error(
      `[docs-ledger] FAIL: 缺少内嵌助手注册表 ${path.relative(REPO_ROOT, embeddedRegistryPath).replace(/\\/g, '/')}（运行 npm run docs:manifest 生成并提交）`
    );
    process.exit(1);
  }
  const embedded = JSON.parse(fs.readFileSync(embeddedRegistryPath, 'utf8'));
  if (JSON.stringify(embedded.pages) !== JSON.stringify(assistantRegistry.pages)) {
    console.error('[docs-ledger] FAIL: 内嵌助手注册表与页面注册表不同步（运行 npm run docs:manifest 重新生成并提交）');
    process.exit(1);
  }
  console.log('[docs-ledger] assistant registry in sync (D1-01)');
  console.log('[docs-ledger] OK（ratchet 未回退）');
}

main().catch(err => {
  console.error('[docs-ledger] ERROR', err);
  process.exit(1);
});