/**
 * 详情页富文本图片查看器（§4.9）组件测试。
 *
 * 覆盖用户可见的核心诉求：详情页回显的图片能点开、能放大/缩小/旋转/翻转、
 * 多图可切换、键盘与遮罩都能退出，且关闭后焦点回到被点的图片。
 * 富文本编辑器相关回归由 RichTextEditorImageMenu / staged-images 套件负责。
 */
import React, { useRef } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import RichTextImageViewer from '../RichTextImageViewer';

function Harness({ html, enabled = true }: { html: string; enabled?: boolean }) {
  const ref = useRef<HTMLDivElement | null>(null);
  return (
    <div>
      <div
        ref={ref}
        className="ticket-rich-text"
        dangerouslySetInnerHTML={{ __html: html }}
      />
      <RichTextImageViewer containerRef={ref} enabled={enabled} />
    </div>
  );
}

const ONE_IMAGE = '<p>看这张图</p><img src="/api/attachments/1/content" alt="截图" width="162" />';
const TWO_IMAGES =
  '<img src="/api/attachments/1/content" alt="第一张" /><img src="/api/attachments/2/content" alt="第二张" />';

function thumbnail(name: RegExp | string) {
  return screen.getByRole('button', { name });
}

function viewerImage(): HTMLImageElement {
  return screen.getByRole('img') as HTMLImageElement;
}

function stage(): HTMLElement {
  return screen.getByRole('dialog').querySelector('.rte-image-viewer__stage') as HTMLElement;
}

function openViewer(html: string = ONE_IMAGE, trigger: RegExp = /查看图片/) {
  render(<Harness html={html} />);
  fireEvent.click(thumbnail(trigger));
  return screen.getByRole('dialog');
}

describe('RichTextImageViewer', () => {
  it('给正文图片补齐键盘可达性提示，点击后打开查看器', () => {
    render(<Harness html={ONE_IMAGE} />);
    const thumb = thumbnail(/查看图片：截图/);
    expect(thumb).toHaveAttribute('tabindex', '0');
    expect(thumb).toHaveAttribute('title', '点击查看大图（可缩放 / 旋转 / 翻转）');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    fireEvent.click(thumb);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('100%')).toBeInTheDocument();
  });

  it('回车 / 空格同样可打开（键盘用户）', () => {
    render(<Harness html={ONE_IMAGE} />);
    fireEvent.keyDown(thumbnail(/查看图片/), { key: 'Enter' });
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    fireEvent.keyDown(thumbnail(/查看图片/), { key: ' ' });
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('放大 / 缩小按 1.25 倍步进，且不会越过 100% 以下的原始语义', () => {
    openViewer();
    fireEvent.click(screen.getByRole('button', { name: '放大' }));
    expect(screen.getByText('125%')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '缩小' }));
    expect(screen.getByText('100%')).toBeInTheDocument();
  });

  it('滚轮与双击可缩放，适应窗口会回到 100%', () => {
    openViewer();
    fireEvent.wheel(stage(), { deltaY: -120 });
    expect(screen.getByText('125%')).toBeInTheDocument();

    fireEvent.doubleClick(stage());
    expect(screen.getByText('100%')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '放大' }));
    fireEvent.click(screen.getByRole('button', { name: '适应窗口' }));
    expect(screen.getByText('100%')).toBeInTheDocument();
  });

  it('旋转与翻转写入 transform，并可用重置恢复', () => {
    openViewer();
    fireEvent.click(screen.getByRole('button', { name: '向右旋转' }));
    expect(viewerImage().style.transform).toContain('rotate(90deg)');

    fireEvent.click(screen.getByRole('button', { name: '向左旋转' }));
    expect(viewerImage().style.transform).toContain('rotate(0deg)');

    fireEvent.click(screen.getByRole('button', { name: '水平翻转' }));
    expect(viewerImage().style.transform).toContain('scaleX(-1)');
    fireEvent.click(screen.getByRole('button', { name: '垂直翻转' }));
    expect(viewerImage().style.transform).toContain('scaleY(-1)');

    fireEvent.click(screen.getByRole('button', { name: '重置' }));
    expect(viewerImage().style.transform).toContain('rotate(0deg)');
    expect(viewerImage().style.transform).toContain('scaleX(1)');
    expect(viewerImage().style.transform).toContain('scaleY(1)');
  });

  it('多图时计数正确、可前后切换', () => {
    openViewer(TWO_IMAGES, /查看图片：第一张/);
    expect(screen.getByText('1 / 2')).toBeInTheDocument();
    expect(viewerImage().getAttribute('src')).toBe('/api/attachments/1/content');

    fireEvent.click(screen.getByRole('button', { name: '下一张' }));
    expect(screen.getByText('2 / 2')).toBeInTheDocument();
    expect(viewerImage().getAttribute('src')).toBe('/api/attachments/2/content');

    fireEvent.keyDown(window, { key: 'ArrowRight' });
    expect(screen.getByText('1 / 2')).toBeInTheDocument();

    fireEvent.keyDown(window, { key: 'ArrowLeft' });
    expect(screen.getByText('2 / 2')).toBeInTheDocument();
  });

  it('单图时不显示切换按钮与计数', () => {
    openViewer();
    expect(screen.queryByRole('button', { name: '下一张' })).not.toBeInTheDocument();
    expect(screen.queryByText(/\/ 2/)).not.toBeInTheDocument();
  });

  it('Esc / 点击遮罩 / 关闭按钮都能退出，并把焦点还给触发的图片', () => {
    openViewer();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(thumbnail(/查看图片/)).toHaveFocus();

    fireEvent.click(thumbnail(/查看图片/));
    fireEvent.mouseDown(screen.getByRole('dialog'));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    fireEvent.click(thumbnail(/查看图片/));
    fireEvent.click(screen.getByRole('button', { name: '关闭' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('关闭后恢复页面滚动', () => {
    openViewer();
    expect(document.body.style.overflow).toBe('hidden');
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(document.body.style.overflow).toBe('');
  });

  it('enabled=false（纯文本回退）时不接管图片点击', () => {
    render(<Harness html={ONE_IMAGE} enabled={false} />);
    const img = document.querySelector('.ticket-rich-text img') as HTMLImageElement;
    fireEvent.click(img);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
