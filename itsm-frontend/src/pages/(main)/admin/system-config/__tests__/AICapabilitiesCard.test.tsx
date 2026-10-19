import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

import AICapabilitiesCard from '../AICapabilitiesCard';
import type { AICapabilities } from '@/lib/api/system-config-api';

// antd + jsdom 渲染较慢，统一放大等待预算（与同目录页面测试口径一致）。
jest.setTimeout(120000);

/**
 * 「AI 能力开关」卡片行为测试（P6；2026-10 自 MCP 页迁移到系统配置 → AI 配置）。
 *
 * 文案由 key 直出（模拟 t），断言聚焦三态语义：来源徽标、脏字段保存、恢复默认、只读降级。
 */
const t = (key: string, params?: Record<string, string | number>): string =>
  params ? `${key}(${Object.values(params).join(',')})` : key;

const snapshot = (overrides: Partial<AICapabilities> = {}): AICapabilities => ({
  mcpEnabled: true,
  mcpWriteEnabled: false,
  botEnabled: true,
  assistantMemoryEnabled: true,
  assistantDockEnabled: true,
  helpAgentEnabled: true,
  defaults: {
    mcpEnabled: true,
    mcpWriteEnabled: false,
    botEnabled: true,
    assistantMemoryEnabled: true,
    assistantDockEnabled: true,
    helpAgentEnabled: true,
  },
  overridden: {},
  updatedAt: '2026-09-30T01:00:00Z',
  updatedBy: 'admin',
  keys: [
    'mcp.enabled',
    'mcp.write_enabled',
    'bot.enabled',
    'assistant.memory_enabled',
    'assistant.dock.enabled',
    'help.agent.enabled',
  ],
  ...overrides,
});

describe('AICapabilitiesCard（AI 能力开关卡片）', () => {
  it('渲染能力键当前生效值与来源徽标（覆盖 / 默认）', () => {
    render(
      <AICapabilitiesCard
        capabilities={snapshot({ overridden: { 'mcp.write_enabled': true } })}
        canWrite
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );

    expect(screen.getByTestId('ai-capability-switch-mcp.enabled')).toBeChecked();
    expect(screen.getByTestId('ai-capability-switch-mcp.write_enabled')).not.toBeChecked();
    expect(screen.getByTestId('ai-capability-switch-bot.enabled')).toBeChecked();
    // V2-01：个性化记忆租户开关（默认开启，静态默认同值）。
    expect(screen.getByTestId('ai-capability-switch-assistant.memory_enabled')).toBeChecked();
    // T5-05：Dock / 帮助智能体灰度（默认开启）。
    expect(screen.getByTestId('ai-capability-switch-assistant.dock.enabled')).toBeChecked();
    expect(screen.getByTestId('ai-capability-switch-help.agent.enabled')).toBeChecked();

    expect(screen.getByTestId('ai-capability-source-mcp.enabled')).toHaveTextContent(
      'mcp.capabilities.sourceDefault',
    );
    expect(screen.getByTestId('ai-capability-source-mcp.write_enabled')).toHaveTextContent(
      'mcp.capabilities.sourceOverride',
    );
  });

  it('保存仅提交变更过的字段（字段缺省 = 不修改）', () => {
    const onSave = jest.fn();
    render(
      <AICapabilitiesCard
        capabilities={snapshot()}
        canWrite
        t={t}
        onSave={onSave}
        onReset={jest.fn()}
      />,
    );

    // 无脏字段：保存不可点。
    expect(screen.getByTestId('ai-capability-save')).toBeDisabled();

    fireEvent.click(screen.getByTestId('ai-capability-switch-bot.enabled'));
    fireEvent.click(screen.getByTestId('ai-capability-switch-mcp.write_enabled'));
    fireEvent.click(screen.getByTestId('ai-capability-switch-assistant.dock.enabled'));
    fireEvent.click(screen.getByTestId('ai-capability-switch-help.agent.enabled'));
    fireEvent.click(screen.getByTestId('ai-capability-save'));

    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave).toHaveBeenCalledWith({
      mcpWriteEnabled: true,
      botEnabled: false,
      assistantDockEnabled: false,
      helpAgentEnabled: false,
    });
    // 未变更的 mcp.enabled 不进入补丁（避免把"跟随默认"固化成覆盖）。
    expect(onSave.mock.calls[0][0]).not.toHaveProperty('mcpEnabled');
  });

  it('恢复默认仅重置已覆盖的键；无覆盖时不可点', () => {
    const onReset = jest.fn();
    const { unmount } = render(
      <AICapabilitiesCard
        capabilities={snapshot({
          overridden: {
            'mcp.write_enabled': true,
            'bot.enabled': true,
            'assistant.dock.enabled': true,
          },
        })}
        canWrite
        t={t}
        onSave={jest.fn()}
        onReset={onReset}
      />,
    );

    fireEvent.click(screen.getByTestId('ai-capability-reset'));
    expect(onReset).toHaveBeenCalledWith([
      'mcp.write_enabled',
      'bot.enabled',
      'assistant.dock.enabled',
    ]);
    unmount();

    render(
      <AICapabilitiesCard
        capabilities={snapshot()}
        canWrite
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );
    expect(screen.getByTestId('ai-capability-reset')).toBeDisabled();
  });

  it('无 system_config:write：只读提示 + 开关/保存/恢复全部禁用', () => {
    render(
      <AICapabilitiesCard
        capabilities={snapshot()}
        canWrite={false}
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );

    expect(screen.getByTestId('ai-capability-readonly')).toBeInTheDocument();
    expect(screen.getByTestId('ai-capability-switch-mcp.enabled')).toBeDisabled();
    expect(screen.getByTestId('ai-capability-save')).toBeDisabled();
    expect(screen.getByTestId('ai-capability-reset')).toBeDisabled();
  });

  it('读取失败（无 system_config:read / 503）降级提示且不阻塞保存区渲染', () => {
    render(
      <AICapabilitiesCard
        capabilities={null}
        canWrite
        error
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );

    expect(screen.getByTestId('ai-capability-error')).toBeInTheDocument();
    expect(screen.getByTestId('ai-capability-save')).toBeDisabled();
  });
});
