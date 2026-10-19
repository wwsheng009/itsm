/**
 * V2-01 个性化记忆：纯函数核心（解析/裁剪/上限/元数据护栏）。
 */
import {
  ASSISTANT_MEMORY_VERSION,
  MEMORY_MAX_PHRASES,
  MEMORY_MAX_PHRASE_CHARS,
  MEMORY_MAX_TEMPLATES,
  addMemoryPhrase,
  emptyAssistantMemory,
  isMemoryEmpty,
  normalizeAssistantMemory,
  recentActionLabel,
  recordRecentAction,
  removeMemoryPhrase,
  removeMemoryTemplate,
  upsertMemoryTemplate,
} from '../types';

describe('normalizeAssistantMemory（V2-01 解析与裁剪）', () => {
  it('非法输入安全降级为空记忆', () => {
    for (const raw of [null, undefined, 'text', 42, []]) {
      expect(normalizeAssistantMemory(raw)).toEqual(emptyAssistantMemory());
    }
    expect(normalizeAssistantMemory({}).version).toBe(ASSISTANT_MEMORY_VERSION);
  });

  it('phrases：去空/去重/按字符截断/条数上限', () => {
    const long = 'x'.repeat(MEMORY_MAX_PHRASE_CHARS + 50);
    const memory = normalizeAssistantMemory({
      phrases: [' 重置密码 ', '', '重置密码', long, ...Array.from({ length: 30 }, (_, i) => `p${i}`)],
    });
    expect(memory.phrases.length).toBe(MEMORY_MAX_PHRASES);
    expect(memory.phrases[0]).toBe('重置密码');
    expect(memory.phrases.filter(p => p === '重置密码')).toHaveLength(1);
    expect(Array.from(memory.phrases[1]).length).toBe(MEMORY_MAX_PHRASE_CHARS);
  });

  it('templates：缺名/重名丢弃，body 截断', () => {
    const memory = normalizeAssistantMemory({
      templates: [
        { name: '网络故障', body: 'x'.repeat(5000) },
        { name: '网络故障', body: '重复名' },
        { name: '   ', body: '空名' },
        { body: '缺名' },
      ],
    });
    expect(memory.templates).toHaveLength(1);
    expect(memory.templates[0].name).toBe('网络故障');
    expect(Array.from(memory.templates[0].body).length).toBe(1000);
  });

  it('recent：只接受结构化动作键 + 合法时间；同键去重', () => {
    const memory = normalizeAssistantMemory({
      recent: [
        { key: 'inline_apply:triage_detail', at: '2026-10-18T10:00:00.000Z' },
        { key: 'inline_apply:triage_detail', at: '2026-10-18T11:00:00.000Z' },
        { key: '这是一段对话正文', at: '2026-10-18T10:00:00.000Z' },
        { key: 'Inline_Apply:UPPER', at: '2026-10-18T10:00:00.000Z' },
        { key: 'memory.phrase_used', at: 'not-a-date' },
        { key: 'memory.template_used', at: '2026-10-18T09:00:00.000Z' },
      ],
    });
    expect(memory.recent.map(item => item.key)).toEqual([
      'inline_apply:triage_detail',
      'memory.template_used',
    ]);
  });
});

describe('记忆写操作（V2-01）', () => {
  it('addMemoryPhrase：置顶、幂等、超限淘汰最旧', () => {
    let memory = emptyAssistantMemory();
    memory = addMemoryPhrase(memory, ' 申请 VPN ');
    expect(memory.phrases).toEqual(['申请 VPN']);
    expect(addMemoryPhrase(memory, '申请 VPN').phrases).toEqual(['申请 VPN']);

    for (let i = 0; i < MEMORY_MAX_PHRASES + 5; i += 1) {
      memory = addMemoryPhrase(memory, `词 ${i}`);
    }
    expect(memory.phrases).toHaveLength(MEMORY_MAX_PHRASES);
    expect(memory.phrases[0]).toBe(`词 ${MEMORY_MAX_PHRASES + 4}`);
    expect(memory.phrases).not.toContain('申请 VPN');
  });

  it('upsertMemoryTemplate：同名覆盖并置顶；removeMemoryTemplate 删除', () => {
    let memory = upsertMemoryTemplate(emptyAssistantMemory(), '网络故障', '旧内容');
    memory = upsertMemoryTemplate(memory, '账号问题', '无法登录');
    memory = upsertMemoryTemplate(memory, '网络故障', '新内容');
    expect(memory.templates.map(t => t.name)).toEqual(['网络故障', '账号问题']);
    expect(memory.templates[0].body).toBe('新内容');
    memory = removeMemoryTemplate(memory, '网络故障');
    expect(memory.templates.map(t => t.name)).toEqual(['账号问题']);
  });

  it('removeMemoryPhrase 删除指定常用语', () => {
    const memory = ['a', 'b'].reduce((acc, item) => addMemoryPhrase(acc, item), emptyAssistantMemory());
    expect(removeMemoryPhrase(memory, 'a').phrases).toEqual(['b']);
  });

  it('recordRecentAction：置顶去重 + 非法键（含正文）静默忽略', () => {
    let memory = recordRecentAction(
      emptyAssistantMemory(),
      'inline_apply:triage_create',
      '2026-10-18T09:00:00.000Z'
    );
    memory = recordRecentAction(memory, 'memory.phrase_used', '2026-10-18T10:00:00.000Z');
    memory = recordRecentAction(memory, 'inline_apply:triage_create', '2026-10-18T11:00:00.000Z');
    expect(memory.recent.map(item => item.key)).toEqual([
      'inline_apply:triage_create',
      'memory.phrase_used',
    ]);
    expect(memory.recent[0].at).toBe('2026-10-18T11:00:00.000Z');

    const before = JSON.stringify(memory);
    expect(recordRecentAction(memory, '管理员说把工单转给二线')).toBe(memory);
    expect(recordRecentAction(memory, 'bad key with spaces', '2026-10-18T10:00:00.000Z')).toBe(memory);
    expect(recordRecentAction(memory, 'ok.key', 'bad-date')).toBe(memory);
    expect(JSON.stringify(memory)).toBe(before);
  });

  it('isMemoryEmpty / recentActionLabel：未知键原样展示不伪造语义', () => {
    expect(isMemoryEmpty(emptyAssistantMemory())).toBe(true);
    expect(isMemoryEmpty(addMemoryPhrase(emptyAssistantMemory(), 'x'))).toBe(false);
    expect(recentActionLabel('inline_apply:triage_detail')).toContain('工单详情');
    expect(recentActionLabel('unknown.scope')).toBe('unknown.scope');
  });
});