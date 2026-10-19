#!/usr/bin/env node
/**
 * P2 L1 自动合并执行器（方案 §5.7「校验全绿可自动合并/自动发布（策略开关，默认关，先观察）」）。
 *
 * 安全口径（默认最保守）：
 *  1. **默认 dry-run**：只列出 L1 机械修正候选（不写文件）；
 *  2. **策略开关默认关**：`--apply` 还需 `DOCS_AUTO_MERGE=1`，否则自动降级为 dry-run；
 *  3. **校验门禁**：应用前 `validate-help-docs.mjs` 必须全绿；应用后复跑校验，失败则**整体回滚**并 exit 1；
 *  4. **只允许 L1**：frontmatter 补全（可推导 version）/ 相对链接修正 / 行尾空白；L2/L3 与未知类型一律跳过（交人工）；
 *  5. **审计留痕**：实际写入追加 `docs/help/_automerge-audit.jsonl`（时间/触发者/模式/校验前后/明细/回滚）；
 *  6. 文件数上限（默认 20，`--max-files` 覆盖）。
 *
 * 用法：
 *   node scripts/apply-doc-fixes.mjs                       # dry-run（默认）
 *   node scripts/apply-doc-fixes.mjs --report ../docs/help/_automerge-report.json
 *   DOCS_AUTO_MERGE=1 node scripts/apply-doc-fixes.mjs --apply
 */
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

import {
  addFrontmatterVersion,
  buildAutoMergeAudit,
  classifyDocFix,
  renderAuditLine,
  rewriteRelativeLinks,
  selectAutoMergeCandidates,
  normalizeEofNewline,
  summarizeCandidates,
} from './lib/doc-merge-core.mjs';
import { HELP_DIR, REPO_ROOT, parseFrontmatter, walkMarkdown } from './lib/help-docs.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const FRONTEND_ROOT = path.resolve(__dirname, '..');
const AUDIT_FILE = path.join(HELP_DIR, '_automerge-audit.jsonl');
const VALIDATOR = path.join(__dirname, 'validate-help-docs.mjs');

const toPosix = value => value.split(path.sep).join('/');

function parseArgs(argv) {
  const options = { apply: false, report: null, root: HELP_DIR, maxFiles: 20 };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === '--apply') options.apply = true;
    else if (arg === '--report') options.report = argv[++index] ?? null;
    else if (arg === '--root') options.root = path.resolve(REPO_ROOT, argv[++index] ?? HELP_DIR);
    else if (arg === '--max-files') options.maxFiles = Number.parseInt(argv[++index] ?? '20', 10);
  }
  return options;
}

/** 推导文档 version（代理口径：前端包 major.minor；缺省/非法 → null 表示不机械补）。 */
function derivedDocsVersion() {
  try {
    const pkg = JSON.parse(fs.readFileSync(path.join(FRONTEND_ROOT, 'package.json'), 'utf8'));
    const match = /^(\d+)\.(\d+)/.exec(pkg.version ?? '');
    return match ? `${match[1]}.${match[2]}` : null;
  } catch {
    return null;
  }
}

/** 运行校验器（返回 { ok, output }；不抛出）。 */
function runValidator() {
  const result = spawnSync(process.execPath, [VALIDATOR], {
    cwd: FRONTEND_ROOT,
    encoding: 'utf8',
  });
  const output = `${result.stdout ?? ''}${result.stderr ?? ''}`.trim();
  return { ok: result.status === 0, output, status: result.status ?? -1 };
}

/** basename → 相对仓库路径（仅唯一命中；歧义不入索引）。 */
function buildDocsIndex(docs) {
  const seen = new Map();
  const ambiguous = new Set();
  for (const file of docs) {
    const basename = path.basename(file);
    if (seen.has(basename)) {
      ambiguous.add(basename);
      continue;
    }
    seen.set(basename, toPosix(path.relative(REPO_ROOT, file)));
  }
  for (const basename of ambiguous) seen.delete(basename);
  return seen;
}

/** 相对链接存在性（相对当前文件解析；仓库绝对路径已在 core 中跳过）。 */
function linkExists(fromFile, target) {
  try {
    const resolved = path.resolve(path.dirname(fromFile), decodeURIComponent(target));
    return fs.existsSync(resolved);
  } catch {
    return false;
  }
}

function linkRelative(fromFile, hitRelative) {
  const targetAbs = path.join(REPO_ROOT, hitRelative);
  return path.relative(path.dirname(fromFile), targetAbs);
}

/** 为单篇文档收集 L1 机械修正候选（apply: content => nextContent 幂等变换）。 */
function collectFixes(file, raw, { docsIndex, version }) {
  const relative = toPosix(path.relative(REPO_ROOT, file));
  const fixes = [];

  if (version) {
    const camel = addFrontmatterVersion(raw, version);
    if (camel.changed) {
      fixes.push({
        id: `${relative}#frontmatter_version_add`,
        kind: 'frontmatter_version_add',
        level: classifyDocFix('frontmatter_version_add'),
        relative,
        file,
        detail: `version → ${version}`,
        apply: content => addFrontmatterVersion(content, version).content,
      });
    }
  }

  const links = rewriteRelativeLinks(raw, {
    fromFile: file,
    docsIndex,
    exists: linkExists,
    relative: linkRelative,
    toPosix: value => value.split('\\').join('/'),
  });
  if (links.changes.length > 0) {
    fixes.push({
      id: `${relative}#link_normalize`,
      kind: 'link_normalize',
      level: classifyDocFix('link_normalize'),
      relative,
      file,
      detail: links.changes.map(change => `${change.from} → ${change.to}`).join('; '),
      apply: content =>
        rewriteRelativeLinks(content, {
          fromFile: file,
          docsIndex,
          exists: linkExists,
          relative: linkRelative,
          toPosix: value => value.split('\\').join('/'),
        }).content,
    });
  }

  const eof = normalizeEofNewline(raw);
  if (eof !== raw) {
    fixes.push({
      id: `${relative}#eof_newline`,
      kind: 'eof_newline',
      level: classifyDocFix('eof_newline'),
      relative,
      file,
      detail: '补齐文件结尾换行（不动行内字节）',
      apply: normalizeEofNewline,
    });
  }

  return fixes;
}

