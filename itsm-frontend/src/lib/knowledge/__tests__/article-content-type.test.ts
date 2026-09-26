/**
 * 正文类型模型单测：类型词汇、别名归一、历史数据兜底判定与渲染分发口径。
 *
 * 这些断言锁定前后端共享的约定（与 itsm-backend/common/knowledgecontent 一致），
 * 防止「Markdown 文章被当 HTML 吞掉」或反过来的渲染回归。
 */

import {
  ARTICLE_CONTENT_TYPE_LABELS,
  detectArticleContentType,
  editorKindForContentType,
  normalizeArticleContentType,
  resolveArticleContentType,
} from '../article-content-type';

describe('normalizeArticleContentType', () => {
  it('识别四种标准类型', () => {
    expect(normalizeArticleContentType('text')).toBe('text');
    expect(normalizeArticleContentType('markdown')).toBe('markdown');
    expect(normalizeArticleContentType('html')).toBe('html');
    expect(normalizeArticleContentType('rich_text')).toBe('rich_text');
  });

  it('兼容别名与大小写 / 空白', () => {
    expect(normalizeArticleContentType('MD')).toBe('markdown');
    expect(normalizeArticleContentType(' rich-text ')).toBe('rich_text');
    expect(normalizeArticleContentType('RichText')).toBe('rich_text');
    expect(normalizeArticleContentType('plain')).toBe('text');
  });

  it('空值与未知值返回 null（由调用方兜底）', () => {
    expect(normalizeArticleContentType('')).toBeNull();
    expect(normalizeArticleContentType('   ')).toBeNull();
    expect(normalizeArticleContentType(undefined)).toBeNull();
    expect(normalizeArticleContentType(null)).toBeNull();
    expect(normalizeArticleContentType('pdf')).toBeNull();
  });
});

describe('detectArticleContentType', () => {
  it('空内容判定为纯文本', () => {
    expect(detectArticleContentType('')).toBe('text');
    expect(detectArticleContentType('   \n ')).toBe('text');
    expect(detectArticleContentType(undefined)).toBe('text');
  });

  it('Markdown 原文判定为 markdown（不再整段暴露 ## / 表格语法）', () => {
    expect(detectArticleContentType('## 标题\n\n- 列表项')).toBe('markdown');
    expect(detectArticleContentType('| a | b |\n| - | - |\n| 1 | 2 |')).toBe('markdown');
  });

  it('仅含内联标签的文本仍按 markdown 处理', () => {
    expect(detectArticleContentType('第一行<br>第二行')).toBe('markdown');
  });

  it('块级 / 结构标签判定为 rich_text', () => {
    expect(detectArticleContentType('<p>hello</p>')).toBe('rich_text');
    expect(detectArticleContentType('<table><tr><td>x</td></tr></table>')).toBe('rich_text');
    expect(detectArticleContentType('<img src="/a.png" data-attachment-id="1">')).toBe(
      'rich_text'
    );
  });
});

describe('resolveArticleContentType', () => {
  it('显式类型优先于内容形态', () => {
    expect(
      resolveArticleContentType({ contentType: 'markdown', content: '<p>看起来像 HTML</p>' })
    ).toBe('markdown');
    expect(
      resolveArticleContentType({ contentType: 'rich_text', content: '## 看起来像 Markdown' })
    ).toBe('rich_text');
  });

  it('缺失 / 非法类型回退到内容形态判定（历史数据无回归）', () => {
    expect(resolveArticleContentType({ content: '<p>hello</p>' })).toBe('rich_text');
    expect(resolveArticleContentType({ contentType: 'legacy', content: '## 标题' })).toBe(
      'markdown'
    );
    expect(resolveArticleContentType({})).toBe('text');
    expect(resolveArticleContentType(null)).toBe('text');
  });
});

describe('editorKindForContentType', () => {
  it('仅 rich_text 走富文本编辑器', () => {
    expect(editorKindForContentType('rich_text')).toBe('rich');
  });

  it('markdown / text / html 走纯文本输入域（避免丢语义 / 洗掉结构）', () => {
    expect(editorKindForContentType('markdown')).toBe('plain');
    expect(editorKindForContentType('text')).toBe('plain');
    expect(editorKindForContentType('html')).toBe('plain');
  });
});

describe('类型展示文案', () => {
  it('四种类型都有中文标签', () => {
    (['text', 'markdown', 'html', 'rich_text'] as const).forEach(type => {
      expect(ARTICLE_CONTENT_TYPE_LABELS[type]).toBeTruthy();
    });
  });
});
