import React, { useCallback, useEffect, useState } from 'react';
import { Alert, App } from 'antd';

import {
  SystemConfigAPI,
  type AICapabilities,
  type AICapabilitiesPatch,
  type AICapabilityKey,
} from '@/lib/api/system-config-api';
import { useI18n } from '@/lib/i18n/useI18n';
import { usePermissions } from '@/lib/hooks/use-permissions';

import AICapabilitiesCard from './AICapabilitiesCard';

/**
 * 「AI 配置」页签内容（系统配置 → AI 配置）。
 *
 * 平台级运行时能力开关（MCP 总开关 / 外部写工具面 / Bot 能力）：
 * - 读写走 `GET/PUT /api/v1/system-configs/ai-capabilities`（`system_config:read/write`），
 *   保存后后端立即失效缓存，**无需重启**；
 * - 缺省不写库、跟随部署默认（环境变量 / 配置文件）；
 * - 2026-10 自「MCP 外部工具」页迁移至此：MCP 页保留指路提示，开关本体统一在系统配置维护。
 */
const AICapabilitiesSection: React.FC = () => {
  const { message, modal } = App.useApp();
  const { t } = useI18n();
  const { hasPermission } = usePermissions();
  const canWrite = hasPermission('system_config', 'write');

  const [capabilities, setCapabilities] = useState<AICapabilities | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(false);

  /** 读取运行时能力开关（无 system_config:read / 服务未就绪时降级提示，不阻塞页面）。 */
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const snapshot = await SystemConfigAPI.getAICapabilities();
      setCapabilities(snapshot);
      setError(false);
    } catch {
      setError(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const reportError = useCallback(
    (e: unknown) => {
      const status = (e as { httpStatus?: number } | undefined)?.httpStatus;
      if (status === 403) {
        message.warning(t('mcp.capabilities.writeDenied'));
        return;
      }
      message.error(t('mcp.capabilities.saveFailed'));
    },
    [message, t],
  );

  /** 保存能力开关：仅提交脏字段（字段缺省 = 不修改）。 */
  const handleSave = useCallback(
    async (patch: AICapabilitiesPatch) => {
      setSaving(true);
      try {
        const snapshot = await SystemConfigAPI.updateAICapabilities(patch);
        setCapabilities(snapshot);
        setError(false);
        message.success(t('mcp.capabilities.saved'));
      } catch (e) {
        reportError(e);
      } finally {
        setSaving(false);
      }
    },
    [message, reportError, t],
  );

  /** 恢复默认：删除覆盖行、恢复跟随环境默认（仅重置已覆盖的键，二次确认防误触）。 */
  const handleReset = useCallback(
    (keys: AICapabilityKey[]) => {
      if (!keys.length) return;
      modal.confirm({
        title: t('mcp.capabilities.resetConfirmTitle'),
        content: t('mcp.capabilities.resetConfirmContent'),
        okText: t('common.confirm'),
        cancelText: t('common.cancel'),
        onOk: async () => {
          setSaving(true);
          try {
            const snapshot = await SystemConfigAPI.updateAICapabilities({ reset: keys });
            setCapabilities(snapshot);
            setError(false);
            message.success(t('mcp.capabilities.saved'));
          } catch (e) {
            reportError(e);
          } finally {
            setSaving(false);
          }
        },
      });
    },
    [message, modal, reportError, t],
  );

  return (
    <>
      <Alert
        data-testid="ai-capabilities-info"
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="AI 能力开关（平台级配置，免重启）"
        description="MCP 与 Bot 的能力开关在此统一维护：保存后立即生效、无需重启服务；缺省跟随部署配置（环境变量 / 配置文件）。此前位于「MCP 外部工具」页顶部的能力开关卡片已迁移到本页签，MCP 页保留指路提示。"
      />
      <AICapabilitiesCard
        capabilities={capabilities}
        canWrite={canWrite}
        error={error}
        loading={loading}
        saving={saving}
        t={t}
        onSave={handleSave}
        onReset={handleReset}
      />
    </>
  );
};

export default AICapabilitiesSection;
