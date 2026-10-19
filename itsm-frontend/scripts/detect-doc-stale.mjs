#!/usr/bin/env node
/**
 * H1-03 文档 stale 检测（代码/页面定义变更 → 更新提议）。
 *
 * 口径（方案 §5.6 触发源 4）：CI/夜批对比页面清单（`src/generated/page-manifest.json`，H0-02 生成物）
 * 的基线 vs 当前版本，识别「页面定义变化 / 新增页面 / 删除页面」，据此生成文档更新提议：
 *   - 变更页面 → 有文档 = `review_update`（附变化字段）；无文档 = `create_doc`；
 *   - 新增页面 → 无文档 = `create_doc`；已有文档 = `review_update`；
 *   - 删除页面 → 仍有文档引用 = `remove_reference`。
 * 产物：`docs/help/_stale-proposals.json`（机器可读）+ 同名 `.md`（Issue/Step Summary），
 * 供 `itsm-agent/prompts/docs-maintenance.md` 的 PR 通道消费。
 *
 * 用法：
 *   node scripts/detect-doc-stale.mjs                                  # 默认 --base origin/main
 *   node scripts/detect-doc-stale.mjs --base HEAD --out ../docs/help/_stale-proposals.json
 *   node scripts/detect-doc-stale.mjs --base-manifest <manifest.json>  # 离线/测试：直接给定基线清单
 *   node scripts/detect-doc-stale.mjs --head-manifest <manifest.json>  # 离线/测试：给定当前清单（默认取仓内生成物）
 *   node scripts/detect-doc-stale.mjs --fail                           # 有提议时退出码 2（可选门禁）
 * 退出码：默认 0（夜批不因提议失败）；--fail 且有提议 → 2；参数/环境错误 → 1。
 */
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';

import {
  PAGE_MANIFEST_REL,
  REPO_ROOT,
  loadHelpDocs,
  parsePageManifest,
} from './lib/help-docs.mjs';
import { buildProposals, diffPages, renderMarkdown, summarize } from './lib/doc-stale-core.mjs';

function argValue(args, name, fallback) {
  const idx = args.indexOf(name);
  if (idx < 0) return fallback;
  return args[idx + 1] ?? fallback;
}

function readHeadPages(headManifestPath) {
  const file = headManifestPath
    ? path.resolve(process.cwd(), headManifestPath)
    : path.join(REPO_ROOT, PAGE_MANIFEST_REL);
  if (!fs.existsSync(file)) {
    throw new Error(`缺少页面清单 ${PAGE_MANIFEST_REL}（先运行 npm run docs:manifest）`);
  }
  return parsePageManifest(fs.readFileSync(file, 'utf8')).pages;
}

function readBasePages(base, baseManifestPath) {
  if (baseManifestPath) {
    const file = path.resolve(process.cwd(), baseManifestPath);
    return { pages: parsePageManifest(fs.readFileSync(file, 'utf8')).pages, missing: false };
  }
  try {
    const raw = execFileSync('git', ['show', `${base}:${PAGE_MANIFEST_REL}`], {
      cwd: REPO_ROOT,
      encoding: 'utf8',
      // 基线无清单时 git 会写 stderr（fatal: path ... does not exist）——属预期降级，不泄漏到控制台。
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    return { pages: parsePageManifest(raw).pages, missing: false };
  } catch {
    // 基线尚无页面清单（首次引入生成物等）→ 按空基线处理并在报告中标注。
    return { pages: [], missing: true };
  }
}

function buildDocsByPageKey() {
  const docsByPageKey = new Map();
  for (const doc of loadHelpDocs()) {
    for (const key of doc.pageKeys) {
      if (!docsByPageKey.has(key)) docsByPageKey.set(key, []);
      docsByPageKey.get(key).push(doc.relative);
    }
  }
  return docsByPageKey;
}

function main() {
  const args = process.argv.slice(2);
  const base = argValue(args, '--base', 'origin/main');
  const baseManifestPath = argValue(args, '--base-manifest', '');
  const headManifestPath = argValue(args, '--head-manifest', '');
  const out = path.resolve(
    argValue(args, '--out', path.join(REPO_ROOT, 'docs', 'help', '_stale-proposals.json'))
  );
  const failOnProposals = args.includes('--fail');

  const headPages = readHeadPages(headManifestPath);
  const baseInfo = readBasePages(base, baseManifestPath);
  const diff = diffPages(baseInfo.pages, headPages);
  const proposals = buildProposals(diff, buildDocsByPageKey());
  const report = {
    generatedAt: new Date().toISOString(),
    base: baseManifestPath || `git:${base}`,
    headManifest: headManifestPath || PAGE_MANIFEST_REL,
    baseManifestMissing: baseInfo.missing,
    headCount: headPages.length,
    diff,
    proposals,
    summary: summarize(diff, proposals),
  };

  fs.mkdirSync(path.dirname(out), { recursive: true });
  fs.writeFileSync(out, `${JSON.stringify(report, null, 2)}\n`, 'utf8');
  const markdown = renderMarkdown(report);
  fs.writeFileSync(out.replace(/\.json$/, '.md'), markdown, 'utf8');

  const actions = Object.entries(report.summary.actions)
    .map(([key, value]) => `${key}:${value}`)
    .join(' ');
  console.log(
    `[docs-stale] base=${report.base} pages=${headPages.length} changed=${report.summary.changed} ` +
      `added=${report.summary.added} removed=${report.summary.removed} proposals=${report.summary.proposals}（${actions}）`
  );
  console.log(`[docs-stale] wrote ${path.relative(REPO_ROOT, out).replace(/\\/g, '/')}`);

  if (process.env.GITHUB_STEP_SUMMARY) {
    try {
      fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${markdown}\n`, 'utf8');
    } catch {
      /* Step Summary 为 best-effort，不阻塞检测 */
    }
  }

  if (failOnProposals && proposals.length > 0) {
    process.exit(2);
  }
}

try {
  main();
} catch (err) {
  console.error(`[docs-stale] ERROR ${err.message}`);
  process.exit(1);
}