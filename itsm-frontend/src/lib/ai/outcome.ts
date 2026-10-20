/**
 * I0-04 Outcome Contract（结果落点）：把助手回合内产生的「可验证结果」归一为回执条目。
 *
 * 数据源（全部来自既有 SSE 契约，零额外请求）：
 * - 工具事件 `done` + `artifactId`（I0-04 事件扩展）→ `artifact` 产物落点；
 * - 工具事件 `pending` / `pendingApprovals` → 审批中心 `navigation`；
 * - 草稿类工具（`draft_ticket_fields`/`draft_kb_article`/`help_create_draft`）→ `draft` + 目标页面/`applyTarget`。
 *
 * 边界：仅做「展示与跳转」；不臆造引用（无 artifactId 的产物工具只给规则跳转），重复落点去重，上限 4 条。
 */
import type { AIToolStreamEvent } from '@/lib/api/ai-api';

export type AssistantOutcomeKind = 'entity' | 'artifact' | 'draft' | 'navigation';

export interface AssistantOutcome {
  kind: AssistantOutcomeKind;
  /** 稳定引用（artifact:31 / invocation:9 / route 等），供展示与核验。 */
  ref: string;
  /** 人类可读标题（优先来自后端产物 title / 工具规则）。 */
  title?: string;
  /** 标题别名（兼容后端 name 字段；与 title 同口径）。 */
  name?: string;
  /** 摘要/描述（标题缺失时取首句作为回退）。 */
  summary?: string;
  /** 兼容既有字段（与 summary 同义；工具名等短说明）。 */
  label: string;
  /** 跳转目标（缺省 = 无跳转，仅可复制引用）。 */
  route?: string;
  /** 应用落点标识（如 ticket.create.form；供 Inline Apply 通道消费）。 */
  applyTarget?: string;
  description?: string;
}

const OUTCOME_LIMIT = 4;

interface OutcomeRule {
  kind: AssistantOutcomeKind;
  label: string;
  route?: string;
  applyTarget?: string;
}

const TOOL_OUTCOME_RULES: Record<string, OutcomeRule> = {
  draft_ticket_fields: {
    kind: 'draft',
    label: '去创建工单（字段草案）',
    route: '/tickets/create',
    applyTarget: 'ticket.create.form',
  },
  draft_kb_article: {
    kind: 'draft',
    label: '去新建知识文章（草稿）',
    route: '/knowledge/articles/new',
    applyTarget: 'knowledge.article.form',
  },
  help_create_draft: {
    kind: 'draft',
    label: '帮助草稿待评审（覆盖率看板）',
    route: '/ai/help-coverage',
  },
  create_ticket: { kind: 'navigation', label: '去审批中心（建单待审批）', route: '/ai/approval' },
  update_ticket: { kind: 'navigation', label: '去审批中心（更新待审批）', route: '/ai/approval' },
  link_ticket_ci: { kind: 'navigation', label: '去审批中心（关联动作待审批）', route: '/ai/approval' },
  analyze_ci_impact_plan: { kind: 'artifact', label: '影响分析产物' },
};

/**
 * 产物标题规则：工具没有回带 artifactTitle 时的可读名称。
 *
 * 这些名称与后端 saveArtifact 的 title 同源（不必逐字一致），但**绝不**用
 * 「类型 + #id」拼占位名——内部 id 只用于 ref/路由，不进入正文。
 */
const ARTIFACT_TITLE_BY_TOOL: Record<string, string> = {
  draft_ticket_fields: '工单字段草案',
  analyze_ci_impact_plan: '配置项变更影响分析草案',
  draft_kb_article: '知识文章草稿',
  help_create_draft: '帮助文档草稿',
  help_update_draft: '帮助文档草稿',
};

const artifactTitleFor = (event: AIToolStreamEvent): string | undefined => {
  const fromBackend = (event.artifactTitle ?? '').trim();
  if (fromBackend) return fromBackend;
  if (event.artifactKind === 'plan') return 'AI 生成计划草案';
  if (event.artifactKind === 'analysis') return 'AI 影响分析草案';
  if (event.artifactKind === 'draft') return 'AI 生成草稿';
  return ARTIFACT_TITLE_BY_TOOL[event.tool];
};

export function deriveAssistantOutcomes(input: {
  toolEvents?: readonly AIToolStreamEvent[];
  pendingApprovals?: readonly number[];
}): AssistantOutcome[] {
  const outcomes: AssistantOutcome[] = [];
  const seen = new Set<string>();
  const push = (outcome: AssistantOutcome) => {
    const key = `${outcome.kind}:${outcome.ref}`;
    if (seen.has(key)) return;
    seen.add(key);
    outcomes.push(outcome);
  };

  for (const event of input.toolEvents ?? []) {
    if (!event || typeof event.tool !== 'string' || event.tool.length === 0) continue;
    const status = event.status ?? '';

    if (status === 'pending') {
      const ref = typeof event.id === 'number' && event.id > 0 ? `invocation:${event.id}` : `approval:${event.tool}`;
      push({
        kind: 'navigation',
        ref,
        title: '去审批中心（待审批）',
        label: '去审批中心（待审批）',
        route: '/ai/approval',
        description: event.tool,
      });
      continue;
    }
    if (status !== 'done') continue;

    if (typeof event.artifactId === 'number' && event.artifactId > 0) {
      const title = artifactTitleFor(event);
      push({
        kind: 'artifact',
        ref: `artifact:${event.artifactId}`,
        // 标题优先用后端真实 title；缺失时用工具规则；两者都缺失才落到通用占位，
        // 由回执卡上报 title_fallback（可观测），而不是把 id 铺到正文。
        title: title ?? 'AI 生成产物',
        name: title,
        label: title ?? 'AI 生成产物',
        route: `/ai/artifacts/${event.artifactId}`,
        description: event.tool,
      });
    }
    const rule = TOOL_OUTCOME_RULES[event.tool];
    if (rule) {
      push({
        kind: rule.kind,
        // 稳定引用只承载「类型 + 业务标识」，不把路由拼进来（路由单独放 route）。
        ref: `${rule.kind}:${event.tool}`,
        title: rule.label,
        label: rule.label,
        route: rule.route,
        applyTarget: rule.applyTarget,
        description: event.tool,
      });
    }
  }

  for (const id of input.pendingApprovals ?? []) {
    if (typeof id !== 'number' || id <= 0) continue;
    push({
      kind: 'navigation',
      ref: `invocation:${id}`,
      title: `去审批中心（#${id} 待审批）`,
      label: `去审批中心（#${id} 待审批）`,
      route: '/ai/approval',
    });
  }

  return outcomes.slice(0, OUTCOME_LIMIT);
}

export const OUTCOME_KIND_LABELS: Record<AssistantOutcomeKind, string> = {
  entity: '对象',
  artifact: '产物',
  draft: '草稿',
  navigation: '跳转',
};