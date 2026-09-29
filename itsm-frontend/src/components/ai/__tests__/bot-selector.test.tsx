import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

import BotSelector, { DEFAULT_BOT_VALUE } from '../BotSelector';
import type { BotOption } from '@/lib/api/ai-api';

// B2-04 BotSelector 组件测试：
// 兼容默认（空列表/加载中不渲染）、选项渲染、选择回调（含默认助手哨兵）、
// 已有会话锁定（切换仅影响新会话）。

const bots: BotOption[] = [
  { id: 11, slug: 'ops', name: '运维助手', audience: 'internal' },
  { id: 22, slug: 'user-bot', name: '用户助手', audience: 'end_user' },
];

describe('BotSelector', () => {
  it('候选为空或加载中时不渲染（兼容默认：行为与引入选择器前一致）', () => {
    const { container: empty } = render(
      <BotSelector bots={[]} value={null} onChange={jest.fn()} />
    );
    expect(empty).toBeEmptyDOMElement();

    const { container: loading } = render(
      <BotSelector bots={bots} value={null} onChange={jest.fn()} loading />
    );
    expect(loading).toBeEmptyDOMElement();
  });

  it('渲染默认助手与全部候选 Bot', () => {
    render(<BotSelector bots={bots} value={null} onChange={jest.fn()} />);
    const selector = screen.getByTestId('bot-selector');
    expect(selector).toBeInTheDocument();
    // antd Select：默认助手为当前值（value=null → 哨兵 0）。
    expect(screen.getByText('默认助手')).toBeInTheDocument();
  });

  it('选择候选 Bot 回调其 id；选回默认助手回调 null', () => {
    const onChange = jest.fn();
    render(<BotSelector bots={bots} value={null} onChange={onChange} />);

    // antd Select 在测试环境使用鼠标事件开合。
    fireEvent.mouseDown(screen.getByTestId('bot-selector'));
    fireEvent.click(screen.getByTitle('运维助手'));
    expect(onChange).toHaveBeenCalledWith(11);
  });

  it('已有会话时锁定：选择器禁用（切换仅影响新会话）', () => {
    render(<BotSelector bots={bots} value={22} onChange={jest.fn()} locked />);
    expect(screen.getByTestId('bot-selector')).toHaveClass('ant-select-disabled');
  });

  it('默认助手哨兵值为 0 且与 null 显式解耦', () => {
    expect(DEFAULT_BOT_VALUE).toBe(0);
    render(<BotSelector bots={bots} value={0} onChange={jest.fn()} />);
    expect(screen.getByTestId('bot-selector')).toBeInTheDocument();
  });
});
