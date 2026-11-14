#!/usr/bin/env node
/**
 * Hardcoded color audit.
 *
 * Usage:
 *   node scripts/audit-hardcoded-colors.mjs
 *   node scripts/audit-hardcoded-colors.mjs --json
 *
 * Scope: TypeScript and CSS files under src/
 * Excludes tests. Token definition files are reported separately.
 *
 * This is an audit, not a style linter: it does not fail the build.
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const srcRoot = path.join(frontendRoot, 'src');

const EXTENSIONS = new Set(['.ts', '.tsx', '.css']);
const TOKEN_FILES = new Set([
  'styles/theme-variables.css',
  'lib/design-system/colors.ts',
  'lib/design-system/chart-theme.ts',
  'lib/design-system/theme.tsx',
  'lib/design-system/spacing.ts',
  'design-system/tokens/index.ts',
  'lib/color-contrast.ts',
]);

const RAW_RE = /#[0-9a-fA-F]{3,8}\b|rgba?\([^)]*\)|hsla?\([^)]*\)/g;
const INLINE_RE = /(?:color|background|backgroundColor|borderColor|fill|stroke|boxShadow)\s*:\s*['"](?:#|rgb|hsl|white|black)/;
const TAILWIND_RAW_RE = /(?:bg|text|border|from|to|via|ring|fill|stroke|decoration|outline|shadow)-\[#[0-9a-fA-F]{3,8}\]/;
const TAILWIND_PALETTE_RE = /\b(?:bg|text|border|from|to|via|ring|fill|stroke)-(?:white|black|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|zinc|neutral|stone)(?:-\d{2,3})?\b/g;

const walk = dir => {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === '__tests__' || entry.name === 'node_modules') continue;
      out.push(...walk(full));
    } else if (entry.isFile() && EXTENSIONS.has(path.extname(entry.name))) {
      if (/\.(test|spec)\.(ts|tsx)$/.test(entry.name)) continue;
      out.push(full);
    }
  }
  return out;
};

const rel = full => path.relative(srcRoot, full).split(path.sep).join('/');
const addToMap = (map, key, value) => {
  if (!map.has(key)) map.set(key, []);
  map.get(key).push(...value);
};
const sortedEntries = map => [...map.entries()].sort((a, b) => b[1].length - a[1].length);

const files = walk(srcRoot);
const rawByValue = new Map();
const inlineFiles = new Map();
const cssFiles = new Map();
const otherFiles = new Map();
const tailwindRawFiles = new Map();
const tailwindPaletteFiles = new Map();
let tokenOccurrences = 0;
let tokenLines = 0;
let rawOccurrences = 0;
let rawLines = 0;

for (const file of files) {
  const fileRel = rel(file);
  const text = fs.readFileSync(file, 'utf8');
  const lines = text.split(/\r?\n/);
  const isTokenFile = TOKEN_FILES.has(fileRel);
  const isCss = path.extname(file) === '.css';

  lines.forEach((line, index) => {
    const hits = line.match(RAW_RE) ?? [];
    if (hits.length) {
      rawOccurrences += hits.length;
      rawLines += 1;
      if (isTokenFile) {
        tokenOccurrences += hits.length;
        tokenLines += 1;
        return;
      }
      for (const hit of hits) rawByValue.set(hit, (rawByValue.get(hit) ?? 0) + 1);
      const entry = { line: index + 1, text: line.trim().slice(0, 240), hits };
      if (isCss) {
        addToMap(cssFiles, fileRel, [entry]);
      } else if (INLINE_RE.test(line)) {
        addToMap(inlineFiles, fileRel, [entry]);
      } else {
        addToMap(otherFiles, fileRel, [entry]);
      }
    }

    if (!isTokenFile) {
      const twRaw = line.match(TAILWIND_RAW_RE);
      if (twRaw) addToMap(tailwindRawFiles, fileRel, [{ line: index + 1, text: line.trim().slice(0, 240), hits: twRaw }]);
      const twPalette = line.match(TAILWIND_PALETTE_RE);
      if (twPalette) addToMap(tailwindPaletteFiles, fileRel, [{ line: index + 1, text: line.trim().slice(0, 240), hits: twPalette }]);
    }
  });
}

const countMapLines = map => [...map.values()].reduce((sum, entries) => sum + entries.length, 0);
const countMapOccurrences = map => [...map.values()].reduce((sum, entries) => sum + entries.reduce((n, e) => n + e.hits.length, 0), 0);
const topValues = [...rawByValue.entries()].sort((a, b) => b[1] - a[1]).slice(0, 25);

const summary = {
  scannedFiles: files.length,
  tokenFiles: [...TOKEN_FILES],
  token: { lines: tokenLines, occurrences: tokenOccurrences },
  rawColors: { lines: rawLines, occurrences: rawOccurrences },
  nonToken: {
    inline: { lines: countMapLines(inlineFiles), files: inlineFiles.size },
    css: { lines: countMapLines(cssFiles), files: cssFiles.size },
    other: { lines: countMapLines(otherFiles), files: otherFiles.size },
  },
  tailwind: {
    arbitraryHex: { lines: countMapLines(tailwindRawFiles), files: tailwindRawFiles.size },
    paletteClasses: { occurrences: countMapOccurrences(tailwindPaletteFiles), files: tailwindPaletteFiles.size },
  },
  topValues,
  topInlineFiles: sortedEntries(inlineFiles).slice(0, 20).map(([file, entries]) => ({ file, lines: entries.length })),
  topCssFiles: sortedEntries(cssFiles).slice(0, 20).map(([file, entries]) => ({ file, lines: entries.length })),
  topOtherFiles: sortedEntries(otherFiles).slice(0, 20).map(([file, entries]) => ({ file, lines: entries.length })),
  topTailwindPaletteFiles: sortedEntries(tailwindPaletteFiles).slice(0, 20).map(([file, entries]) => ({ file, occurrences: entries.reduce((n, e) => n + e.hits.length, 0) })),
};

if (process.argv.includes('--json')) {
  console.log(JSON.stringify(summary, null, 2));
} else {
  console.log('Hardcoded Color Audit');
  console.log(`Scanned: ${summary.scannedFiles} files under src/`);
  console.log('');
  console.log('Raw color literals');
  console.log(`  total:       ${rawOccurrences} occurrences / ${rawLines} lines`);
  console.log(`  token files: ${tokenOccurrences} occurrences / ${tokenLines} lines (allowed)`);
  console.log(`  non-token:   ${rawOccurrences - tokenOccurrences} occurrences`);
  console.log('');
  console.log('Non-token distribution');
  console.log(`  component inline styles: ${summary.nonToken.inline.lines} lines / ${summary.nonToken.inline.files} files`);
  console.log(`  CSS files:              ${summary.nonToken.css.lines} lines / ${summary.nonToken.css.files} files`);
  console.log(`  other TS/JS constants:  ${summary.nonToken.other.lines} lines / ${summary.nonToken.other.files} files`);
  console.log('');
  console.log('Tailwind');
  console.log(`  arbitrary hex classes: ${summary.tailwind.arbitraryHex.lines} lines / ${summary.tailwind.arbitraryHex.files} files`);
  console.log(`  palette classes:       ${summary.tailwind.paletteClasses.occurrences} occurrences / ${summary.tailwind.paletteClasses.files} files`);
  console.log('');
  console.log('Most common raw values');
  for (const [value, count] of topValues) console.log(`  ${String(count).padStart(4)}  ${value}`);
  console.log('');
  console.log('Top component inline files');
  for (const { file, lines } of summary.topInlineFiles) console.log(`  ${String(lines).padStart(4)}  ${file}`);
  console.log('');
  console.log('Top CSS files');
  for (const { file, lines } of summary.topCssFiles) console.log(`  ${String(lines).padStart(4)}  ${file}`);
  console.log('');
  console.log('Top other TS/JS files');
  for (const { file, lines } of summary.topOtherFiles) console.log(`  ${String(lines).padStart(4)}  ${file}`);
  console.log('');
  console.log('Top Tailwind palette files');
  for (const { file, occurrences } of summary.topTailwindPaletteFiles) console.log(`  ${String(occurrences).padStart(4)}  ${file}`);
}

export { summary };