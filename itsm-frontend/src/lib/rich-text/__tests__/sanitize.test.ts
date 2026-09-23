import { RICH_TEXT_ALLOWED_ATTR, htmlToPlainText, isRichTextEmpty, sanitizeRichTextHtml } from '../sanitize';

describe('rich-text/sanitize：图片属性白名单', () => {
  it('保留 width/height/data-align（缩放与对齐的落库协议）', () => {
    const out = sanitizeRichTextHtml(
      '<img src="/api/v1/attachments/1/content" alt="图" width="320" height="180" data-align="center" data-attachment-id="1">'
    );
    expect(out).toContain('src="/api/v1/attachments/1/content"');
    expect(out).toContain('width="320"');
    expect(out).toContain('height="180"');
    expect(out).toContain('data-align="center"');
    expect(out).toContain('data-attachment-id="1"');
  });

  it('保留百分比宽度', () => {
    const out = sanitizeRichTextHtml('<img src="/api/v1/attachments/1/content" width="50%">');
    expect(out).toContain('width="50%"');
  });

  it('剥离内联 style 与事件属性，尺寸只能走属性协议', () => {
    const out = sanitizeRichTextHtml(
      '<img src="/api/v1/attachments/1/content" style="width:400px" onerror="alert(1)" width="400">'
    );
    expect(out).not.toContain('style=');
    expect(out).not.toContain('onerror');
    expect(out).toContain('width="400"');
  });

  it('白名单包含图片尺寸与对齐属性', () => {
    expect(RICH_TEXT_ALLOWED_ATTR).toEqual(
      expect.arrayContaining(['width', 'height', 'data-align', 'data-attachment-id'])
    );
  });

  it('保留同源 blob: 暂存图片（创建页先占位后上传，替换成正式附件地址前不能丢图）', () => {
    const out = sanitizeRichTextHtml(
      '<img src="blob:http://localhost/abc" alt="a.png" data-attachment-id="staged-1" width="320" data-align="center">'
    );
    expect(out).toContain('src="blob:http://localhost/abc"');
    expect(out).toContain('data-attachment-id="staged-1"');
    expect(out).toContain('width="320"');
  });

  it('跨源 blob: 图片仍被移除（只放行同源预览地址）', () => {
    const out = sanitizeRichTextHtml('<img src="blob:https://evil.example.com/abc" alt="x" width="10">');
    expect(out).not.toContain('<img');
  });
});

describe('rich-text/sanitize：既有行为回归', () => {
  it('外链图片（非白名单主机）整元素移除', () => {
    const out = sanitizeRichTextHtml('<p>图</p><img src="https://evil.example.com/track.png" width="10">');
    expect(out).not.toContain('<img');
  });

  it('仅含图片时视为非空', () => {
    expect(isRichTextEmpty('<img src="/api/v1/attachments/1/content">')).toBe(false);
    expect(isRichTextEmpty('<p> </p>')).toBe(true);
  });

  it('纯文本派生不包含属性文本', () => {
    expect(htmlToPlainText('<p>第一行</p><p>第二行</p>')).toBe('第一行\n第二行');
  });
});
