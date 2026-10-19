import React from 'react';
import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react';

import AICapabilitiesSection from '../AICapabilitiesSection';
import { SystemConfigAPI, type AICapabilities } from '@/lib/api/system-config-api';

// antd + jsdom 渲染较慢，统一放大等待预算（与 MCP 页测试口径一致）。
jest.setTimeout(180000);
configure({ asyncUtilTimeout: 60000 });

jest.mock('@/lib/api/system-config-api', () => {
  const actual = jest.requireActual('@/lib/api/system-config-api');
  return {
    __esModule: true,
    ...actual,
    // 保留 isAICapabilityOverridden 等纯函数实现，仅替换网络方法。
    SystemConfigAPI: {
      getAICapabilities: jest.fn(),
      updateAICapabilities: jest.fn(),
    },
  };
});

const mockedHasPermission = jest.fn();
jest.mock('@/lib/hooks/use-permissions', () => ({
  usePermissions: () => ({ hasPermission: mockedHasPermission }),
}));

const mockedConfig = SystemConfigAPI as jest.Mocked<typeof SystemConfigAPI>;

const snapshot = (overrides: Partial<AICapabilities> = {}): AICapabilities => ({
  mcpEnabled: true,
  mcpWriteEnabled: false,
  botEnabled: true,
  assistantMemoryEnabled: true,
  defaults: { mcpEnabled: true, mcpWriteEnabled: false, botEnabled: true, assistantMemoryEnabled: true },
  overridden: {},
  updatedAt: '2026-09-30T01:00:00Z',
  updatedBy: 'admin',
  keys: ['mcp.enabled', 'mcp.write_enabled', 'bot.enabled', 'assistant.memory_enabled'],
  ...overrides,
});

function renderSection() {
  return render(
    <App>
      <AICapabilitiesSection />
    </App>,
  );
}

/**
 * 「AI 配置」页签容器测试（2026-10 能力开关自 MCP 页迁移）。
 *
 * 覆盖：加载快照 → 脏字段保存（仅提交变更键）→ 恢复默认二次确认（仅重置已覆盖键）→ 只读降级。
 */
describe('AICapabilitiesSection（系统配置 → AI 配置）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedHasPermission.mockReturnValue(true);
    mockedConfig.getAICapabilities.mockResolvedValue(snapshot() as never);
    mockedConfig.updateAICapabilities.mockResolvedValue(snapshot() as never);
  });

  it('加载快照并仅提交脏字段保存（bot.enabled → false）', async () => {
    mockedConfig.updateAICapabilities.mockResolvedValue(snapshot({ botEnabled: false }) as never);

    renderSection();

    expect(await screen.findByTestId('ai-capabilities-info')).toBeInTheDocument();
    const botSwitch = await screen.findByTestId('ai-capability-switch-bot.enabled');
    expect(botSwitch).toBeChecked();

    fireEvent.click(botSwitch);
    fireEvent.click(screen.getByTestId('ai-capability-save'));

    await waitFor(() =>
      expect(mockedConfig.updateAICapabilities).toHaveBeenCalledWith({ botEnabled: false }),
    );
    // 保存回读后按新快照渲染（未变更键不进入补丁）。
    await waitFor(() =>
      expect(screen.getByTestId('ai-capability-switch-bot.enabled')).not.toBeChecked(),
    );
  });

  it('恢复默认：二次确认后仅重置已覆盖的键', async () => {
    mockedConfig.getAICapabilities.mockResolvedValue(
      snapshot({ overridden: { 'mcp.write_enabled': true } }) as never,
    );

    renderSection();

    const reset = await screen.findByTestId('ai-capability-reset');
    expect(reset).toBeEnabled();
    fireEvent.click(reset);

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /确\s*[定认]/ }));

    await waitFor(() =>
      expect(mockedConfig.updateAICapabilities).toHaveBeenCalledWith({
        reset: ['mcp.write_enabled'],
      }),
    );
  });

  it('无 system_config:write：只读提示 + 全部禁用', async () => {
    mockedHasPermission.mockReturnValue(false);

    renderSection();

    expect(await screen.findByTestId('ai-capability-readonly')).toBeInTheDocument();
    expect(screen.getByTestId('ai-capability-switch-mcp.enabled')).toBeDisabled();
    expect(screen.getByTestId('ai-capability-save')).toBeDisabled();
    expect(screen.getByTestId('ai-capability-reset')).toBeDisabled();
  });
});
