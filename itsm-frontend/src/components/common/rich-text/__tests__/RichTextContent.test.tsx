/**
 * 富文本只读渲染组件（单字段范式双读）。
 *
 * 覆盖：HTML 走净化后渲染（脚本/事件属性被剥离、站内图片保留 data-attachment-id）、
 * 历史纯文本按原样输出（不当作 HTML 解析）、空值占位、图片查看器开关。
 */
import React from 'react';
import { render, screen } from '@testing-library/react';
import RichTextContent from '../RichTextContent';

describe('RichTextContent：HTML / 纯文本双读', () => {
  it('HTML 正文经净化后渲染，脚本与事件属性被剥离', () => {
    const { container } = render(
      <RichTextContent
        content={'<p onclick="alert(1)">正文<img src="/api/v1/attachments/1/content" data-attachment-id="1"></p><script>alert(2)</script>'}
      />
    );

    expect(container.querySelector('p')).not.toBeNull();
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelector('p')?.getAttribute('onclick')).toBeNull();
    expect(container.querySelector('img')?.getAttribute('data-attachment-id')).toBe('1');
  });

  it('历史纯文本原样输出，不解析 HTML 且保留换行语义', () => {
    const { container } = render(<RichTextContent content={'第一行\n<b>不是标签</b>'} />);

    expect(container.textContent).toBe('第一行\n<b>不是标签</b>');
    expect(container.querySelector('b')).toBeNull();
  });

  it('空值走占位文案，可自定义', () => {
    const { rerender } = render(<RichTextContent content={null} />);
    expect(screen.getByText('-')).toBeTruthy();

    rerender(<RichTextContent content="" emptyText="未填写" />);
    expect(screen.getByText('未填写')).toBeTruthy();
  });

  it('关闭图片查看器时不接管容器内图片', () => {
    const twoImages = '<img src="/api/v1/attachments/1/content" alt="图一"><img src="/api/v1/attachments/2/content" alt="图二">';
    const { container, rerender } = render(
      <RichTextContent content={twoImages} enableImageViewer={false} />
    );
    expect(container.querySelectorAll('img')[0].getAttribute('role')).toBeNull();

    rerender(<RichTextContent content={twoImages} enableImageViewer />);
    expect(container.querySelectorAll('img')[0].getAttribute('role')).toBe('button');
  });
});
