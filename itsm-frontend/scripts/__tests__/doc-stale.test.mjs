/**
 * H1-03 doc-stale 核心纯函数单测（node:test）。
 * 运行：node --test scripts/__tests__/doc-stale.test.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  buildProposals,
  changedFields,
  diffPages,
  pageSignature,
  renderMarkdown,
  summarize,
} from '../lib/doc-stale-core.mjs';

const page = (pageKey, extra = {}) => ({
  pageKey,
  pattern: `/${pageKey}`,
  title: `标题-${pageKey}`,
  entrypoint: 'chat',
  scene: null,
  suggestions: [{ id: 'a', kind: 'task', readOnly: true }],
  ...extra,
});

test('changedFields：逐字段判定 + suggestions 变化', () => {
  const base = page('ticket.detail');
  assert.deepEqual(changedFields(base, page('ticket.detail')), []);

  assert.deepEqual(changedFields(base, page('ticket.detail', { title: '新标题' })), ['title']);
  assert.deepEqual(changedFields(base, page('ticket.detail', { pattern: '/x/:id' })), ['pattern']);
  assert.deepEqual(changedFields(base, page('ticket.detail', { scene: 's2' })), ['scene']);
  assert.deepEqual(
    changedFields(
      base,
      page('ticket.detail', { suggestions: [{ id: 'b', kind: 'question', readOnly: true }] })
    ),
    ['suggestions']
  );
});

test('diffPages：新增/删除/变更（稳定排序）', () => {
  const base = [page('a.page'), page('b.page'), page('c.page', { title: '旧' })];
  const head = [page('a.page'), page('c.page', { title: '新' }), page('d.page')];

  const diff = diffPages(base, head);
  assert.deepEqual(diff.added, ['d.page']);
  assert.deepEqual(diff.removed, ['b.page']);
  assert.deepEqual(diff.changed, [{ pageKey: 'c.page', fields: ['title'] }]);
});

test('buildProposals：动作规则与优先级（create_doc > remove_reference > review_update）', () => {
  const diff = {
    added: ['new.page'],
    removed: ['gone.page'],
    changed: [
      { pageKey: 'changed.withdoc', fields: ['pattern'] },
      { pageKey: 'changed.nodoc', fields: ['suggestions'] },
    ],
  };
  const docsByPageKey = new Map([
    ['changed.withdoc', ['docs/help/x.md']],
    ['gone.page', ['docs/help/y.md']],
  ]);

  const proposals = buildProposals(diff, docsByPageKey);
  const byKey = new Map(proposals.map(item => [item.pageKey, item]));

  assert.equal(byKey.get('new.page').action, 'create_doc');
  assert.deepEqual(byKey.get('new.page').docs, []);
  assert.equal(byKey.get('gone.page').action, 'remove_reference');
  assert.equal(byKey.get('changed.nodoc').action, 'create_doc');
  assert.deepEqual(byKey.get('changed.nodoc').reasons, ['manifest_changed:suggestions']);
  assert.equal(byKey.get('changed.withdoc').action, 'review_update');
  assert.deepEqual(byKey.get('changed.withdoc').reasons, ['manifest_changed:pattern']);

  // 排序：create_doc 优先，其次 remove_reference，最后 review_update。
  assert.deepEqual(
    proposals.map(item => item.action),
    ['create_doc', 'create_doc', 'remove_reference', 'review_update']
  );
});

test('summarize 与 renderMarkdown：计数与空提议文案', () => {
  const diff = { added: ['a'], removed: [], changed: [{ pageKey: 'b', fields: ['title'] }] };
  const proposals = buildProposals(diff, new Map());
  const summary = summarize(diff, proposals);
  assert.deepEqual(summary, {
    added: 1,
    removed: 0,
    changed: 1,
    proposals: 2,
    actions: { create_doc: 2, review_update: 0, remove_reference: 0 },
  });

  const md = renderMarkdown({
    base: 'git:origin/main',
    baseManifestMissing: false,
    headCount: 21,
    proposals,
    summary,
  });
  assert.match(md, /帮助文档 stale 检测/);
  assert.match(md, /create_doc/);

  const emptyMd = renderMarkdown({
    base: 'git:origin/main',
    baseManifestMissing: true,
    headCount: 21,
    proposals: [],
    summary: summarize({ added: [], removed: [], changed: [] }, []),
  });
  assert.match(emptyMd, /未发现需要更新的文档/);
});

test('pageSignature：与字段全集一致（标题/入口/场景/建议）', () => {
  const signature = pageSignature(page('x'));
  assert.equal(signature, JSON.stringify(JSON.parse(signature)));
  assert.notEqual(signature, pageSignature(page('x', { entrypoint: 'ticket_list' })));
});