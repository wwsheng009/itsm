import { Editor } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';

import { extractStagedImageIds, stripStagedImages } from '@/lib/rich-text/staged-images';

import { RichTextEditorResizableImage } from '../RichTextEditorResizableImage';

const createEditor = (content: string) =>
  new Editor({
    extensions: [StarterKit, RichTextEditorResizableImage],
    content,
  });

describe('RichTextEditorResizableImage', () => {
  it('解析 width / height / data-align 属性', () => {
    const editor = createEditor(
      '<img src="/api/v1/attachments/1/content" width="320" height="180" data-align="center">'
    );
    const image = editor.state.doc.firstChild;

    expect(image?.type.name).toBe('image');
    expect(image?.attrs.width).toBe(320);
    expect(image?.attrs.height).toBe(180);
    expect(image?.attrs['data-align']).toBe('center');
    editor.destroy();
  });

  it('序列化时保留宽度与对齐（比例宽度原样透传）', () => {
    const editor = createEditor('<img src="/api/v1/attachments/1/content" width="50%" data-align="right">');
    const html = editor.getHTML();

    expect(html).toContain('width="50%"');
    expect(html).toContain('data-align="right"');
    editor.destroy();
  });

  it('未设置宽度时不输出 width 属性（保持原始尺寸）', () => {
    const editor = createEditor('<img src="/api/v1/attachments/1/content">');
    const html = editor.getHTML();

    expect(html).toContain('<img');
    expect(html).not.toContain('width=');
    editor.destroy();
  });

  it('HTML 往返保留 data-attachment-id（回归：创建页保存后图片消失）', () => {
    const editor = createEditor(
      '<img src="blob:http://localhost:3000/abc" alt="a.png" data-attachment-id="staged-1" width="320" data-align="center">'
    );
    const html = editor.getHTML();

    // 属性必须在 schema 里声明，否则 insertContent(html) → getHTML() 往返会丢标记，
    // 提交时 extractStagedImageIds 找不到暂存图，图片既不会被上传也不会被替换。
    expect(html).toContain('data-attachment-id="staged-1"');
    expect(extractStagedImageIds(html)).toEqual(['staged-1']);
    expect(stripStagedImages(html)).not.toContain('<img');

    // NodeView 渲染出的 img 同样带上标记（便于排查与端到端断言）
    expect(
      editor.view.dom.querySelector('.rte-image-node__img')?.getAttribute('data-attachment-id')
    ).toBe('staged-1');

    editor.destroy();
  });

  it('NodeView 渲染四角手柄，选中后出现选中态', () => {
    const editor = createEditor('<img src="/api/v1/attachments/1/content" width="200"><p>正文</p>');
    const node = editor.view.dom.querySelector('.rte-image-node');
    const handles = editor.view.dom.querySelectorAll('.rte-image-node__handle');

    expect(node).not.toBeNull();
    expect(handles).toHaveLength(4);
    expect(node?.querySelector('img')?.getAttribute('width')).toBe('200');

    editor.commands.setNodeSelection(0);
    expect(
      editor.view.dom.querySelector('.rte-image-node')?.classList.contains('rte-image-node--selected')
    ).toBe(true);

    // 选区移入段落文本后取消选中，手柄随之隐藏（由 CSS 控制 display）
    editor.commands.setTextSelection(2);
    expect(
      editor.view.dom.querySelector('.rte-image-node')?.classList.contains('rte-image-node--selected')
    ).toBe(false);
    editor.destroy();
  });

  it('手柄方向键按 10px 步进调整宽度并写入节点属性', () => {
    const editor = createEditor('<img src="/api/v1/attachments/1/content">');
    // jsdom 无布局，显式给出编辑区宽度，验证夹取上限逻辑不吞掉结果
    Object.defineProperty(editor.view.dom, 'clientWidth', { value: 800, configurable: true });

    const handle = editor.view.dom.querySelector('.rte-image-node__handle--se') as HTMLElement;
    expect(handle).not.toBeNull();
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));

    expect(editor.state.doc.firstChild?.attrs.width).toBe(50);
    expect(editor.getHTML()).toContain('width="50"');
    editor.destroy();
  });
});
