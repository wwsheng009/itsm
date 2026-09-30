/**
 * B3-02 契约测试：launcher 路由 state 的构建/校验/请求片段（纯函数，无渲染）。
 *
 * 口径：非法 state 一律按「无上下文」处理（不抛错、不部分采纳）；目标必须成对；
 * 请求片段仅含非空字段（缺省请求体与现状一致）。
 */
import {
  ASK_AI_STATE_SOURCE,
  ASK_AI_SUMMARY_MAX_LENGTH,
  askAIEntrypointLabel,
  buildAskAIRequestScope,
  buildAskAIState,
  readAskAIScope,
} from '../ask-ai-scope';

describe('buildAskAIState / readAskAIScope', () => {
  it('往返一致：合法 scope 保留入口与目标，summary 去空白', () => {
    const state = buildAskAIState({
      entrypoint: 'ticket_detail',
      targetType: 'ticket',
      targetId: 42,
      summary: '  打印机故障  ',
    });
    expect(state.source).toBe(ASK_AI_STATE_SOURCE);
    expect(readAskAIScope(JSON.parse(JSON.stringify(state)))).toEqual({
      entrypoint: 'ticket_detail',
      targetType: 'ticket',
      targetId: 42,
      summary: '打印机故障',
    });
  });

  it('无目标上下文（列表页入口）合法', () => {
    const scope = readAskAIScope(buildAskAIState({ entrypoint: 'ticket_list' }));
    expect(scope).toEqual({ entrypoint: 'ticket_list' });
  });

  it('来源不符 / 非对象 / 缺 scope 一律 null', () => {
    expect(readAskAIScope(null)).toBeNull();
    expect(readAskAIScope('x')).toBeNull();
    expect(readAskAIScope([])).toBeNull();
    expect(readAskAIScope({ source: 'other', scope: { entrypoint: 'chat' } })).toBeNull();
    expect(readAskAIScope({ source: ASK_AI_STATE_SOURCE })).toBeNull();
  });

  it('未知入口拒绝（不静默降级）', () => {
    expect(
      readAskAIScope({ source: ASK_AI_STATE_SOURCE, scope: { entrypoint: 'ticketdetail' } })
    ).toBeNull();
  });

  it('目标必须成对：只给一半拒绝', () => {
    expect(
      readAskAIScope({ source: ASK_AI_STATE_SOURCE, scope: { entrypoint: 'chat', targetType: 'ticket' } })
    ).toBeNull();
    expect(
      readAskAIScope({ source: ASK_AI_STATE_SOURCE, scope: { entrypoint: 'chat', targetId: 3 } })
    ).toBeNull();
  });

  it('目标类型未知 / ID 非正整数拒绝', () => {
    expect(
      readAskAIScope({
        source: ASK_AI_STATE_SOURCE,
        scope: { entrypoint: 'chat', targetType: 'user', targetId: 3 },
      })
    ).toBeNull();
    for (const badId of [0, -1, 1.5, '3', null]) {
      expect(
        readAskAIScope({
          source: ASK_AI_STATE_SOURCE,
          scope: { entrypoint: 'chat', targetType: 'ticket', targetId: badId },
        })
      ).toBeNull();
    }
  });

  it('summary 超长截断到上限（与后端 300 rune 同口径）', () => {
    const long = '中'.repeat(ASK_AI_SUMMARY_MAX_LENGTH + 20);
    const scope = readAskAIScope(buildAskAIState({ entrypoint: 'chat', summary: long }));
    expect(scope?.summary).toHaveLength(ASK_AI_SUMMARY_MAX_LENGTH);
  });
});

describe('buildAskAIRequestScope', () => {
  it('null/undefined → 空对象（请求体与现状一致）', () => {
    expect(buildAskAIRequestScope(null)).toEqual({});
    expect(buildAskAIRequestScope(undefined)).toEqual({});
  });

  it('chat + 无目标 → 空对象（不落字段）', () => {
    expect(buildAskAIRequestScope({ entrypoint: 'chat' })).toEqual({});
  });

  it('非 chat 入口 + 目标 → 全字段', () => {
    expect(
      buildAskAIRequestScope({
        entrypoint: 'ticket_detail',
        targetType: 'ticket',
        targetId: 42,
        summary: '打印机故障',
      })
    ).toEqual({
      entrypoint: 'ticket_detail',
      targetType: 'ticket',
      targetId: 42,
      summary: '打印机故障',
    });
  });

  it('入口标签可读', () => {
    expect(askAIEntrypointLabel('ticket_detail')).toBe('工单详情');
    expect(askAIEntrypointLabel('ci_detail')).toBe('配置项详情');
  });
});
