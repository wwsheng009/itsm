/**
 * H1-03 文档 stale 检测核心（纯函数，node:test 覆盖）。
 *
 * 口径（方案 §5.6 触发源 4：代码变更 → 标记相关文档 stale → 生成更新提议）：
 *   - 页面定义变化判定字段：pattern / title / entrypoint / scene / suggestions（id/kind/readOnly）；
 *   - 新增页面 → 无文档 = create_doc；已有文档 = review_update（同 PR 已补文档，低优先复核）；
 *   - 删除页面 → 仍有文档引用 = remove_reference（orphan 清理）；
 *   - 变更页面 → 无文档 = create_doc；有文档 = review_update（附字段原因）。
 * 优先级：create_doc > remove_reference > review_update；同级按 pageKey。
 */

export const PAGE_SIGNATURE_FIELDS = ['pattern', 'title', 'entrypoint', 'scene'];

function signaturePart(page) {
  return {
    pattern: page.pattern ?? null,
    title: page.title ?? null,
    entrypoint: page.entrypoint ?? null,
    scene: page.scene ?? null,
    suggestions: (page.suggestions ?? []).map(item => ({
      id: item.id ?? null,
      kind: item.kind ?? null,
      readOnly: item.readOnly ?? null,
    })),
  };
}

export function pageSignature(page) {
  return JSON.stringify(signaturePart(page));
}

/** 返回变化的字段名（空数组 = 无变化）。 */
export function changedFields(basePage, headPage) {
  const base = signaturePart(basePage);
  const head = signaturePart(headPage);
  const fields = [];
  for (const field of PAGE_SIGNATURE_FIELDS) {
    if (base[field] !== head[field]) fields.push(field);
  }
  if (JSON.stringify(base.suggestions) !== JSON.stringify(head.suggestions)) {
    fields.push('suggestions');
  }
  return fields;
}

export function diffPages(basePages, headPages) {
  const baseByKey = new Map(basePages.map(page => [page.pageKey, page]));
  const headByKey = new Map(headPages.map(page => [page.pageKey, page]));

  const added = [...headByKey.keys()].filter(key => !baseByKey.has(key)).sort();
  const removed = [...baseByKey.keys()].filter(key => !headByKey.has(key)).sort();
  const changed = [...headByKey.keys()]
    .filter(key => baseByKey.has(key))
    .map(key => ({ pageKey: key, fields: changedFields(baseByKey.get(key), headByKey.get(key)) }))
    .filter(item => item.fields.length > 0)
    .sort((a, b) => a.pageKey.localeCompare(b.pageKey));

  return { added, removed, changed };
}

const ACTION_ORDER = { create_doc: 0, remove_reference: 1, review_update: 2 };

/** 依据 diff 与「页面键 → 文档」映射生成更新提议（稳定排序）。 */
export function buildProposals({ added, removed, changed }, docsByPageKey) {
  const docsOf = key => docsByPageKey?.get(key) ?? [];
  const proposals = [];

  for (const item of changed) {
    const docs = docsOf(item.pageKey);
    proposals.push({
      pageKey: item.pageKey,
      action: docs.length > 0 ? 'review_update' : 'create_doc',
      docs,
      reasons: [`manifest_changed:${item.fields.join(',')}`],
    });
  }
  for (const key of added) {
    const docs = docsOf(key);
    proposals.push({
      pageKey: key,
      action: docs.length > 0 ? 'review_update' : 'create_doc',
      docs,
      reasons: ['new_page'],
    });
  }
  for (const key of removed) {
    const docs = docsOf(key);
    if (docs.length > 0) {
      proposals.push({
        pageKey: key,
        action: 'remove_reference',
        docs,
        reasons: ['page_removed'],
      });
    }
  }

  return proposals.sort((a, b) => {
    if (ACTION_ORDER[a.action] !== ACTION_ORDER[b.action]) {
      return ACTION_ORDER[a.action] - ACTION_ORDER[b.action];
    }
    return a.pageKey.localeCompare(b.pageKey);
  });
}

export function summarize(diff, proposals) {
  const actions = { create_doc: 0, review_update: 0, remove_reference: 0 };
  for (const proposal of proposals) actions[proposal.action] += 1;
  return {
    added: diff.added.length,
    removed: diff.removed.length,
    changed: diff.changed.length,
    proposals: proposals.length,
    actions,
  };
}

/** 夜批任务/Issue 用的 Markdown 摘要。 */
export function renderMarkdown(report) {
  const lines = [];
  lines.push('## 帮助文档 stale 检测（H1-03 夜间批任务）');
  lines.push('');
  lines.push(
    `基线：\`${report.base}\`${report.baseManifestMissing ? '（基线无页面清单 → 按空基线处理）' : ''} · ` +
      `页面 ${report.headCount} 个 · 变更 ${report.summary.changed} / 新增 ${report.summary.added} / 删除 ${report.summary.removed}`
  );
  lines.push('');
  if (report.proposals.length === 0) {
    lines.push('未发现需要更新的文档（页面定义自基线以来无变化）。');
    return `${lines.join('\n')}\n`;
  }
  lines.push(`| 页面键 | 动作 | 关联文档 | 原因 |`);
  lines.push('|---|---|---|---|');
  for (const proposal of report.proposals) {
    lines.push(
      `| \`${proposal.pageKey}\` | ${proposal.action} | ${proposal.docs.join('<br>') || '（无）'} | ${proposal.reasons.join('；')} |`
    );
  }
  lines.push('');
  lines.push(
    '处理指引：按 `itsm-agent/prompts/docs-maintenance.md` 起草/修订，`npm run docs:validate` 与 `npm run docs:ledger:check` 全绿后走 PR 通道。'
  );
  return `${lines.join('\n')}\n`;
}