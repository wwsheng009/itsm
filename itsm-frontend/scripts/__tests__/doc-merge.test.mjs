/**
 * P2 L1 自动合并核心纯函数单测（node:test）。
 * 运行：node --test scripts/__tests__/doc-merge.test.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  FIX_LEVELS,
  addFrontmatterVersion,
  buildAutoMergeAudit,
  classifyDocFix,
  renderAuditLine,
  rewriteRelativeLinks,
  selectAutoMergeCandidates,
  normalizeEofNewline,
  summarizeCandidates,
} from '../lib/doc-merge-core.mjs';

const fix = (id, kind, relative = 'docs/help/a.md', level = undefined) => ({
  id,
  kind,
  relative,
  ...(level ? { level } : {}),
});

test('classifyDocFix：L1 白名单 / 内容类 L2 / 未知 fail-safe L3', () => {
  assert.equal(classifyDocFix('frontmatter_version_add'), FIX_LEVELS.L1);
  assert.equal(classifyDocFix('link_normalize'), FIX_LEVELS.L1);
  assert.equal(classifyDocFix('eof_newline'), FIX_LEVELS.L1);
  assert.equal(classifyDocFix('content_update'), FIX_LEVELS.L2);
  assert.equal(classifyDocFix('body_edit'), FIX_LEVELS.L2);
  assert.equal(classifyDocFix('create_doc'), FIX_LEVELS.L3);
  assert.equal(classifyDocFix('something_new'), FIX_LEVELS.L3, '未知类型保守视为需人工');
});

test('selectAutoMergeCandidates：默认关 / 校验未过 / 仅 L1 / 上限截断', () => {
  const fixes = [
    fix('1', 'frontmatter_version_add', 'docs/help/b.md'),
    fix('2', 'link_normalize', 'docs/help/a.md'),
    fix('3', 'content_update', 'docs/help/c.md'),
    fix('4', 'create_doc', 'docs/help/d.md'),
  ];

  // 策略开关关闭（默认）：全部跳过，且不看校验。
  const disabled = selectAutoMergeCandidates(fixes);
  assert.deepEqual(disabled.candidates, []);
  assert.equal(disabled.skipped.filter(item => item.reason === 'policy_disabled').length, fixes.length);

  // 开关开启但校验未过：同样不放行。
  const notGreen = selectAutoMergeCandidates(fixes, { enabled: true, validationOk: false });
  assert.deepEqual(notGreen.candidates, []);
  assert.equal(notGreen.skipped.filter(item => item.reason === 'validation_not_green').length, fixes.length);

  // 开关开启 + 校验全绿：仅 L1、按文件稳定排序；L2/L3 明确跳过。
  const green = selectAutoMergeCandidates(fixes, { enabled: true, validationOk: true });
  assert.deepEqual(
    green.candidates.map(item => item.id),
    ['2', '1'],
    '按 relative 排序（a.md 先于 b.md）'
  );
  const reasons = green.skipped.map(item => item.reason);
  assert.ok(reasons.includes('level_l2_requires_human'));
  assert.ok(reasons.includes('level_l3_requires_human'));

  // 文件数上限：超出部分跳过并给出原因。
  const capped = selectAutoMergeCandidates(fixes, { enabled: true, validationOk: true, maxFiles: 1 });
  assert.equal(capped.candidates.length, 1);
  assert.ok(capped.skipped.some(item => item.reason === 'max_files_exceeded'));
});

test('addFrontmatterVersion：缺失补全 / 空行填值 / 已有值不动 / 无 frontmatter 不造', () => {
  const raw = '---\npage_keys:\n  - ticket.detail\naudience: internal\nlast_reviewed: 2026-10-01\nowner: docs\n---\n\n# 工单详情\n\n正文。\n';
  const added = addFrontmatterVersion(raw, '1.7');
  assert.equal(added.changed, true);
  assert.match(added.content, /\nversion: 1\.7\n/);
  assert.ok(added.content.endsWith('\n\n# 工单详情\n\n正文。\n'), '正文逐字节保留');
  // 幂等：再补一次不变更。
  assert.equal(addFrontmatterVersion(added.content, '1.7').changed, false);

  // 已有空 `version:` → 填值。
  const empty = '---\naudience: internal\nversion:\nowner: docs\n---\n\n# 标题\n';
  const filled = addFrontmatterVersion(empty, '1.7');
  assert.equal(filled.changed, true);
  assert.match(filled.content, /version: 1\.7/);
  assert.ok(!/version:\n/.test(filled.content));

  // 已有值 / 缺 frontmatter / 非法推导值 → 不变更。
  assert.equal(addFrontmatterVersion('---\nversion: 1.8\n---\n\n# T\n', '1.7').changed, false);
  assert.equal(addFrontmatterVersion('# 无 frontmatter\n', '1.7').changed, false);
  assert.equal(addFrontmatterVersion(raw, 'v1').changed, false);
  assert.equal(addFrontmatterVersion(raw, null).changed, false);
});

test('normalizeEofNewline：仅补 EOF 换行；**行尾空格（Markdown 硬换行）逐字节保留**', () => {
  // 行尾两空格 = 硬换行语法：必须原样保留（逐行去空白会改变渲染语义）。
  const hardBreak = '# 标题  \n第二行\n\n-- 行尾空格保留 --  \n末尾';
  const fixed = normalizeEofNewline(hardBreak);
  assert.equal(fixed, `${hardBreak}\n`, '只补 EOF 换行');
  assert.ok(fixed.includes('标题  \n'), '行尾两个空格保留');
  assert.ok(fixed.includes('--  \n'), '行尾空格保留');
  assert.equal(normalizeEofNewline(fixed), fixed, '幂等');
  assert.equal(normalizeEofNewline(''), '', '空文件不动');
  assert.equal(normalizeEofNewline('已带换行\n'), '已带换行\n');
});

test('rewriteRelativeLinks：唯一命中才改写（保留锚点/图片前缀/标题），歧义与外部链接不动', () => {
  const docsIndex = new Map([
    ['detail.md', 'docs/help/tickets/detail.md'],
    ['index.md', 'docs/help/index.md'],
  ]);
  const exists = (_fromFile, target) => target === './ok.md';
  const relative = (fromFile, hit) =>
    fromFile === 'docs/help/guide/start.md' ? `../${hit.split('/').slice(-2).join('/')}` : hit;

  const content = [
    '[已可用](./ok.md)',
    '[坏链](./detail.md#字段)',
    '![截图](broken.png)',
    '[歧义](unknown.md)',
    '[外部](https://example.com/a.md)',
    '[绝对](/docs/help/index.md)',
  ].join('\n');

  const result = rewriteRelativeLinks(content, {
    fromFile: 'docs/help/guide/start.md',
    docsIndex,
    exists,
    relative,
  });

  assert.equal(result.changes.length, 1, '仅唯一命中的相对坏链被改写');
  assert.deepEqual(result.changes[0], { from: './detail.md#字段', to: '../tickets/detail.md#字段' });
  assert.ok(result.content.includes('[已可用](./ok.md)'), '可解析链接不动');
  assert.ok(result.content.includes('../tickets/detail.md#字段'), '锚点保留');
  assert.ok(result.content.includes('![截图](broken.png)'), '未命中的图片链接不动');
  assert.ok(result.content.includes('[歧义](unknown.md)'), '歧义不动');
  assert.ok(result.content.includes('[外部](https://example.com/a.md)'), '外部链接不动');
  assert.ok(result.content.includes('[绝对](/docs/help/index.md)'), '绝对路径不动（仓库绝对不做相对改写）');
});

test('buildAutoMergeAudit / renderAuditLine：审计结构与 JSONL 单行', () => {
  const record = buildAutoMergeAudit({
    at: '2026-10-18T12:00:00Z',
    actor: 'ci#42',
    mode: 'apply',
    policyEnabled: true,
    validationBefore: 'green',
    validationAfter: 'green',
    applied: [{ relative: 'docs/help/a.md', kind: 'trailing_whitespace', detail: 'x' }],
    skipped: [{ id: '3', reason: 'level_l2_requires_human' }],
  });
  assert.equal(record.mode, 'apply');
  assert.equal(record.applied.length, 1);
  assert.equal(record.rolledBack, false);

  const line = renderAuditLine(record);
  assert.ok(line.endsWith('\n'));
  assert.equal(line.trim().split('\n').length, 1, 'JSONL 单行');
  assert.deepEqual(JSON.parse(line), record);
});

test('summarizeCandidates：计数与文件去重', () => {
  const summary = summarizeCandidates([
    fix('1', 'frontmatter_version_add', 'docs/help/a.md'),
    fix('2', 'eof_newline', 'docs/help/a.md'),
    fix('3', 'link_normalize', 'docs/help/b.md'),
  ]);
  assert.equal(summary.total, 3);
  assert.equal(summary.files, 2);
  assert.deepEqual(summary.byKind, {
    frontmatter_version_add: 1,
    eof_newline: 1,
    link_normalize: 1,
  });
});