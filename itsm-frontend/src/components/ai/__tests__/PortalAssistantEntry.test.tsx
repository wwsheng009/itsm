/**
 * V1-02 门户入口测试：权限门禁、打开 Dock + 预填提示词、埋点复用 assistant_opened。
 */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';

import PortalAssistantEntry, {
  PORTAL_ASSISTANT_ENTRY_TESTID,
  PORTAL_ASSISTANT_ENTRY_OPEN_TESTID,
} from '../PortalAssistantEntry';
import { usePermissions } from '@/lib/hooks/use-permissions';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.setTimeout(300000);

jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const grant = (allowed: boolean) => {
  mockedPermissions.mockReturnValue({
    hasPermission: () => allowed,
  } as unknown as ReturnType<typeof usePermissions>);
};

beforeEach(() => {
  jest.clearAllMocks();
  useAssistantStore.setState({ open: false, pendingPrompt: undefined });
  grant(true);
});

describe('PortalAssistantEntry（V1-02 门户入口）', () => {
  it('无 ai:read 权限不渲染（入口级门禁）', () => {
    grant(false);
    render(<PortalAssistantEntry pageKey="service.catalog" />);

    expect(screen.queryByTestId(PORTAL_ASSISTANT_ENTRY_TESTID)).not.toBeInTheDocument();
    expect(screen.queryByTestId(PORTAL_ASSISTANT_ENTRY_OPEN_TESTID)).not.toBeInTheDocument();
  });

  it('点击打开 Dock 并预填提示词；埋点复用 assistant_opened（source=launcher）', () => {
    render(<PortalAssistantEntry pageKey="service.catalog" />);
    fireEvent.click(screen.getByTestId(PORTAL_ASSISTANT_ENTRY_OPEN_TESTID));

    const state = useAssistantStore.getState();
    expect(state.open).toBe(true);
    expect(state.pendingPrompt).toContain('服务目录条目');
    expect(mockedTrack).toHaveBeenCalledWith(
      'assistant_opened',
      expect.objectContaining({
        pageKey: 'service.catalog',
        source: 'launcher',
        extra: expect.objectContaining({ entry: 'portal_entry' }),
      })
    );
  });

  it('自定义提示词透传（不同门户面可复用）；不触发任何提交', () => {
    render(
      <PortalAssistantEntry pageKey="service.requests" prompt="帮我检查这张服务申请该补哪些字段" />
    );
    fireEvent.click(screen.getByTestId(PORTAL_ASSISTANT_ENTRY_OPEN_TESTID));

    expect(useAssistantStore.getState().pendingPrompt).toBe('帮我检查这张服务申请该补哪些字段');
    // 负向：入口只预填/开面板，不发送、不提交（pendingPrompt 由面板消费）。
    expect(mockedTrack).toHaveBeenCalledTimes(1);
  });
});