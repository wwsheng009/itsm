/**
 * P2 L1 自动合并策略（方案 §5.7）：机械修正的分类、候选选择与安全变换（纯函数）。
 *
 * 口径：
 * - **L1 机械修正**：frontmatter 补全（可推导值）、相对链接修正、行尾空白清理 → 校验全绿可自动合并（策略开关）；
 * - **L2/L3**（正文内容修订 / 新增页面文档）**绝不自动合并**：内容修订走一键审阅、首篇必须人工过目；
 * - **未知修正类型一律按 L3 处理（fail-safe）**，避免策略扩张后误放；
 * - 默认 `enabled=false`（先观察）：`selectAutoMergeCandidates` 在开关关闭/校验未通过时返回空集；
 * - 每批文件数上限（默认 20），防止一次自动合并放大风险。
 */

export const FIX_LEVELS = Object.freeze({ L1: 'l1', L2: 'l2', L3: 'l3' });

/** L1 允许的机械修正类型（白名单；新增类型必须同步单测与方案证据）。 */
export const L1_FIX_KINDS = Object.freeze([
  'frontmatter_version_add',
  'link_normalize',
  'eof_newline',
]);

const L1_KIND_SET = new Set(L1_FIX_KINDS);

/** 修正类型 → 分级：L1 白名单；内容类显式 L2；其余（含未知）fail-safe 视为 L3。 */
export function classifyDocFix(kind) {
  if (L1_KIND_SET.has(kind)) return FIX_LEVELS.L1;
  if (kind === 'content_update' || kind === 'body_edit') return FIX_LEVELS.L2;
  return FIX_LEVELS.L3;
}

/**
 * 选择可自动合并的候选：
 * - `enabled=false`（默认）→ []（策略开关默认关，只观察）；
 * - `validationOk=false` → []（校验全绿是自动合并的前置条件）；
 * - 仅保留 L1；按文件稳定排序后截断到 `maxFiles`。
 * 返回 `{ candidates, skipped }`（skipped 含原因，供审计/报告）。
 */
export function selectAutoMergeCandidates(fixes, { enabled = false, validationOk = false, maxFiles = 20 } = {}) {
  const skipped = [];
  if (!enabled) {
    for (const fix of fixes ?? []) skipped.push({ id: fix.id, reason: 'policy_disabled' });
    return { candidates: [], skipped };
  }
  if (!validationOk) {
    for (const fix of fixes ?? []) skipped.push({ id: fix.id, reason: 'validation_not_green' });
    return { candidates: [], skipped };
  }

  const byLevel = [];
  for (const fix of fixes ?? []) {
    const level = fix.level ?? classifyDocFix(fix.kind);
    if (level === FIX_LEVELS.L1) {
      byLevel.push(fix);
      continue;
    }
    skipped.push({ id: fix.id, reason: `level_${level}_requires_human` });
  }
  byLevel.sort((a, b) => (a.relative < b.relative ? -1 : a.relative > b.relative ? 1 : a.id < b.id ? -1 : 1));

  const limit = Number.isFinite(maxFiles) && maxFiles >= 0 ? maxFiles : 20;
  const candidates = byLevel.slice(0, limit);
  for (const fix of byLevel.slice(limit)) skipped.push({ id: fix.id, reason: 'max_files_exceeded' });
  return { candidates, skipped };
}

/**
 * 仅补齐文件结尾换行（不动任何行内字节）。
 *
 * 为什么不做逐行去尾空白：Markdown 中「行尾两个及以上空格」是**硬换行**语法，
 * 逐行清理会改变渲染语义（本地 E2E 发现该风险后收窄规则）。EOF 换行缺失是纯粹的行尾字节问题，可证安全。
 */
export function normalizeEofNewline(content) {
  if (content === '') return content;
  return content.endsWith('\n') ? content : `${content}\n`;
}

/**
 * frontmatter 补全：仅当**缺失/为空**且提供了合法推导值时插入 `version`。
 * 无 frontmatter / 已有值 / 推导值非法 → 不变更（不机械造 frontmatter，不覆盖人工值）。
 */
