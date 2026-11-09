/**
 * ArticleContentRenderer：知识库正文「按内容类型分发渲染」回归。
 *
 * 报障背景：`/knowledge/articles/3` 这类 Markdown 文章在详情页把 `## 标题`、`| 表格 |`
 * 原样暴露——历史实现只有「HTML / 非 HTML」两态（`isHtmlContent` 启发式），Markdown 无渲染支路。
 * 本文件锁定四条分支的真实 DOM 结果（解析链走真实依赖，未 mock react-markdown）：
 * - markdown：标题 / 加粗 / GFM 表格被解析，语法定界符不再泄漏为字面量；
 *   样式锁定：正文落在 MarkdownMessage 的作用域容器（.ai-md + 表格滚动包装），
 *   不得回退到 Tailwind `prose`（项目未装 typography 插件，该 class 无任何规则）；
 * - text    ：原样保留换行与标记字符（不解析任何语法）；
 * - html    ：净化后按 HTML 渲染（脚本 / 事件属性不入 DOM）；
 * - rich_text：同为 HTML，但保留 `data-attachment-id` 站内图片引用锚点。
 */
import React from 'react';
import { render, screen } from '@testing-library/react';

import ArticleContentRenderer from '../ArticleContentRenderer';

/** 与工单 id=3 同构的 Markdown 样本（标题 + 加粗 + GFM 表格 + 行内代码）。 */
const MARKDOWN = [
  '## 一、关于「待审批」',
  '',
  '**结论**：暂无独立状态字段。',
  '',
  '| 状态 | 数量 |',
  '| --- | --- |',
  '| `new` | 9 |',
  '| `approved` | 1 |',
  '',
].join('\n');

describe('ArticleContentRenderer：按内容类型分发渲染', () => {
  it('markdown：解析标题 / 加粗 / GFM 表格，语法记号不出现在渲染结果中', () => {
    const { container } = render(
      <ArticleContentRenderer content={MARKDOWN} contentType="markdown" />
    );

    expect(container.querySelector('h2')?.textContent).toBe('一、关于「待审批」');
    expect(container.querySelector('strong')?.textContent).toBe('结论');
    expect(container.querySelectorAll('thead th')).toHaveLength(2);
    expect(Array.from(container.querySelectorAll('tbody td')).map(td => td.textContent)).toEqual([
      'new',
      '9',
      'approved',
      '1',
    ]);
    expect(container.querySelector('td code')?.textContent).toBe('new');

    const text = container.textContent || '';
    expect(text).not.toContain('##');
    expect(text).not.toContain('**');
    expect(text).not.toContain('| --- |');

    // 样式口径：Markdown 分支复用 MarkdownMessage 的作用域样式（.ai-md）。
    // 回归点：曾用 `prose max-w-none` 包裹，项目未安装 @tailwindcss/typography，
    // 该 class 无规则 + preflight 清零 ⇒ 标题与段落粘连、表格无框无内边距、列表丢符号。
    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toContain('ai-md');
    expect(root.className).not.toContain('prose');
    expect(root.querySelector('style')?.textContent).toContain('.ai-md');
    // GFM 表格进入横向滚动容器（窄屏不撑破正文列），与 AI 回答同一渲染口径。
    expect(container.querySelector('.ai-md-tablewrap > table')).not.toBeNull();
  });

  it('markdown：内嵌 HTML 图片按图片渲染，并保留服务端绝对地址', () => {
    const { container } = render(
      <ArticleContentRenderer
        content={
          '## 图文混排\n\n**Markdown 语法仍然生效**\n\n<img src="http://172.18.3.238:8089/api/v1/attachments/32/content" alt="截图" width="640" data-itsm-width="50">'
        }
        contentType="markdown"
      />
    );

    // Markdown 与内嵌 HTML 图片可以在同一篇正文里同时渲染。
    expect(container.querySelector('h2')?.textContent).toBe('图文混排');
    expect(container.querySelector('strong')?.textContent).toBe('Markdown 语法仍然生效');
    const img = container.querySelector('img');
    expect(img).not.toBeNull();
    // 客户端发布后写入的是服务端绝对地址，必须保留。
    expect(img?.getAttribute('src')).toBe(
      'http://172.18.3.238:8089/api/v1/attachments/32/content'
    );
    expect(img?.getAttribute('alt')).toBe('截图');
    expect(img?.getAttribute('width')).toBe('640');
    // 编辑器私有属性不属于服务端渲染契约，允许被白名单剥离。
    expect(img?.getAttribute('data-itsm-width')).toBeNull();
  });

  it('markdown：内嵌 HTML 仍走白名单净化，脚本与事件属性不进入 DOM', () => {
    const { container } = render(
      <ArticleContentRenderer
        content={
          '<img src="/api/v1/attachments/32/content" onerror="alert(1)"><script>alert(1)</script>'
        }
        contentType="markdown"
      />
    );

    expect(container.querySelector('img')?.getAttribute('src')).toBe(
      '/api/v1/attachments/32/content'
    );
    expect(container.querySelectorAll('[onerror]')).toHaveLength(0);
    expect(container.querySelector('script')).toBeNull();
  });

  it('text：保留换行与标记字符，不解析 Markdown', () => {
    const { container } = render(
      <ArticleContentRenderer content={'## 不是标题\n\n| 原样 | 文本 |'} contentType="text" />
    );

    expect(container.querySelector('h2')).toBeNull();
    expect(container.querySelector('table')).toBeNull();
    expect(container.textContent).toContain('## 不是标题');
    expect(container.textContent).toContain('| 原样 | 文本 |');
    expect((container.firstElementChild as HTMLElement).className).toContain('whitespace-pre-wrap');
  });

  it('html：按 HTML 语义渲染，但脚本与事件属性被净化剥离', () => {
    const { container } = render(
      <ArticleContentRenderer
        content={'<h3>导入标题</h3><p>正文</p><script>alert(1)</script><img src="x" onerror="alert(1)">'}
        contentType="html"
      />
    );

    expect(container.querySelector('h3')?.textContent).toBe('导入标题');
    expect(container.querySelector('p')?.textContent).toBe('正文');
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelectorAll('[onerror]')).toHaveLength(0);
  });

  it('rich_text：保留 data-attachment-id 站内图片引用（与附件引用保护同一锚点）', () => {
    const { container } = render(
      <ArticleContentRenderer
        content={'<p>图文</p><img src="/api/v1/attachments/12/preview" data-attachment-id="12" alt="拓扑" />'}
        contentType="rich_text"
      />
    );

    const img = container.querySelector('img[data-attachment-id="12"]');
    expect(img).not.toBeNull();
    expect(img?.getAttribute('src')).toBe('/api/v1/attachments/12/preview');
    expect(img?.getAttribute('alt')).toBe('拓扑');
  });

  it('空内容：渲染占位文案而非空容器', () => {
    render(<ArticleContentRenderer content={'   '} contentType="markdown" emptyText="本文暂无内容。" />);

    expect(screen.getByText('本文暂无内容。')).toBeTruthy();
  });

  it('空内容也可传入自定义占位节点', () => {
    render(<ArticleContentRenderer content={null} contentType="text" emptyText={<span>暂无</span>} />);

    expect(screen.getByText('暂无')).toBeTruthy();
  });
});
