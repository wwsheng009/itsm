import {
  PARALLEL_RANGE,
  RETRY_RANGE,
  TIMEOUT_RANGE,
  credentialRequired,
  hasHealthAlert,
  isToolEffective,
  maskedHint,
  pollExpired,
  runningStatusTone,
  shouldKeepPolling,
  toolStatusKey,
  validateServerName,
  validateServerURL,
} from '../mcp-helpers';
import type { MCPServer, MCPTool } from '@/lib/api/mcp-api';

const server = (overrides: Partial<MCPServer> = {}): MCPServer =>
  ({ enabled: true, running_status: 'connected', ...overrides }) as MCPServer;

const tool = (overrides: Partial<MCPTool> = {}): MCPTool =>
  ({ enabled: true, healthy: true, configured_enabled: true, quarantined: false, ...overrides }) as MCPTool;

describe('mcp-helpers 状态口径', () => {
  it('运行态映射：未启用恒为灰；connected/error/过渡态分色', () => {
    expect(runningStatusTone(server({ enabled: false, running_status: 'connected' }))).toBe('default');
    expect(runningStatusTone(server())).toBe('success');
    expect(runningStatusTone(server({ running_status: 'connecting' }))).toBe('warning');
    expect(runningStatusTone(server({ running_status: 'reconnecting' }))).toBe('warning');
    expect(runningStatusTone(server({ running_status: 'error' }))).toBe('error');
    expect(runningStatusTone(server({ running_status: 'configured' }))).toBe('default');
  });

  it('工具生效 = 配置启用 + 健康 + 未隔离；标签优先级 隔离 > 停用 > 不可用 > 生效', () => {
    expect(isToolEffective(tool())).toBe(true);
    expect(isToolEffective(tool({ healthy: false }))).toBe(false);
    expect(isToolEffective(tool({ quarantined: true }))).toBe(false);
    // 后端 toolView：enabled = 治理位 && healthy && !quarantined（生效口径）。
    expect(isToolEffective(tool({ enabled: false }))).toBe(false);
    expect(isToolEffective(tool({ configured_enabled: false, enabled: false }))).toBe(false);

    expect(toolStatusKey(tool({ quarantined: true, configured_enabled: false }))).toBe('quarantined');
    expect(toolStatusKey(tool({ configured_enabled: false }))).toBe('disabled');
    expect(toolStatusKey(tool({ healthy: false }))).toBe('unhealthy');
    expect(toolStatusKey(tool())).toBe('active');
  });

  it('健康摘要：错误或认证失效触发告警', () => {
    expect(hasHealthAlert(undefined)).toBe(false);
    expect(hasHealthAlert({ error: 0, auth_required: 0 } as never)).toBe(false);
    expect(hasHealthAlert({ error: 1, auth_required: 0 } as never)).toBe(true);
    expect(hasHealthAlert({ error: 0, auth_required: 2 } as never)).toBe(true);
  });

  it('掩码展示：仅键名，值不参与渲染', () => {
    expect(maskedHint(undefined)).toBe('');
    expect(maskedHint({})).toBe('');
    expect(maskedHint({ token: '****', 'Authorization': '***' })).toBe('token, Authorization');
  });
});

describe('mcp-helpers 校验（与后端 validation.go 同口径）', () => {
  it('标识：^[a-z0-9_-]{1,32}$', () => {
    expect(validateServerName('gitlab')).toBeUndefined();
    expect(validateServerName('git_lab-2')).toBeUndefined();
    expect(validateServerName('')).toBe('validation.nameRequired');
    expect(validateServerName('   ')).toBe('validation.nameRequired');
    expect(validateServerName('GitLab')).toBe('validation.namePattern');
    expect(validateServerName('bad name')).toBe('validation.namePattern');
    expect(validateServerName('a'.repeat(33))).toBe('validation.namePattern');
  });

  it('地址：http(s)、必须带主机、不得含用户信息', () => {
    expect(validateServerURL('https://mcp.example.com/mcp')).toBeUndefined();
    expect(validateServerURL('http://127.0.0.1:8080/mcp')).toBeUndefined();
    expect(validateServerURL('')).toBe('validation.urlRequired');
    expect(validateServerURL('ftp://example.com')).toBe('validation.urlScheme');
    expect(validateServerURL('not-a-url')).toBe('validation.urlInvalid');
    expect(validateServerURL('https://user:pass@example.com/mcp')).toBe('validation.urlUserInfo');
  });

  it('凭据类型决定是否必填；范围常量与后端一致', () => {
    expect(credentialRequired('none')).toBe(false);
    expect(credentialRequired(undefined)).toBe(false);
    expect(credentialRequired('static_header')).toBe(true);
    expect(credentialRequired('oauth2')).toBe(true);

    expect(TIMEOUT_RANGE).toEqual({ min: 1000, max: 300000 });
    expect(PARALLEL_RANGE).toEqual({ min: 1, max: 32 });
    expect(RETRY_RANGE).toEqual({ min: 0, max: 5 });
  });
});

describe('mcp-helpers 轮询判定（D8：202 + 2s 轮询）', () => {
  it('pollExpired：达到上限即超时', () => {
    expect(pollExpired(1000, 1000 + 29999, 30000)).toBe(false);
    expect(pollExpired(1000, 1000 + 30000, 30000)).toBe(true);
  });

  it('shouldKeepPolling：过渡态继续轮询，终态停止', () => {
    expect(shouldKeepPolling({ running_status: 'connecting' })).toBe(true);
    expect(shouldKeepPolling({ running_status: 'connected' }, true)).toBe(false);
    expect(shouldKeepPolling({ running_status: 'configured' }, true)).toBe(true);
    expect(shouldKeepPolling({ running_status: 'configured' }, false)).toBe(false);
    expect(shouldKeepPolling({ running_status: 'disconnecting' }, false)).toBe(true);
    expect(shouldKeepPolling({ running_status: 'error' }, true)).toBe(false);
  });
});