export function addFrontmatterVersion(content, version, { versionRe = /^\d+(\.\d+){0,3}$/ } = {}) {
  if (!version || !versionRe.test(version)) return { content, changed: false };
  const match = /^---\r?\n([\s\S]*?)\r?\n---(\r?\n?)/.exec(content);
  if (!match) return { content, changed: false };

  const block = match[1];
  const lines = block.split(/\r?\n/);
  const versionLine = /^\s*version\s*:\s*(.*)$/;
  const hasValue = lines.some(line => {
    const kv = versionLine.exec(line);
    return kv && kv[1].trim() !== '' && kv[1].trim() !== '""' && kv[1].trim() !== "''";
  });
  if (hasValue) return { content, changed: false };

  // 已有空 `version:` 行 → 补值；否则插到 frontmatter 末尾。
  const emptyIndex = lines.findIndex(line => {
    const kv = versionLine.exec(line);
    return kv && kv[1].trim() === '';
  });
  if (emptyIndex >= 0) {
    lines[emptyIndex] = `version: ${version}`;
  } else {
    lines.push(`version: ${version}`);
  }

  const rebuilt = `---\n${lines.join('\n')}\n---${match[2]}`;
  return { content: rebuilt + content.slice(match[0].length), changed: true };
}

const MARKDOWN_LINK_RE = /(!?\[[^\]]*\]\()([^)]+)(\))/g;

/**
 * 相对链接机械修正：目标不存在时，若其 basename 在 `docsIndex`（basename → 相对路径）中**唯一命中**，
 * 则改写为相对当前文件的正确路径（保留锚点/标题/前缀 `!`）。歧义或未命中 → 不改（交人工）。
 *
 * 依赖注入（便于单测）：
 *   - `exists(fromFile, target)`：目标相对链接是否可解析到真实文件；
 *   - `relative(fromFile, hitRelativePath)`：生成从当前文件到命中文档的相对路径。
 */
export function rewriteRelativeLinks(content, { fromFile, docsIndex, exists, relative, toPosix = p => p.split('\\').join('/') }) {
  const changes = [];
  const next = content.replace(MARKDOWN_LINK_RE, (whole, prefix, rawTarget, suffix) => {
    const trimmed = rawTarget.trim();
    if (!trimmed) return whole;
    const [targetPart, ...rest] = trimmed.split(/\s+/);
    const [pathPart, anchor = ''] = targetPart.split('#');
    if (!pathPart) return whole;
    if (/^(https?:|mailto:|tel:|data:|\/)/i.test(pathPart)) return whole;

    let decoded = pathPart;
    try {
      decoded = decodeURIComponent(pathPart);
    } catch {
      /* 保留原样，按未命中处理 */
    }
    if (typeof exists === 'function' && exists(fromFile, decoded)) return whole;

    const basename = decoded.split('/').pop();
    const hit = docsIndex?.get(basename);
    if (!hit) return whole;

    const rewrittenBase = typeof relative === 'function' ? toPosix(relative(fromFile, hit)) : toPosix(hit);
    if (rewrittenBase === toPosix(decoded)) return whole;
    const rewritten = `${rewrittenBase}${anchor ? `#${anchor}` : ''}`;
    changes.push({ from: targetPart, to: rewritten });
    return `${prefix}${rewritten}${rest.length > 0 ? ` ${rest.join(' ')}` : ''}${suffix}`;
  });
  return { content: next, changes };
}

/** 审计记录（写入 `docs/help/_automerge-audit.jsonl`；所有自动写入留痕，方案 §5.7）。 */
export function buildAutoMergeAudit({
  at,
  actor = 'local',
  mode = 'dry-run',
  policyEnabled = false,
  validationBefore,
  validationAfter,
  applied = [],
  skipped = [],
  rolledBack = false,
  note = '',
}) {
  return {
    at,
    actor,
    mode,
    policyEnabled,
    validationBefore: validationBefore ?? null,
    validationAfter: validationAfter ?? null,
    applied: applied.map(item => ({
      relative: item.relative,
      kind: item.kind,
      detail: item.detail ?? null,
    })),
    skipped,
    rolledBack,
    note,
  };
}

/** 审计行（JSONL，单行；便于追加与机器解析）。 */
export function renderAuditLine(record) {
  return `${JSON.stringify(record)}\n`;
}

/** 候选汇总（stdout/报告共用）。 */
export function summarizeCandidates(candidates) {
  const byKind = {};
  for (const fix of candidates) {
    byKind[fix.kind] = (byKind[fix.kind] ?? 0) + 1;
  }
  return {
    total: candidates.length,
    files: [...new Set(candidates.map(fix => fix.relative))].length,
    byKind,
  };
}