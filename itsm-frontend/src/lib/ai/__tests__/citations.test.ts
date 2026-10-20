/**
 * I0-04 引用通道测试：来源目标解析、正文 `【文档n】` → 锚点、开发态路径剥离。
 */
import {
  citationTarget,
  linkifyCitationMarkers,
  objectDetailRoute,
  stripDocPath,
} from '../citations';
import type { RagAnswer } from '@/lib/api/ai-api';

const helpSource: RagAnswer = {
  objectType: 'help_doc',
  id: 1,
  title: '创建事件',
  source: 'docs/help/incidents/create.md',
  snippet: '先在事件页点击新建。',
};

const kbSource: RagAnswer = {
  objectType: 'kb',
  id: 42,
  title: 'VPN 排查指南',
  snippet: '先检查客户端版本。',
};

describe('stripDocPath', () => {
  it('剥离 docs/help 前缀与 .md 后缀', () => {
    expect(stripDocPath('docs/help/incidents/create.md')).toBe('incidents/create');
    expect(stripDocPath('帮助文档')).toBe('帮助文档');
    expect(stripDocPath('')).toBe('');
  });
});

describe('objectDetailRoute', () => {
  it('已核实路由才返回，未知类型/缺 id 返回 undefined', () => {
    expect(objectDetailRoute('ticket', 7)).toBe('/tickets/7');
    expect(objectDetailRoute('kb', 42)).toBe('/knowledge/articles/42');
    expect(objectDetailRoute('unknown', 1)).toBeUndefined();
    expect(objectDetailRoute('ticket', undefined)).toBeUndefined();
  });
});

describe('citationTarget', () => {
  it('帮助文档 → 只读文档详情，展示名不含开发路径', () => {
    const target = citationTarget(helpSource, 0);
    expect(target.href).toBe('/ai/help-doc?path=docs%2Fhelp%2Fincidents%2Fcreate.md');
    expect(target.displayName).toBe('创建事件');
    expect(target.displayName).not.toContain('.md');
  });

  it('知识库 → 文章详情', () => {
    expect(citationTarget(kbSource, 0)).toMatchObject({
      href: '/knowledge/articles/42',
      displayName: 'VPN 排查指南',
    });
  });
});

describe('linkifyCitationMarkers', () => {
  it('把【文档n】转为指向对应来源的 Markdown 链接', () => {
    const linked = linkifyCitationMarkers('先创建事件【文档1】，再补影响面【文档2】。', [
      helpSource,
      { ...helpSource, id: 2, title: '事件详情', source: 'docs/help/incidents/detail.md' },
    ]);

    expect(linked).toContain('[【文档1】](/ai/help-doc?path=docs%2Fhelp%2Fincidents%2Fcreate.md');
    expect(linked).toContain('[【文档2】](/ai/help-doc?path=docs%2Fhelp%2Fincidents%2Fdetail.md');
  });

  it('序号越界 / 无 sources 时保持原文（不臆造锚点）', () => {
    expect(linkifyCitationMarkers('结论【文档3】', [helpSource])).toBe('结论【文档3】');
    expect(linkifyCitationMarkers('结论【文档1】')).toBe('结论【文档1】');
  });

  it('代码块与行内 code 内的标记不改写', () => {
    const content = ['```', '【文档1】', '```', '', '命令 `【文档1】` 保留'].join('\n');
    const linked = linkifyCitationMarkers(content, [helpSource]);
    expect(linked).toContain('```\n【文档1】\n```');
    expect(linked).toContain('`【文档1】`');
  });
});
