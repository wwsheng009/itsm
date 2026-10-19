#!/usr/bin/env node
/**
 * H1-02 帮助文档校验器（结构 / frontmatter / page_keys / 链接 / 敏感信息）。
 *
 * 方案 §5.5「自动校验（结构 / page_keys / 链接 / 引用事实存在于 manifest / 敏感信息 / mkdocs 构建）」
 * 与 §6「CI Job：help-coverage ratchet + 校验器」的在仓实现。
 *
 * 错误级（任一项 → exit 1）：
 *  1. frontmatter 缺失或必填键为空：page_keys / audience / version / last_reviewed / owner；
 *  2. 取值域：audience ∈ {internal, customer, admin, all, end_user}；version 形如 1.8 / 1.8.2；
 *     last_reviewed = YYYY-MM-DD 且不在未来；
 *  3. 结构：正文含一级标题（# ），正文非空；
 *  4. 事实引用：page_keys 必须存在于前端页面注册表（与 Page Manifest 同源；未知键 = orphan）；
 *  5. 链接：相对 Markdown 链接/图片必须解析到仓库内真实文件；
 *  6. 敏感信息：私钥块 / AWS Access Key / sk- 令牌 / 明文口令赋值。
 *
 * 警告级（不导致失败，仅供评审提示）：frontmatter 未知键、正文过短（<200 字符）。
 *
 * 用法：node scripts/validate-help-docs.mjs
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

const REQUIRED_KEYS = ['page_keys', 'audience', 'version', 'last_reviewed', 'owner'];
const KNOWN_KEYS = new Set([...REQUIRED_KEYS, 'title', 'summary']);
const AUDIENCES = new Set(['internal', 'customer', 'admin', 'all', 'end_user']);
const VERSION_RE = /^\d+(\.\d+){0,3}$/;
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const MIN_BODY_CHARS = 200;

const SENSITIVE_PATTERNS = [
  { name: '私钥块', re: /-----BEGIN [A-Z ]*PRIVATE KEY-----/ },
  { name: 'AWS Access Key', re: /\bAKIA[0-9A-Z]{16}\b/ },
  { name: 'sk- 令牌', re: /\bsk-[A-Za-z0-9]{20,}\b/ },
  { name: '明文口令赋值', re: /(password|passwd|secret|api[_-]?key)\s*[:=]\s*["'][^"'\s]{6,}["']/i },
];

const errors = [];
const warnings = [];

function fail(file, message) {
  errors.push(`${path.relative(REPO_ROOT, file).replace(/\\/g, '/')}: ${message}`);
}

function warn(file, message) {
  warnings.push(`${path.relative(REPO_ROOT, file).replace(/\\/g, '/')}: ${message}`);
}

/** 打包并加载前端页面注册表（与 gen-page-manifest.mjs 同源，避免第二份真源）。 */
async function loadRegistryPageKeys() {
  const esbuild = require('esbuild');
  const result = await esbuild.build({
    stdin: {
      contents: "export { PAGE_ASSISTANT_REGISTRY } from './src/lib/ai/page-context/registry';",
      resolveDir: FRONTEND_ROOT,
      sourcefile: 'validate-help-docs-entry.ts',
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
  const tmpFile = path.join(tmpDir, 'validate-help-docs.cjs');
  fs.writeFileSync(tmpFile, result.outputFiles[0].text, 'utf8');
  const mod = require(tmpFile);
  return new Set((mod.PAGE_ASSISTANT_REGISTRY ?? []).map(page => page.pageKey));
}

function parseFrontmatter(raw) {
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(raw);
  if (!match) return { data: null, body: raw };
  const data = {};
  let listKey = null;
  for (const line of match[1].split(/\r?\n/)) {
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
    if (entry.name.startsWith('_')) continue; // 生成物/内部文件
    out.push(full);
  }
  return out.sort();
}

function checkLinks(file, body) {
  const linkRe = /!?\[[^\]]*\]\(([^)]+)\)/g;
  let match;
  while ((match = linkRe.exec(body)) !== null) {
    let target = match[1].trim();
    if (!target) continue;
    // 去掉 title 与锚点。
    target = target.split(/\s+/)[0].split('#')[0];
    if (!target) continue;
    if (/^(https?:|mailto:|tel:|data:)/i.test(target)) continue;

    const decoded = decodeURIComponent(target);
    const resolved = decoded.startsWith('/')
      ? path.join(REPO_ROOT, decoded.slice(1))
      : path.resolve(path.dirname(file), decoded);
    if (!fs.existsSync(resolved)) {
      fail(file, `断链：${target}（解析为 ${path.relative(REPO_ROOT, resolved).replace(/\\/g, '/')}）`);
    }
  }
}

function checkSensitive(file, body) {
  for (const pattern of SENSITIVE_PATTERNS) {
    if (pattern.re.test(body)) {
      fail(file, `疑似敏感信息（${pattern.name}）；请脱敏或改为占位符`);
    }
  }
}

async function main() {
  const registryKeys = await loadRegistryPageKeys();
  const docs = walkMarkdown(HELP_DIR);
  if (docs.length === 0) {
    console.error('[docs-validate] FAIL: docs/help 下没有任何文档');
    process.exit(1);
  }

  const today = new Date();
  today.setHours(23, 59, 59, 999);

  for (const file of docs) {
    const raw = fs.readFileSync(file, 'utf8');
    const { data, body } = parseFrontmatter(raw);
    if (!data) {
      fail(file, '缺少 frontmatter（--- 块）');
      continue;
    }

    for (const key of REQUIRED_KEYS) {
      const value = data[key];
      if (value === undefined || value === null) {
        fail(file, `frontmatter 必填键缺失：${key}`);
        continue;
      }
      // page_keys 允许空数组（通用文档，如帮助中心索引）；页面级覆盖缺口由 ratchet 门禁兜底。
      if (key === 'page_keys') continue;
      const empty = value === '' || (Array.isArray(value) && value.length === 0);
      if (empty) fail(file, `frontmatter 必填键为空：${key}`);
    }
    for (const key of Object.keys(data)) {
      if (!KNOWN_KEYS.has(key)) warn(file, `未知 frontmatter 键：${key}`);
    }

    const audience = data.audience;
    if (typeof audience === 'string' && audience && !AUDIENCES.has(audience)) {
      fail(file, `audience 取值非法：${audience}（允许 ${[...AUDIENCES].join('|')}）`);
    }
    const version = data.version;
    if (typeof version === 'string' && version && !VERSION_RE.test(version)) {
      fail(file, `version 取值非法：${version}（形如 1.8 / 1.8.2）`);
    }
    const reviewed = data.last_reviewed;
    if (typeof reviewed === 'string' && reviewed) {
      if (!DATE_RE.test(reviewed)) {
        fail(file, `last_reviewed 必须是 YYYY-MM-DD：${reviewed}`);
      } else if (Date.parse(`${reviewed}T00:00:00Z`) > today.getTime()) {
        fail(file, `last_reviewed 不得是未来日期：${reviewed}`);
      }
    }

    const pageKeys = Array.isArray(data.page_keys)
      ? data.page_keys
      : typeof data.page_keys === 'string' && data.page_keys
        ? [data.page_keys]
        : [];
    const seen = new Set();
    for (const key of pageKeys) {
      if (seen.has(key)) fail(file, `page_keys 重复：${key}`);
      seen.add(key);
      if (!registryKeys.has(key)) {
        fail(file, `page_keys 引用未注册页面（orphan）：${key}`);
      }
    }

    if (!/^#\s+\S+/m.test(body)) {
      fail(file, '正文缺少一级标题（# …）');
    }
    const trimmed = body.trim();
    if (!trimmed) {
      fail(file, '正文为空');
    } else if (trimmed.length < MIN_BODY_CHARS) {
      warn(file, `正文过短（${trimmed.length} < ${MIN_BODY_CHARS} 字符），请确认内容完整`);
    }

    checkLinks(file, body);
    checkSensitive(file, body);
  }

  if (warnings.length > 0) {
    console.warn(`[docs-validate] ${warnings.length} 条警告：`);
    for (const line of warnings) console.warn(`  - ${line}`);
  }
  if (errors.length > 0) {
    console.error(`[docs-validate] FAIL: ${errors.length} 个错误：`);
    for (const line of errors) console.error(`  - ${line}`);
    process.exit(1);
  }
  console.log(
    `[docs-validate] OK：${docs.length} 篇文档（frontmatter/结构/page_keys/链接/敏感信息），0 错误`
  );
}

main().catch(err => {
  console.error('[docs-validate] ERROR', err);
  process.exit(1);
});