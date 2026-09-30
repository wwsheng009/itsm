import {
  SystemConfigAPI,
  isAICapabilityOverridden,
  type AICapabilitiesPatch,
} from '@/lib/api/system-config-api';
import { httpClient } from '@/lib/api/http-client';

jest.mock('@/lib/api/http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    delete: jest.fn(),
    patch: jest.fn(),
  },
}));

const mockGet = httpClient.get as jest.Mock;
const mockPut = httpClient.put as jest.Mock;

describe('SystemConfigAPI', () => {
  beforeEach(() => { jest.clearAllMocks(); });

  describe('getConfigs', () => {
    it('should get configs with params', async () => {
      const expected = { items: [], total: 0, page: 1, pageSize: 10, totalPages: 0 };
      mockGet.mockResolvedValue(expected);
      const res = await SystemConfigAPI.getConfigs({ page: 1, pageSize: 10 });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/system-configs', { page: 1, pageSize: 10 });
      expect(res).toEqual(expected);
    });

    it('should get configs without params', async () => {
      const expected = { items: [], total: 0, page: 1, pageSize: 10, totalPages: 0 };
      mockGet.mockResolvedValue(expected);
      const res = await SystemConfigAPI.getConfigs();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/system-configs', { pageSize: 1000 });
      expect(res).toEqual(expected);
    });
  });

  describe('getConfig', () => {
    it('should get config by id', async () => {
      const expected = { id: 1, key: 'site_name', value: 'ITSM' };
      mockGet.mockResolvedValue(expected);
      const res = await SystemConfigAPI.getConfig(1);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/system-configs/1');
      expect(res).toEqual(expected);
    });
  });

  describe('getConfigByKey', () => {
    it('should get config by key', async () => {
      const expected = { id: 1, key: 'site_name', value: 'ITSM' };
      mockGet.mockResolvedValue(expected);
      const res = await SystemConfigAPI.getConfigByKey('site_name');
      expect(mockGet).toHaveBeenCalledWith('/api/v1/system-configs/key/site_name');
      expect(res).toEqual(expected);
    });
  });

  describe('updateConfig', () => {
    it('should update config', async () => {
      const data = { key: 'site_name', value: 'New ITSM' };
      const expected = { id: 1, key: 'site_name', value: 'New ITSM' };
      mockPut.mockResolvedValue(expected);
      const res = await SystemConfigAPI.updateConfig(1, data);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/system-configs/1', data);
      expect(res).toEqual(expected);
    });
  });

  describe('updateConfigs', () => {
    it('should batch update configs', async () => {
      const data = [{ key: 'siteName', value: 'A' }, { key: 'siteDescription', value: 'B' }];
      const expected = [{ id: 1, value: 'A' }, { id: 2, value: 'B' }];
      mockPut.mockResolvedValue(expected);
      const res = await SystemConfigAPI.updateConfigs(data);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/system-configs/batch', data);
      expect(res).toEqual(expected);
    });
  });

  describe('getSystemStatus', () => {
    it('should get system status', async () => {
      const expected = { version: '1.0.0', uptime: 3600 };
      mockGet.mockResolvedValue(expected);
      const res = await SystemConfigAPI.getSystemStatus();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/system-configs/status');
      expect(res).toEqual(expected);
    });
  });

  // ==================== AI 能力开关（GET/PUT /system-configs/ai-capabilities） ====================

  const aiCapabilities = {
    mcpEnabled: true,
    mcpWriteEnabled: false,
    botEnabled: true,
    defaults: { mcpEnabled: true, mcpWriteEnabled: false, botEnabled: true },
    overridden: { 'mcp.write_enabled': true },
    updatedAt: '2026-09-30T01:00:00Z',
    updatedBy: 'admin',
    keys: ['mcp.enabled', 'mcp.write_enabled', 'bot.enabled'],
  } as const;

  describe('getAICapabilities', () => {
    it('should get ai capabilities snapshot (default camelCase httpClient)', async () => {
      mockGet.mockResolvedValue(aiCapabilities);
      const res = await SystemConfigAPI.getAICapabilities();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/system-configs/ai-capabilities');
      expect(res).toEqual(aiCapabilities);
    });
  });

  describe('isAICapabilityOverridden', () => {
    it('兼容 snake_case（后端原始 map key）与 camelCase（httpClient 归一化）两种形态', () => {
      expect(
        isAICapabilityOverridden({ overridden: { 'mcp.write_enabled': true } }, 'mcp.write_enabled'),
      ).toBe(true);
      // httpClient 的 toCamelCase 只把 `_x` 转大写、**点号保留** → 实际形态为 `mcp.writeEnabled`。
      expect(
        isAICapabilityOverridden({ overridden: { 'mcp.writeEnabled': true } }, 'mcp.write_enabled'),
      ).toBe(true);
      // 防御：把 `.`/`_` 都视作分隔符的紧凑形态（历史实现/其他调用方）同样识别。
      expect(
        isAICapabilityOverridden({ overridden: { mcpWriteEnabled: true } }, 'mcp.write_enabled'),
      ).toBe(true);
      expect(isAICapabilityOverridden({ overridden: {} }, 'mcp.write_enabled')).toBe(false);
      expect(isAICapabilityOverridden(null, 'mcp.write_enabled')).toBe(false);
    });
  });

  describe('updateAICapabilities', () => {
    it('should PUT three-state patch (omitted = unchanged, reset = restore default)', async () => {
      const patch: AICapabilitiesPatch = { mcpWriteEnabled: true, reset: ['bot.enabled'] };
      mockPut.mockResolvedValue({ ...aiCapabilities, mcpWriteEnabled: true });
      const res = await SystemConfigAPI.updateAICapabilities(patch);
      expect(mockPut).toHaveBeenCalledWith('/api/v1/system-configs/ai-capabilities', patch);
      expect(res.mcpWriteEnabled).toBe(true);
    });

    it('should propagate 403/503 errors to the caller', async () => {
      mockPut.mockRejectedValue({ httpStatus: 403 });
      await expect(SystemConfigAPI.updateAICapabilities({ botEnabled: false })).rejects.toEqual({
        httpStatus: 403,
      });
    });
  });
});
