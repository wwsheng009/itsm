/**
 * `isHtmlContent` 形态判定用例（FE-5）。
 *
 * 判定错误的两类代价：
 * - 漏判（HTML 当 Markdown）：`<p>` 被原样显示成标签文本、图片不渲染；
 * - 误判（Markdown 当 HTML）：整篇 Markdown 进富文本编辑器，标题 / 列表语义丢失。
 * 因此正例覆盖编辑器实际产物（段落 / 列表 / 图片 / 表格），反例覆盖常见 Markdown。
 */
import { isHtmlContent } from '../content-format';

describe('isHtmlContent', () => {
  it.each([
    ['段落', '<p>问题描述</p>'],
    ['标题', '<h2>排查步骤</h2>'],
    ['无序列表', '<ul><li>第一步</li></ul>'],
    ['有序列表', '<ol><li>第一步</li></ol>'],
    ['引用', '<blockquote>注意</blockquote>'],
    ['代码块', '<pre><code>ping 10.0.0.1</code></pre>'],
    ['表格', '<table><tbody><tr><td>a</td></tr></tbody></table>'],
    ['图片', '<img src="/api/v1/knowledge/articles/1/attachments/2/preview" data-attachment-id="2" />'],
    ['分隔线', '<hr />'],
    ['带属性的块级标签', '<p class="a" style="margin:0">x</p>'],
    ['前后有空白的内容', '\n  <div>正文</div>\n'],
  ])('识别富文本 HTML：%s', (_name, html) => {
    expect(isHtmlContent(html)).toBe(true);
  });

  it.each([
    ['纯文本', 'VPN 拨号失败时请先检查账号状态'],
    ['Markdown 标题', '# 问题描述\n\n正文'],
    ['Markdown 列表', '- 第一步\n- 第二步'],
    ['Markdown 图片', '![拓扑图](/api/v1/attachments/9/content)'],
    ['Markdown 表格', '| 列 | 值 |\n| --- | --- |\n| a | 1 |'],
    ['仅内联标签', '这是 <strong>重点</strong> 内容'],
    ['仅换行标签', '第一行<br/>第二行'],
    ['仅链接', '参考 <a href="/knowledge">知识库</a>'],
    ['尖括号伪标签', '延迟 < 100ms'],
  ])('不误判为 HTML：%s', (_name, content) => {
    expect(isHtmlContent(content)).toBe(false);
  });

  it('空值一律按非 HTML 处理', () => {
    expect(isHtmlContent('')).toBe(false);
    expect(isHtmlContent(undefined)).toBe(false);
    expect(isHtmlContent(null)).toBe(false);
  });
});
