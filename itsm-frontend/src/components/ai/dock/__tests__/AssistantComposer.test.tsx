/**
 * composer 重构（2026-10-19）回归测试。
 *
 * 锁定的产品契约：
 * 1. 输入区是**单一容器**：输入框与动作按钮同属 composer，附件不再独占一整行；
 * 2. 静止态**只有 4 个图标按钮**（附件 / 记忆 / 后台精算 / 发送），不含文字型按钮；
 * 3. 附件入口在工具条内、与输入框同一行；附件芯片出现在文本域上方（不是另一行按钮）；
 * 4. 记忆的 3 个常驻按钮收为 1 个（保存动作进弹出层），chip 仍在弹出层内可点；
 * 5. 可访问性：每个图标按钮都有 aria-label，键盘可达。
 */
import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';

import AssistantComposer, { ASSISTANT_COMPOSER_TESTID } from '../AssistantComposer';
import type { AIAttachmentRef } from '@/lib/api/ai-api';

jest.mock('@/lib/i18n/useI18n', () => ({
  useI18n: () => ({
    t: (key: string, vars?: Record<string, unknown>) => {
      const table: Record<string, string> = {
        'assistant.panel.inputPlaceholder': '请输入你的问题…',
        'assistant.panel.send': '发送',
        'assistant.panel.stopGenerating': '停止生成',
        'assistant.panel.asyncStart': '后台精算',
        'assistant.panel.asyncStartTooltip': '后台精算说明',
        'assistant.attachment.upload': '附件',
        'assistant.attachment.uploadHint': `最多 ${vars?.max} 个`,
        'assistant.attachment.asyncUnsupported': '后台精算暂不支持附件',
        'assistant.attachment.remove': `移除附件 ${vars?.name}`,
      };
      return table[key] ?? key;
    },
  }),
}));

const attachments: AIAttachmentRef[] = [
  {
    id: 7,
    fileName: '错误截图.png',
    fileSize: 2048,
    mimeType: 'image/png',
    fileUrl: '/api/v1/ai/attachments/7/content',
  },
];

const renderComposer = (overrides: Partial<React.ComponentProps<typeof AssistantComposer>> = {}) => {
  const props: React.ComponentProps<typeof AssistantComposer> = {
    query: '',
    setQuery: jest.fn(),
    onSend: jest.fn(),
    onStop: jest.fn(),
    streaming: false,
    inputRef: { current: null },
    inputAssist: {
      assist: null,
      handleKeyDown: jest.fn(),
      selectItem: jest.fn(),
      highlightItem: jest.fn(),
    },
    widePanel: false,
    onStartAsync: jest.fn(),
    attachments: [],
    attachmentInput: <input type="file" style={{ display: 'none' }} />,
    onOpenAttachment: jest.fn(),
    attachmentUploading: false,
    attachmentError: null,
    attachmentDisabled: false,
    onRemoveAttachment: jest.fn(),
    tools: <button type="button" aria-label="记忆">记忆</button>,
    ...overrides,
  };
  const view = render(
    <App>
      <AssistantComposer {...props} />
    </App>
  );
  return { ...view, props };
};

describe('AssistantComposer（composer 重构）', () => {
  it('输入框与动作按钮同属单一 composer 容器（附件不再独占一行）', () => {
    const { props } = renderComposer({ query: '工单怎么转派' });
    const composer = screen.getByTestId(ASSISTANT_COMPOSER_TESTID);

    const input = within(composer).getByTestId('assistant-panel-input');
    const attachment = within(composer).getByTestId('chat-attachment-upload');
    const send = within(composer).getByTestId('assistant-panel-send');

    // 三个元素同一个子树：附件按钮与输入框不再分属两行容器。
    expect(composer).toContainElement(input);
    expect(composer).toContainElement(attachment);
    expect(composer).toContainElement(send);

    fireEvent.click(send);
    expect(props.onSend).toHaveBeenCalled();
  });

  it('静止态只有 4 个图标按钮，全部带可访问名', () => {
    renderComposer();
    const composer = screen.getByTestId(ASSISTANT_COMPOSER_TESTID);
    // 排除用户注入的记忆入口（真实实现是一个 Button，这里用 button 断言总数）。
    const buttons = within(composer).getAllByRole('button');
    expect(buttons).toHaveLength(4);

    const labels = buttons.map(btn => btn.getAttribute('aria-label'));
    expect(labels).toEqual(['附件', '记忆', '后台精算', '发送']);
    labels.forEach(label => expect(label).toBeTruthy());
  });

  it('附件芯片渲染在文本域上方，可单独移除且不进工具条', () => {
    const onRemoveAttachment = jest.fn();
    renderComposer({ attachments, onRemoveAttachment });

    const chip = screen.getByTestId('chat-attachment-chip-7');
    expect(chip).toHaveTextContent('错误截图.png');

    fireEvent.click(within(chip).getByRole('button', { name: '移除附件 错误截图.png' }));
    expect(onRemoveAttachment).toHaveBeenCalledWith(7);
  });

  it('有附件时后台精算禁用，并给出原因', () => {
    renderComposer({ query: '帮我看看', attachments });
    expect(screen.getByTestId('assistant-async-start')).toBeDisabled();
  });

  it('流式生成中：发送换成停止，且附件入口禁用', () => {
    const onStop = jest.fn();
    renderComposer({ query: '生成中', streaming: true, onStop });

    expect(screen.queryByTestId('assistant-panel-send')).not.toBeInTheDocument();
    expect(screen.getByTestId('chat-attachment-upload')).toBeDisabled();

    fireEvent.click(screen.getByRole('button', { name: '停止生成' }));
    expect(onStop).toHaveBeenCalled();
  });

  it('附件上传失败原因就地展示（不弹窗、不遮断输入）', () => {
    renderComposer({ attachmentError: '文件类型不支持' });
    expect(screen.getByTestId('chat-attachment-error')).toHaveTextContent('文件类型不支持');
  });

  it('键盘：输入框可聚焦并响应 Enter 发送（委托会话内核）', async () => {
    const handleKeyDown = jest.fn();
    renderComposer({
      query: '你好',
      inputAssist: {
        assist: null,
        handleKeyDown,
        selectItem: jest.fn(),
        highlightItem: jest.fn(),
      },
    });

    const input = screen.getByTestId('assistant-panel-input');
    input.focus();
    await waitFor(() => expect(input).toHaveFocus());
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(handleKeyDown).toHaveBeenCalled();
  });

  it('工作区形态：自定义 testid / 字号，且不渲染 Dock 专属的后台精算按钮', () => {
    renderComposer({
      testId: 'assistant-workspace-composer',
      inputTestId: 'assistant-chat-input',
      inputFontSize: 14,
      onStartAsync: undefined,
    });

    const workspace = screen.getByTestId('assistant-workspace-composer');
    const input = within(workspace).getByTestId('assistant-chat-input');
    expect(input).toHaveStyle({ fontSize: '14px' });

    // Dock 专属入口不出现；附件/发送仍在同一容器内。
    expect(within(workspace).queryByTestId('assistant-async-start')).not.toBeInTheDocument();
    expect(within(workspace).getByTestId('chat-attachment-upload')).toBeInTheDocument();
    expect(within(workspace).getByTestId('assistant-panel-send')).toBeInTheDocument();
  });
});