function writeReport(reportPath, report) {
  if (!reportPath) return;
  const absolute = path.isAbsolute(reportPath) ? reportPath : path.resolve(FRONTEND_ROOT, reportPath);
  fs.mkdirSync(path.dirname(absolute), { recursive: true });
  fs.writeFileSync(absolute, `${JSON.stringify(report, null, 2)}\n`, 'utf8');
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const policyEnabled = process.env.DOCS_AUTO_MERGE === '1';
  const applyRequested = options.apply;
  const mode = applyRequested && policyEnabled ? 'apply' : 'dry-run';

  if (applyRequested && !policyEnabled) {
    console.warn('[docs-merge] 策略开关关闭（DOCS_AUTO_MERGE!=1）→ 自动降级为 dry-run（§5.7 默认关，先观察）。');
  }

  const docs = walkMarkdown(options.root);
  const docsIndex = buildDocsIndex(docs);
  const version = derivedDocsVersion();
  const fixes = docs.flatMap(file => {
    const raw = fs.readFileSync(file, 'utf8');
    return collectFixes(file, raw, { docsIndex, version });
  });

  const validationBefore = runValidator();
  const { candidates, skipped } = selectAutoMergeCandidates(fixes, {
    enabled: mode === 'apply',
    validationOk: validationBefore.ok,
    maxFiles: options.maxFiles,
  });

  const summary = summarizeCandidates(candidates);
  let applied = [];
  let rolledBack = false;
  let validationAfter = null;
  let note = '';

  if (mode === 'apply' && validationBefore.ok) {
    const originals = new Map();
    try {
      for (const fix of candidates) {
        if (!originals.has(fix.file)) {
          originals.set(fix.file, fs.readFileSync(fix.file, 'utf8'));
        }
        const current = fs.readFileSync(fix.file, 'utf8');
        const next = fix.apply(current);
        if (next !== current) {
          fs.writeFileSync(fix.file, next, 'utf8');
          applied.push({ relative: fix.relative, kind: fix.kind, detail: fix.detail ?? null });
        }
      }
      validationAfter = runValidator();
      if (!validationAfter.ok) {
        for (const [file, content] of originals) {
          fs.writeFileSync(file, content, 'utf8');
        }
        rolledBack = true;
        note = '应用后校验未通过 → 已回滚全部变更';
        console.error(`[docs-merge] FAIL: 应用后校验未通过（已回滚）：\n${validationAfter.output}`);
      }
    } catch (err) {
      for (const [file, content] of originals) {
        fs.writeFileSync(file, content, 'utf8');
      }
      rolledBack = true;
      note = `应用异常 → 已回滚：${err.message}`;
      console.error(`[docs-merge] ERROR: ${err.message}`);
    }
  } else if (mode === 'apply') {
    // §5.7：校验全绿是自动合并的前置条件——校验未过时不写入任何文件（fail-closed）。
    note = '校验未通过 → 未应用任何变更（fail-closed）';
    console.error('[docs-merge] FAIL: 校验未通过，按 §5.7 不执行自动合并（未写入任何文件）。');
  }

  const audit = buildAutoMergeAudit({
    at: new Date().toISOString(),
    actor: process.env.GITHUB_ACTOR || process.env.CI ? `${process.env.GITHUB_ACTOR ?? 'ci'}${process.env.GITHUB_RUN_ID ? `#${process.env.GITHUB_RUN_ID}` : ''}` : 'local',
    mode,
    policyEnabled,
    validationBefore: validationBefore.ok ? 'green' : `failed(${validationBefore.status})`,
    validationAfter: validationAfter ? (validationAfter.ok ? 'green' : 'failed') : null,
    applied,
    skipped,
    rolledBack,
    note,
  });

  if (mode === 'apply' && (applied.length > 0 || rolledBack)) {
    fs.appendFileSync(AUDIT_FILE, renderAuditLine(audit), 'utf8');
  }

  const report = {
    generatedAt: audit.at,
    mode,
    policyEnabled,
    validationBefore: audit.validationBefore,
    validationAfter: audit.validationAfter,
    summary,
    candidates: candidates.map(fix => ({
      relative: fix.relative,
      kind: fix.kind,
      detail: fix.detail ?? null,
    })),
    skipped,
    applied,
    rolledBack,
  };
  writeReport(options.report, report);

  console.log(
    `[docs-merge] ${mode}：L1 候选 ${summary.total}（文件 ${summary.files}）` +
      `，按类型 ${JSON.stringify(summary.byKind)}；校验=${audit.validationBefore}` +
      `${mode === 'apply' ? `，已应用 ${applied.length}${rolledBack ? '（已回滚）' : ''}` : ''}`
  );
  for (const candidate of report.candidates) {
    console.log(`  - ${candidate.relative} [${candidate.kind}] ${candidate.detail ?? ''}`);
  }

  if (mode === 'apply' && (rolledBack || (validationAfter && !validationAfter.ok))) {
    process.exit(1);
  }
  if (mode === 'apply' && !validationBefore.ok) {
    process.exit(1);
  }
}

main().catch(err => {
  console.error('[docs-merge] ERROR', err);
  process.exit(1);
});