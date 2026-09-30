import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

import CapabilitySwitchesCard from '../CapabilitySwitchesCard';
import type { AICapabilities } from '@/lib/api/system-config-api';

// antd + jsdom 渲染较慢，统一放大等待预算（与同目录页面测试口径一致）。
jest.setTimeout(120000);

/**
 * 「能力开关」卡片行为测试（P6）。
 *
 * 文案由 key 直出（模拟 t），断言聚焦三态语义：来源徽标、脏字段保存、恢复默认、只读降级。
 */
const t = (key: string, params?: Record<string, string | number>): string =>
  params ? `${key}(${Object.values(params).join(',')})` : key;

const snapshot = (overrides: Partial<AICapabilities> = {}): AICapabilities => ({
  mcpEnabled: true,
  mcpWriteEnabled: false,
  botEnabled: true,
  defaults: { mcpEnabled: true, mcpWriteEnabled: false, botEnabled: true },
  overridden: {},
  updatedAt: '2026-09-30T01:00:00Z',
  updatedBy: 'admin',
  keys: ['mcp.enabled', 'mcp.write_enabled', 'bot.enabled'],
  ...overrides,
});

describe('CapabilitySwitchesCard（能力开关卡片）', () => {
  it('渲染三键当前生效值与来源徽标（覆盖 / 默认）', () => {
    render(
      <CapabilitySwitchesCard
        capabilities={snapshot({ overridden: { 'mcp.write_enabled': true } })}
        canWrite
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );

    expect(screen.getByTestId('mcp-capability-switch-mcp.enabled')).toBeChecked();
    expect(screen.getByTestId('mcp-capability-switch-mcp.write_enabled')).not.toBeChecked();
    expect(screen.getByTestId('mcp-capability-switch-bot.enabled')).toBeChecked();

    expect(screen.getByTestId('mcp-capability-source-mcp.enabled')).toHaveTextContent(
      'mcp.capabilities.sourceDefault',
    );
    expect(screen.getByTestId('mcp-capability-source-mcp.write_enabled')).toHaveTextContent(
      'mcp.capabilities.sourceOverride',
    );
  });

  it('保存仅提交变更过的字段（字段缺省 = 不修改）', () => {
    const onSave = jest.fn();
    render(
      <CapabilitySwitchesCard
        capabilities={snapshot()}
        canWrite
        t={t}
        onSave={onSave}
        onReset={jest.fn()}
      />,
    );

    // 无脏字段：保存不可点。
    expect(screen.getByTestId('mcp-capability-save')).toBeDisabled();

    fireEvent.click(screen.getByTestId('mcp-capability-switch-bot.enabled'));
    fireEvent.click(screen.getByTestId('mcp-capability-switch-mcp.write_enabled'));
    fireEvent.click(screen.getByTestId('mcp-capability-save'));

    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave).toHaveBeenCalledWith({ mcpWriteEnabled: true, botEnabled: false });
    // 未变更的 mcp.enabled 不进入补丁（避免把"跟随默认"固化成覆盖）。
    expect(onSave.mock.calls[0][0]).not.toHaveProperty('mcpEnabled');
  });

  it('恢复默认仅重置已覆盖的键；无覆盖时不可点', () => {
    const onReset = jest.fn();
    const { unmount } = render(
      <CapabilitySwitchesCard
        capabilities={snapshot({ overridden: { 'mcp.write_enabled': true, 'bot.enabled': true } })}
        canWrite
        t={t}
        onSave={jest.fn()}
        onReset={onReset}
      />,
    );

    fireEvent.click(screen.getByTestId('mcp-capability-reset'));
    expect(onReset).toHaveBeenCalledWith(['mcp.write_enabled', 'bot.enabled']);
    unmount();

    render(
      <CapabilitySwitchesCard
        capabilities={snapshot()}
        canWrite
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );
    expect(screen.getByTestId('mcp-capability-reset')).toBeDisabled();
  });

  it('无 system_config:write：只读提示 + 开关/保存/恢复全部禁用', () => {
    render(
      <CapabilitySwitchesCard
        capabilities={snapshot()}
        canWrite={false}
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );

    expect(screen.getByTestId('mcp-capability-readonly')).toBeInTheDocument();
    expect(screen.getByTestId('mcp-capability-switch-mcp.enabled')).toBeDisabled();
    expect(screen.getByTestId('mcp-capability-save')).toBeDisabled();
    expect(screen.getByTestId('mcp-capability-reset')).toBeDisabled();
  });

  it('读取失败（无 system_config:read / 503）降级提示且不阻塞保存区渲染', () => {
    render(
      <CapabilitySwitchesCard
        capabilities={null}
        canWrite
        error
        t={t}
        onSave={jest.fn()}
        onReset={jest.fn()}
      />,
    );

    expect(screen.getByTestId('mcp-capability-error')).toBeInTheDocument();
    expect(screen.getByTestId('mcp-capability-save')).toBeDisabled();
  });
});
