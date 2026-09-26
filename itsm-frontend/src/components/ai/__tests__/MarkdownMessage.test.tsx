/**
 * MarkdownMessage 渲染层测试（jsdom + @testing-library/react）。
 *
 * 解析链走真实依赖：`react-markdown` + `remark-gfm` + `rehype-sanitize`
 *（jest.config.js 的 `transformIgnorePatterns` 已放行这批纯 ESM 包）。
 *
 * 覆盖：① 标题/加粗/列表 ② GFM 表格 ③ 代码块语言标签与复制按钮
 *      ④ sanitize（脚本/事件属性不入 DOM）⑤ streaming 光标。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import MarkdownMessage from '../MarkdownMessage';

const writeText = jest.fn();

describe('MarkdownMessage', () => {
  beforeEach(() => {
    // jsdom 无 navigator.clipboard：按需打桩。
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    });
  });

  it('渲染 Markdown 标题 / 加粗 / 列表', () => {
    const { container } = render(
      <MarkdownMessage content={'# 处置结论\n\n**已恢复** 服务\n\n- 第一步\n- 第二步\n'} />
    );

    expect(container.querySelector('h1')?.textContent).toBe('处置结论');
    expect(container.querySelector('strong')?.textContent).toBe('已恢复');
    expect(container.querySelectorAll('li')).toHaveLength(2);
  });

  it('渲染 GFM 表格，并包在可横向滚动的容器内', () => {
    const { container } = render(
      <MarkdownMessage content={'| 名称 | 数量 |\n| --- | --- |\n| A | 1 |\n'} />
    );

    expect(container.querySelector('table')).not.toBeNull();
    expect(container.querySelectorAll('th')).toHaveLength(2);
    expect(container.querySelectorAll('td')).toHaveLength(2);
    expect(container.querySelector('.ai-md-tablewrap')).not.toBeNull();
  });

  it('代码块：<pre><code> + 语言标签 + 复制按钮写入剪贴板', async () => {
    const { container } = render(
      <MarkdownMessage content={'示例：\n\n```ts\nconst a = 1;\n```\n'} />
    );

    expect(container.querySelector('pre code')).not.toBeNull();
    // react-markdown 会在代码块文本末尾保留一个换行，断言时忽略首尾空白。
    expect(container.querySelector('pre code')?.textContent?.trim()).toBe('const a = 1;');
    // 语言标签从 className="language-ts" 解析
    expect(screen.getByText('ts')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '复制代码' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('const a = 1;'));
    await waitFor(() => expect(screen.getByText('已复制')).toBeTruthy());
  });

  it('行内 code 走胶囊样式且不套 <pre>', () => {
    const { container } = render(<MarkdownMessage content={'执行 `npm run dev` 启动'} />);

    const inline = container.querySelector('p code');
    expect(inline).not.toBeNull();
    expect(inline?.textContent).toBe('npm run dev');
    expect(container.querySelector('pre code')).toBeNull();
  });

  it('sanitize：脚本与事件属性不进入 DOM', () => {
    const { container } = render(
      <MarkdownMessage
        content={
          '<script>alert(1)</script>\n\n<img src=x onerror="alert(1)">\n\n[看这里](javascript:alert(1))\n'
        }
      />
    );

    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelectorAll('[onerror]')).toHaveLength(0);
    expect(container.querySelector('img[src="x"]')).toBeNull();
    expect(container.querySelector('a')?.getAttribute('href') || '').not.toMatch(/javascript:/i);
  });

  it('streaming=true 时在内容末尾渲染闪烁光标', () => {
    const { container, rerender } = render(<MarkdownMessage content={'生成中'} streaming />);
    expect(container.querySelector('.ai-md-caret')).not.toBeNull();

    rerender(<MarkdownMessage content={'生成结束'} />);
    expect(container.querySelector('.ai-md-caret')).toBeNull();
  });
});
