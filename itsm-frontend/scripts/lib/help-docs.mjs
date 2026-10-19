/**
 * H1-02/H1-03 共享帮助文档工具：frontmatter 解析、文档遍历、注册表加载、页面清单解析。
 *
 * 说明：`gen-page-manifest.mjs`（H0-02）早于本模块且是生成器真源，保留其自身实现以稳定既有门禁；
 * 新脚本（校验器 / stale 检测 / 夜批任务）统一走本模块，避免第三份拷贝继续扩散。
 */
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const __dirname = path.dirname(fileURLToPath(import.meta.url));

export const FRONTEND_ROOT = path.resolve(__dirname, '..', '..');
export const REPO_ROOT = path.resolve(FRONTEND_ROOT, '..');
export const HELP_DIR = path.join(REPO_ROOT, 'docs', 'help');
export const PAGE_MANIFEST_REL = 'itsm-frontend/src/generated/page-manifest.json';

export function stripQuotes(value) {
  return value.replace(/^['"]|['"]$/g, '');
}

/** 解析 frontmatter（受控子集：`key: value` 与行内/块级列表）。无 frontmatter → data=null。 */
export function parseFrontmatter(raw) {
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

/** 递归遍历 Markdown（跳过 `_` 前缀的内部文件/生成物）。 */
export function walkMarkdown(dir) {
  if (!fs.existsSync(dir)) return [];
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...walkMarkdown(full));
      continue;
    }
    if (!entry.name.endsWith('.md')) continue;
    if (entry.name.startsWith('_')) continue;
    out.push(full);
  }
  return out.sort();
}

/** 加载帮助文档元数据（相对路径/page_keys/复核日期）。 */
export function loadHelpDocs(dir = HELP_DIR) {
  return walkMarkdown(dir).map(file => {
    const raw = fs.readFileSync(file, 'utf8');
    const { data, body } = parseFrontmatter(raw);
    const pageKeys = Array.isArray(data?.page_keys)
      ? data.page_keys
      : typeof data?.page_keys === 'string' && data.page_keys
        ? [data.page_keys]
        : [];
    const titleMatch = /^#\s+(.+)$/m.exec(body);
    return {
      file,
      relative: path.relative(REPO_ROOT, file).replace(/\\/g, '/'),
      pageKeys,
      lastReviewed: data?.last_reviewed,
      title: titleMatch ? titleMatch[1].trim() : path.basename(file, '.md'),
    };
  });
}

/** 打包并加载前端页面注册表（与 gen-page-manifest.mjs 同源，避免第二份真源）。 */
export async function loadRegistryPageKeys() {
  const esbuild = require('esbuild');
  const result = await esbuild.build({
    stdin: {
      contents: "export { PAGE_ASSISTANT_REGISTRY } from './src/lib/ai/page-context/registry';",
      resolveDir: FRONTEND_ROOT,
      sourcefile: 'help-docs-entry.ts',
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
  const tmpFile = path.join(tmpDir, 'help-docs.cjs');
  fs.writeFileSync(tmpFile, result.outputFiles[0].text, 'utf8');
  const mod = require(tmpFile);
  return new Set((mod.PAGE_ASSISTANT_REGISTRY ?? []).map(page => page.pageKey));
}

/** 解析页面清单 JSON（H0-02 生成物）；结构非法时抛出可定位错误。 */
export function parsePageManifest(jsonText) {
  let parsed;
  try {
    parsed = JSON.parse(jsonText);
  } catch (err) {
    throw new Error(`页面清单不是合法 JSON：${err.message}`);
  }
  if (!parsed || !Array.isArray(parsed.pages)) {
    throw new Error('页面清单缺少 pages 数组（生成物损坏？运行 npm run docs:manifest 重建）');
  }
  return { generatedAt: parsed.generatedAt, pages: parsed.pages };
}