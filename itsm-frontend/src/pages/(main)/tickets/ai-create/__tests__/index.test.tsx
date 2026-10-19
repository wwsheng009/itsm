/**
 * V0-02 员工侧建单页集成：自助问答「去建单」把描述带入表单意图（不自动生成/提交）。
 */
import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import AICreateTicketPage from '..';
import { aiSearchKB } from '@/lib/api/ai-api';
import { A2UIApi } from '@/lib/api/a2ui-api';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiSearchKB: jest.fn(),
}));
jest.mock('@/lib/api/a2ui-api', () => ({
  __esModule: true,
  A2UIApi: { generateTicketForm: jest.fn(), handleTicketAction: jest.fn() },
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedSearch = aiSearchKB as jest.MockedFunction<typeof aiSearchKB>;
const mockedApi = A2UIApi as unknown as {
  generateTicketForm: jest.Mock;
  handleTicketAction: jest.Mock;
};

beforeEach(() => {
  mockedSearch.mockReset();
  mockedApi.generateTicketForm.mockReset();
});

describe('AICreateTicketPage（V0-02 员工侧）', () => {
  it('自助问答与建单表单同页：未命中「去建单」带入描述但不自动生成', async () => {
    mockedSearch.mockResolvedValue({ answers: [] });

    render(
      <App>
        <AICreateTicketPage />
      </App>
    );

    // 两段都在：自助问答 + A2UI 建单表单。
    expect(screen.getByTestId('self-service-assistant')).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/ThinkPad/)).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('self-service-input'), { target: { value: 'VPN 连不上' } });
    fireEvent.click(screen.getByTestId('self-service-ask'));
    fireEvent.click(await screen.findByTestId('self-service-proceed-empty'));

    const intentInput = screen.getByPlaceholderText(/ThinkPad/) as HTMLInputElement;
    await waitFor(() => expect(intentInput.value).toBe('VPN 连不上'));
    // 负向：带入描述不等于自动生成/提交（用户仍需点「发送」并核对表单）。
    expect(mockedApi.generateTicketForm).not.toHaveBeenCalled();
  });
});