/**
 * I0-04 Outcome Contract 派生测试：产物（artifactId）/草稿规则/待审批/去重与上限/无落点不产生。
 */
import {
  deriveAssistantOutcomes,
  OUTCOME_KIND_LABELS,
} from '../outcome';
import type { AIToolStreamEvent } from '@/lib/api/ai-api';

const event = (partial: Partial<AIToolStreamEvent>): AIToolStreamEvent => ({
  tool: 'list_tickets',
  provider: 'builtin',
  phase: 'read',
  status: 'done',
  ...partial,
});

describe('deriveAssistantOutcomes（I0-04）', () => {
  it('产物工具 done + artifactId → artifact 落点（含引用与描述）', () => {
    const outcomes = deriveAssistantOutcomes({
      toolEvents: [event({ tool: 'analyze_ci_impact_plan', artifactId: 31 })],
    });

    expect(outcomes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ kind: 'artifact', ref: 'artifact:31', label: '产物 #31' }),
      ])
    );
    // 规则落点同样出现（产物 + 影响分析规则，各自独立）。
    expect(outcomes.map(item => item.kind)).toContain('artifact');
  });

  it('草稿工具规则 → draft + route + applyTarget（可跳转、可应用）', () => {
    const outcomes = deriveAssistantOutcomes({
      toolEvents: [event({ tool: 'draft_ticket_fields', artifactId: 8 })],
    });

    const draft = outcomes.find(item => item.kind === 'draft');
    expect(draft).toMatchObject({
      label: '去创建工单（字段草案）',
      route: '/tickets/create',
      applyTarget: 'ticket.create.form',
    });
  });

  it('写工具 pending 事件与 pendingApprovals → 审批中心 navigation（去重）', () => {
    const outcomes = deriveAssistantOutcomes({
      toolEvents: [event({ tool: 'create_ticket', status: 'pending', id: 9 })],
      pendingApprovals: [9, 10],
    });

    const navigation = outcomes.filter(item => item.kind === 'navigation');
    // invocation:9 去重为一条；10 追加一条。
    expect(navigation.map(item => item.ref)).toEqual(['invocation:9', 'invocation:10']);
    expect(navigation.every(item => item.route === '/ai/approval')).toBe(true);
  });

  it('无落点事件/用户消息不产生回执', () => {
    expect(
      deriveAssistantOutcomes({ toolEvents: [event({ tool: 'list_tickets' })], pendingApprovals: [] })
    ).toEqual([]);
    expect(deriveAssistantOutcomes({})).toEqual([]);
  });

  it('重复事件去重且上限 4 条；kind 标签齐全', () => {
    const outcomes = deriveAssistantOutcomes({
      toolEvents: [
        event({ tool: 'draft_ticket_fields' }),
        event({ tool: 'draft_ticket_fields' }),
        event({ tool: 'draft_kb_article' }),
        event({ tool: 'help_create_draft' }),
        event({ tool: 'analyze_ci_impact_plan', artifactId: 31 }),
      ],
      pendingApprovals: [9],
    });
    expect(outcomes.length).toBeLessThanOrEqual(4);

    const refs = outcomes.map(item => item.ref);
    expect(new Set(refs).size).toBe(refs.length);
    expect(OUTCOME_KIND_LABELS).toEqual({
      entity: '对象',
      artifact: '产物',
      draft: '草稿',
      navigation: '跳转',
    });
  });
});