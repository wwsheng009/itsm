/**
 * 「补充为知识文章」预填契约单测（会话页投递 / 新建页取回的唯一公共实现）。
 *
 * 覆盖：① 标题推导（标题 / 列表 / 引用 / 表格 / 代码围栏 / 图片链接 / 截断 / 兜底）
 *      ② state 形状与 round-trip ③ 非法 state 一律降级为「无预填」且不抛错。
 */
import {
  ARTICLE_PREFILL_FALLBACK_TITLE,
  ARTICLE_PREFILL_SOURCE,
  buildArticlePrefillState,
  buildConversationArticlePrefillState,
  deriveArticleTitle,
  readArticlePrefillRequest,
  readArticlePrefillState,
} from '../ai-article-prefill';

describe('deriveArticleTitle', () => {
  it('取首个非空行并剥掉 ATX 标题标记', () => {
    expect(deriveArticleTitle('# 处置结论\n\n正文内容')).toBe('处置结论');
    expect(deriveArticleTitle('### VPN 拨号失败排查指南\n\n说明')).toBe('VPN 拨号失败排查指南');
  });

  it('跳过代码围栏，取其后的普通行', () => {
    expect(deriveArticleTitle('```bash\nnpm run build\n```')).toBe('npm run build');
    // 只有围栏、没有正文 → 兜底
    expect(deriveArticleTitle('```\n```')).toBe(ARTICLE_PREFILL_FALLBACK_TITLE);
  });

  it('剥掉列表 / 引用 / 表格标记', () => {
    expect(deriveArticleTitle('- 第一步：重启服务\n- 第二步')).toBe('第一步：重启服务');
    expect(deriveArticleTitle('1. 先查日志\n2. 再查网络')).toBe('先查日志');
    expect(deriveArticleTitle('> 注意：这是引用')).toBe('注意：这是引用');
    expect(deriveArticleTitle('| 名称 | 数量 |\n| --- | --- |')).toBe('名称 数量');
  });

  it('图片整段去掉、链接只保留可见文字、行内标记剥掉', () => {
    expect(deriveArticleTitle('![截图](http://x/y.png) 现象见 [文档](http://doc)')).toBe('现象见 文档');
    expect(deriveArticleTitle('**已恢复** `systemd` 服务')).toBe('已恢复 systemd 服务');
  });

  it('空白输入返回兜底标题', () => {
    expect(deriveArticleTitle('')).toBe(ARTICLE_PREFILL_FALLBACK_TITLE);
    expect(deriveArticleTitle('\n\n   \n')).toBe(ARTICLE_PREFILL_FALLBACK_TITLE);
  });

  it('超长标题按上限截断并补省略号', () => {
    expect(deriveArticleTitle('A'.repeat(90))).toBe(`${'A'.repeat(80)}…`);
    expect(deriveArticleTitle('B'.repeat(10), 4)).toBe('BBBB…');
  });
});

describe('buildArticlePrefillState / readArticlePrefillState', () => {
  const markdown = '# 处理步骤\n\n1. 先查日志\n2. 再查网络\n';

  it('round-trip：会话页投递的 state 能被新建页原样读回（含结构化克隆）', () => {
    const state = buildArticlePrefillState(markdown);
    expect(state.source).toBe(ARTICLE_PREFILL_SOURCE);
    expect(state.prefill).toEqual({ title: '处理步骤', content: markdown });

    const expected = { title: '处理步骤', content: markdown };
    expect(readArticlePrefillState(state)).toEqual(expected);
    // history.state 会经历结构化克隆（等价 JSON 化），读取侧必须照样能取回。
    expect(readArticlePrefillState(JSON.parse(JSON.stringify(state)))).toEqual(expected);
  });

  it('非对象 / 来源不符 / 缺正文 / 正文非字符串 → null', () => {
    expect(readArticlePrefillState(null)).toBeNull();
    expect(readArticlePrefillState(undefined)).toBeNull();
    expect(readArticlePrefillState('ai-chat')).toBeNull();
    expect(readArticlePrefillState(42)).toBeNull();
    expect(readArticlePrefillState({})).toBeNull();
    expect(readArticlePrefillState({ source: 'other', prefill: { content: 'x' } })).toBeNull();
    expect(readArticlePrefillState({ source: ARTICLE_PREFILL_SOURCE })).toBeNull();
    expect(readArticlePrefillState({ source: ARTICLE_PREFILL_SOURCE, prefill: { content: 123 } })).toBeNull();
  });

  it('正文为空白 → null（不产生一张「有预填」的空表单）', () => {
    expect(readArticlePrefillState({ source: ARTICLE_PREFILL_SOURCE, prefill: { content: '   \n' } })).toBeNull();
  });

  it('标题缺失 / 空白时按正文重新推导', () => {
    const content = '# 兜底推导标题\n正文';
    for (const title of [undefined, '', '   ']) {
      expect(readArticlePrefillState({ source: ARTICLE_PREFILL_SOURCE, prefill: { title, content } })).toEqual({
        title: '兜底推导标题',
        content,
      });
    }
  });
});

describe('会话级 state（标题栏「保存成文章」）', () => {
  it('buildConversationArticlePrefillState 只携带会话 ID，不内联会话正文', () => {
    const state = buildConversationArticlePrefillState(3);
    expect(state).toEqual({ source: ARTICLE_PREFILL_SOURCE, conversation: { id: 3 } });
    expect(JSON.stringify(state)).not.toContain('content');
  });

  it('readArticlePrefillRequest 区分内联回答与会话两种形态', () => {
    const markdown = '# 处理步骤\n正文';
    expect(readArticlePrefillRequest(buildArticlePrefillState(markdown))).toEqual({
      kind: 'answer',
      prefill: { title: '处理步骤', content: markdown },
    });
    // history.state 的结构化克隆（等价 JSON 化）后仍要能读回
    expect(
      readArticlePrefillRequest(JSON.parse(JSON.stringify(buildConversationArticlePrefillState(12))))
    ).toEqual({ kind: 'conversation', conversationId: 12 });
  });

  it('会话 ID 非法（0 / 负数 / 小数 / 非数字）→ 视为无预填', () => {
    for (const id of [0, -1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, '3', null]) {
      expect(
        readArticlePrefillRequest({ source: ARTICLE_PREFILL_SOURCE, conversation: { id } })
      ).toBeNull();
    }
    expect(readArticlePrefillRequest({ source: ARTICLE_PREFILL_SOURCE, conversation: 3 })).toBeNull();
  });

  it('兼容读取器在会话态返回 null（新页面一律走 readArticlePrefillRequest）', () => {
    expect(readArticlePrefillState(buildConversationArticlePrefillState(3))).toBeNull();
  });
});
