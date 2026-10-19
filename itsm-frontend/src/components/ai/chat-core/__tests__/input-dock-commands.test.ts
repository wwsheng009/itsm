/**
 * T5-02 解析层单测：`/` 快捷指令与 `@` 当前页对象引用的触发/过滤/替换纯函数。
 *
 * 口径（方案 §3.1「输入坞（/ 快捷指令、@ 引用当前页对象）」；T5-02 验收「解析 + 交互测试」）：
 * - 触发解析只在明确的语法上下文命中（斜杠=整串；@=值末尾且前导为边界）；
 * - 过滤大小写不敏感；替换只填不发（返回新文本，不触发发送）。
 */
import {
  SLASH_COMMANDS,
  applyReference,
  applySlashCommand,
  filterSlashCommands,
  matchesReferenceKeyword,
  parseReferenceTrigger,
  parseSlashTrigger,
  referenceDisplayText,
} from '../input-dock-commands';
import type { AssistantPageData } from '@/lib/store/assistant-context-store';

const ticketPage: AssistantPageData = {
  targetType: 'ticket',
  targetId: 42,
  summary: '打印机无法连接',
};

describe('parseSlashTrigger（斜杠触发解析）', () => {
  it('整串 `/` 命中，过滤词为空串', () => {
    expect(parseSlashTrigger('/')).toBe('');
  });

  it('`/工单` 命中，过滤词为命令名', () => {
    expect(parseSlashTrigger('/工单')).toBe('工单');
  });

  it('两侧空白被忽略（trim 后判定）', () => {
    expect(parseSlashTrigger('  /SL ')).toBe('SL');
  });

  it('不在开头 / 含空白 / 含第二个斜杠 / 空串均不触发', () => {
    expect(parseSlashTrigger('你好 /工')).toBeNull();
    expect(parseSlashTrigger('/工 单')).toBeNull();
    expect(parseSlashTrigger('/工/单')).toBeNull();
    expect(parseSlashTrigger('')).toBeNull();
  });
});

describe('filterSlashCommands（命令过滤）', () => {
  it('空过滤词返回全部命令（注册表顺序）', () => {
    expect(filterSlashCommands('').map(command => command.name)).toEqual([
      '/工单',
      '/SLA',
      '/话术',
    ]);
    expect(SLASH_COMMANDS).toHaveLength(3);
  });

  it('中文前缀过滤命中 /工单', () => {
    expect(filterSlashCommands('工').map(command => command.name)).toEqual(['/工单']);
  });

  it('ASCII 过滤大小写不敏感（sl → /SLA）', () => {
    expect(filterSlashCommands('sl').map(command => command.name)).toEqual(['/SLA']);
    expect(filterSlashCommands('SL').map(command => command.name)).toEqual(['/SLA']);
  });

  it('无匹配返回空数组', () => {
    expect(filterSlashCommands('不存在')).toEqual([]);
  });
});

describe('applySlashCommand（选中命令）', () => {
  it('触发态下整串替换为脚手架文案（只填不发）', () => {
    expect(applySlashCommand('/工', SLASH_COMMANDS[0])).toBe('总结我最近处理过的工单');
  });

  it('非触发态原样返回（防御）', () => {
    expect(applySlashCommand('已发送内容', SLASH_COMMANDS[0])).toBe('已发送内容');
  });
});

describe('parseReferenceTrigger（@ 触发解析）', () => {
  it('单独 `@` 命中，起点 0、过滤词为空', () => {
    expect(parseReferenceTrigger('@')).toEqual({ start: 0, keyword: '' });
  });

  it('空白后的 `@词` 命中，start 指向 @ 本身', () => {
    expect(parseReferenceTrigger('你好 @工')).toEqual({ start: 3, keyword: '工' });
  });

  it('连续 @ 时命中最后一个（过滤词不含 @）', () => {
    expect(parseReferenceTrigger('a @b @c')).toEqual({ start: 5, keyword: 'c' });
  });

  it('邮箱式内嵌 @ 不触发（前导必须是行首或空白）', () => {
    expect(parseReferenceTrigger('foo@bar')).toBeNull();
  });

  it('@ 后已有空白（已选过引用）不触发', () => {
    expect(parseReferenceTrigger('@工单#42 ')).toBeNull();
  });
});

describe('referenceDisplayText / matchesReferenceKeyword', () => {
  it('引用标签为 `@类型#编号`', () => {
    expect(referenceDisplayText(ticketPage)).toBe('@工单#42');
  });

  it('空过滤词恒命中；编号/摘要/类型均可命中；无关词不命中', () => {
    expect(matchesReferenceKeyword(ticketPage, '')).toBe(true);
    expect(matchesReferenceKeyword(ticketPage, '42')).toBe(true);
    expect(matchesReferenceKeyword(ticketPage, '打')).toBe(true);
    expect(matchesReferenceKeyword(ticketPage, '工单')).toBe(true);
    expect(matchesReferenceKeyword(ticketPage, 'xyz')).toBe(false);
  });
});

describe('applyReference（选中引用）', () => {
  it('触发段替换为引用标签 + 尾随空格（脱离触发态，便于续写）', () => {
    expect(applyReference('你好 @打', ticketPage)).toBe('你好 @工单#42 ');
  });

  it('仅 `@` 时同样替换', () => {
    expect(applyReference('@', ticketPage)).toBe('@工单#42 ');
  });

  it('非触发态原样返回（防御）', () => {
    expect(applyReference('没有触发词', ticketPage)).toBe('没有触发词');
  });
});
