/**
 * P0-2 回归：RichTextEditor 图片上传 fail-fast（方案 D6 / AC-8）。
 *
 * 背景：组件此前内置 `defaultUploadImage`，会向**未注册**的
 * `POST /api/v1/attachments/upload` 发起请求，产生静默 404。
 * v1.0 决策：删除该断链默认实现——未注入 `onUploadImage` 时
 *  1. 插入图片按钮禁用（tooltip 说明原因）；
 *  2. 粘贴/拖拽图片被拦截并告警，**不发起任何请求**（fetch / XHR 均不得触发）；
 * 注入上传实现后恢复原有能力（粘贴 → 上传 → 插入带 data-attachment-id 的图片）。
 *
 * 说明：仓库 jest 未转译 antd 生态 ESM（`@ant-design/colors` 等），既有约定是
 * 按套件 mock antd（见 src/app/(auth)/login/__tests__/page.test.tsx:55），
 * 此处沿用该约定，仅保留与本用例断言相关的语义（disabled / aria-label / message）。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

const mockMessage = {
  warning: jest.fn(),
  error: jest.fn(),
  success: jest.fn(),
};

jest.mock('antd', () => {
  const ReactLib = require('react');

  const MockButton = (props: Record<string, unknown>) =>
    ReactLib.createElement(
      'button',
      {
        type: 'button',
        disabled: Boolean(props.disabled),
        'aria-label': props['aria-label'],
        onClick: props.onClick as () => void,
      },
      props.icon as React.ReactNode
    );

  const MockTooltip = (props: { children?: React.ReactNode }) =>
    ReactLib.createElement(ReactLib.Fragment, null, props.children);

  const MockApp = (props: { children?: React.ReactNode }) =>
    ReactLib.createElement('div', null, props.children);
  MockApp.useApp = () => ({ message: mockMessage });

  const passthrough = (name: string) => {
    const Mock = (props: { children?: React.ReactNode }) =>
      ReactLib.createElement('div', { 'data-mock': name }, props.children);
    Mock.displayName = `Mock(${name})`;
    return Mock;
  };

  const base: Record<string, unknown> = {
    App: MockApp,
    Button: MockButton,
    Tooltip: MockTooltip,
    Divider: passthrough('Divider'),
    Space: passthrough('Space'),
    Spin: passthrough('Spin'),
    InputNumber: passthrough('InputNumber'),
    Typography: {
      Text: (props: { children?: React.ReactNode }) => ReactLib.createElement('span', null, props.children),
      Title: passthrough('Typography.Title'),
      Paragraph: passthrough('Typography.Paragraph'),
    },
  };

  // 未列出的 antd 组件（Popover/Select 等）降级为透传容器，避免子组件 import 到 undefined
  return new Proxy(base, {
    get: (target, prop: string) =>
      prop in target ? (target as Record<string, unknown>)[prop] : passthrough(`antd.${prop}`),
  });
});

jest.mock('@ant-design/icons', () => {
  const ReactLib = require('react');
  return new Proxy(
    {},
    {
      get: (_target: object, prop: string) =>
        function MockIcon() {
          return ReactLib.createElement('span', { 'data-icon': prop });
        },
    }
  );
});

import RichTextEditor from '../RichTextEditor';

function renderEditor(props: React.ComponentProps<typeof RichTextEditor> = {}) {
  return render(<RichTextEditor {...props} />);
}

/** 工具栏图片按钮：禁用态与可用态 aria-label 不同（均由 btn() 的 title 透传） */
function imageButton(): HTMLElement {
  return screen.getByRole('button', { name: /插入图片|未配置图片上传能力/ });
}

function editorSurface(container: HTMLElement): HTMLElement {
  const node = container.querySelector('.ticket-rich-editor');
  if (!node) throw new Error('未找到编辑器容器 .ticket-rich-editor');
  return node as HTMLElement;
}

function imageFile(name = 'demo.png'): File {
  return new File(['fake-image-bytes'], name, { type: 'image/png' });
}

describe('RichTextEditor 图片上传 fail-fast（P0-2 / D6 / AC-8）', () => {
  let fetchMock: jest.Mock;
  let xhrOpenSpy: jest.SpyInstance;

  beforeEach(() => {
    // jsdom 无 fetch，直接注入替身；被测组件若发起请求会命中该替身
    fetchMock = jest.fn(() => Promise.reject(new Error('不应发起任何上传请求')));
    (globalThis as unknown as { fetch: jest.Mock }).fetch = fetchMock;
    xhrOpenSpy = jest.spyOn(XMLHttpRequest.prototype, 'open');
  });

  afterEach(() => {
    xhrOpenSpy.mockRestore();
  });

  it('未注入 onUploadImage 时插入图片按钮禁用', () => {
    renderEditor();

    expect(imageButton()).toBeDisabled();
  });

  it('未注入 onUploadImage 时点击与粘贴图片都不发起请求，并给出告警', async () => {
    const { container } = renderEditor();

    fireEvent.click(imageButton());
    fireEvent.paste(editorSurface(container), {
      clipboardData: { files: [imageFile()], items: [] },
    });

    await waitFor(() => {
      expect(mockMessage.warning).toHaveBeenCalledWith(expect.stringContaining('未配置图片上传能力'));
    });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(xhrOpenSpy).not.toHaveBeenCalled();
  });

  it('注入 onUploadImage 后按钮可用，粘贴图片走注入实现并插入附件节点', async () => {
    const onUploadImage = jest.fn().mockResolvedValue({
      url: '/api/v1/attachments/9/content',
      id: 9,
      name: 'demo.png',
    });
    const { container } = renderEditor({ onUploadImage });

    expect(imageButton()).toBeEnabled();

    fireEvent.paste(editorSurface(container), {
      clipboardData: { files: [imageFile()], items: [] },
    });

    await waitFor(() => expect(onUploadImage).toHaveBeenCalledTimes(1));

    await waitFor(() => {
      const img = container.querySelector('.rte-content img');
      expect(img).not.toBeNull();
      expect(img).toHaveAttribute('src', '/api/v1/attachments/9/content');
      expect(img).toHaveAttribute('data-attachment-id', '9');
    });
  });
});
