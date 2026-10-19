import {
  HANDOFF_PREFILL_SOURCE,
  buildHandoffPrefillState,
  pickBlankHandoffPrefill,
  readHandoffPrefill,
} from '../prefill';

describe('handoff prefill contract（T5-04a）', () => {
  it('生成并读取 ticket 预填 state', () => {
    const state = buildHandoffPrefillState('ticket', {
      title: '  打印机无法连接  ',
      description: '  来自助手的描述  ',
    });

    expect(state).toEqual({
      source: HANDOFF_PREFILL_SOURCE,
      targetType: 'ticket',
      prefill: { title: '打印机无法连接', description: '来自助手的描述' },
    });
    expect(readHandoffPrefill(state, 'ticket')).toEqual({
      title: '打印机无法连接',
      description: '来自助手的描述',
    });
  });

  it('目标类型不符 / 来源不符 / 空字段一律拒绝', () => {
    const state = buildHandoffPrefillState('ticket', { title: 'x' });
    expect(readHandoffPrefill(state, 'incident')).toBeNull();
    expect(readHandoffPrefill({ ...state, source: 'other' }, 'ticket')).toBeNull();
    expect(readHandoffPrefill({ source: HANDOFF_PREFILL_SOURCE, targetType: 'ticket', prefill: {} }, 'ticket')).toBeNull();
    expect(readHandoffPrefill(null, 'ticket')).toBeNull();
    expect(readHandoffPrefill('bad', 'ticket')).toBeNull();
  });

  it('长度截断：标题 200、描述 4000', () => {
    const state = buildHandoffPrefillState('incident', {
      title: 'T'.repeat(300),
      description: 'D'.repeat(5000),
    });
    expect(state.prefill.title).toHaveLength(200);
    expect(state.prefill.description).toHaveLength(4000);
  });

  it('只填空白字段，不覆盖已有值', () => {
    expect(
      pickBlankHandoffPrefill(
        { title: 'AI 标题', description: 'AI 描述' },
        { title: '', description: '用户已填' }
      )
    ).toEqual({ title: 'AI 标题' });
    expect(
      pickBlankHandoffPrefill(
        { title: 'AI 标题', description: 'AI 描述' },
        { title: '用户标题', description: '用户描述' }
      )
    ).toEqual({});
  });
});
