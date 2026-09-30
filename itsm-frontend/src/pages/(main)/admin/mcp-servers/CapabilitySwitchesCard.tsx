import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Space, Switch, Tag, Tooltip, Typography } from 'antd';
import { RotateCcw, Save } from 'lucide-react';

import type {
  AICapabilities,
  AICapabilitiesPatch,
  AICapabilityKey,
} from '@/lib/api/system-config-api';
import { isAICapabilityOverridden } from '@/lib/api/system-config-api';

const { Text } = Typography;

/** i18n 函数签名（与 useI18n 的 t 对齐）。 */
type TFunc = (key: string, params?: Record<string, string | number>) => string;

/** 三键与响应字段的映射（key 为后端 system_configs 键名，field 为 camelCase 字段）。 */
interface CapabilityRow {
  key: AICapabilityKey;
  field: 'mcpEnabled' | 'mcpWriteEnabled' | 'botEnabled';
  labelKey: string;
  hintKey: string;
}

const ROWS: CapabilityRow[] = [
  {
    key: 'mcp.enabled',
    field: 'mcpEnabled',
    labelKey: 'mcp.capabilities.mcpEnabled',
    hintKey: 'mcp.capabilities.mcpEnabledHint',
  },
  {
    key: 'mcp.write_enabled',
    field: 'mcpWriteEnabled',
    labelKey: 'mcp.capabilities.mcpWriteEnabled',
    hintKey: 'mcp.capabilities.mcpWriteEnabledHint',
  },
  {
    key: 'bot.enabled',
    field: 'botEnabled',
    labelKey: 'mcp.capabilities.botEnabled',
    hintKey: 'mcp.capabilities.botEnabledHint',
  },
];

export interface CapabilitySwitchesCardProps {
  /** 当前生效快照；null = 尚未加载或读取失败（配合 error 展示提示）。 */
  capabilities: AICapabilities | null;
  /** 是否具备 `system_config:write`（仅超级管理员）；false = 只读展示。 */
  canWrite: boolean;
  /** 读取失败（无 `system_config:read` / 服务未就绪）：卡片降级为提示，不阻塞页面。 */
  error?: boolean;
  loading?: boolean;
  saving?: boolean;
  t: TFunc;
  /** 保存脏字段（字段缺省 = 不改；reset 由 onReset 单独触发）。 */
  onSave: (patch: AICapabilitiesPatch) => void | Promise<void>;
  /** 恢复默认（删除覆盖行、跟随环境默认）。 */
  onReset: (keys: AICapabilityKey[]) => void | Promise<void>;
}

/**
 * 「能力开关」卡片（MCP 管理页）。
 *
 * 三个运行时能力开关（`mcp.enabled` / `mcp.write_enabled` / `bot.enabled`）：
 * - 每行展示当前生效值 + 来源徽标（已由管理台覆盖 / 跟随环境默认）；
 * - 保存只提交**变更过的字段**（三态契约：字段缺省 = 不改，不把"跟随默认"意外固化为覆盖）；
 * - 「恢复默认」仅重置已覆盖的键（reset 数组）；
 * - 无 `system_config:write` 时全部只读并提示。
 */
const CapabilitySwitchesCard: React.FC<CapabilitySwitchesCardProps> = ({
  capabilities,
  canWrite,
  error = false,
  loading = false,
  saving = false,
  t,
  onSave,
  onReset,
}) => {
  const [draft, setDraft] = useState<AICapabilities | null>(capabilities);

  useEffect(() => {
    setDraft(capabilities);
  }, [capabilities]);

  /** 显式按键更新草稿（避免联合类型计算属性导致的类型收窄问题）。 */
  const updateField = (field: CapabilityRow['field'], next: boolean) => {
    setDraft(prev => {
      if (!prev) return prev;
      if (field === 'mcpEnabled') return { ...prev, mcpEnabled: next };
      if (field === 'mcpWriteEnabled') return { ...prev, mcpWriteEnabled: next };
      return { ...prev, botEnabled: next };
    });
  };

  /** 脏字段补丁：仅提交与已生效值不同的字段（缺省 = 不改）。 */
  const patch = useMemo<AICapabilitiesPatch | null>(() => {
    if (!draft || !capabilities) return null;
    const next: AICapabilitiesPatch = {};
    if (draft.mcpEnabled !== capabilities.mcpEnabled) next.mcpEnabled = draft.mcpEnabled;
    if (draft.mcpWriteEnabled !== capabilities.mcpWriteEnabled) next.mcpWriteEnabled = draft.mcpWriteEnabled;
    if (draft.botEnabled !== capabilities.botEnabled) next.botEnabled = draft.botEnabled;
    return Object.keys(next).length > 0 ? next : null;
  }, [capabilities, draft]);

  /** 已覆盖的键（恢复默认的提交范围）。 */
  const overriddenKeys = useMemo<AICapabilityKey[]>(
    () =>
      ROWS.map(row => row.key).filter(
        key => isAICapabilityOverridden(capabilities, key),
      ),
    [capabilities],
  );

  const readOnly = !canWrite;
  const updatedLabel =
    capabilities && (capabilities.updatedAt || capabilities.updatedBy)
      ? t('mcp.capabilities.updated', {
          user: capabilities.updatedBy || '-',
          time: capabilities.updatedAt || '-',
        })
      : null;

  return (
    <Card
      size="small"
      data-testid="mcp-capability-card"
      style={{ marginBottom: 16 }}
      loading={loading && !capabilities}
      title={t('mcp.capabilities.title')}
      extra={updatedLabel ? <Text type="secondary" style={{ fontSize: 12 }}>{updatedLabel}</Text> : null}
    >
      {error ? (
        <Alert
          data-testid="mcp-capability-error"
          type="warning"
          showIcon
          message={t('mcp.capabilities.loadFailed')}
          description={t('mcp.capabilities.loadFailedHint')}
          style={{ marginBottom: 12 }}
        />
      ) : null}

      {readOnly ? (
        <Alert
          data-testid="mcp-capability-readonly"
          type="info"
          showIcon
          message={t('mcp.capabilities.readOnlyHint')}
          style={{ marginBottom: 12 }}
        />
      ) : null}

      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        {ROWS.map(row => {
          const checked = draft ? draft[row.field] : false;
          const overridden = isAICapabilityOverridden(capabilities, row.key);
          return (
            <Space key={row.key} direction="vertical" size={2} style={{ width: '100%' }}>
              <Space size={8} align="center">
                <Switch
                  data-testid={`mcp-capability-switch-${row.key}`}
                  aria-label={t(row.labelKey)}
                  checked={checked}
                  disabled={readOnly || !draft}
                  loading={saving}
                  onChange={next => updateField(row.field, next)}
                />
                <Text strong>{t(row.labelKey)}</Text>
                <Tag
                  data-testid={`mcp-capability-source-${row.key}`}
                  color={overridden ? 'blue' : 'default'}
                >
                  {t(overridden ? 'mcp.capabilities.sourceOverride' : 'mcp.capabilities.sourceDefault')}
                </Tag>
                {overridden && capabilities ? (
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {t('mcp.capabilities.defaultValue', {
                      value: String(capabilities.defaults[row.field]),
                    })}
                  </Text>
                ) : null}
              </Space>
              <Text type="secondary" style={{ fontSize: 12 }}>{t(row.hintKey)}</Text>
            </Space>
          );
        })}
      </Space>

      <Space style={{ marginTop: 16 }} wrap>
        <Tooltip title={readOnly ? t('mcp.capabilities.readOnlyHint') : undefined}>
          <Button
            type="primary"
            icon={<Save size={14} />}
            data-testid="mcp-capability-save"
            disabled={readOnly || !patch}
            loading={saving}
            onClick={() => patch && void onSave(patch)}
          >
            {t('mcp.capabilities.save')}
          </Button>
        </Tooltip>
        <Tooltip
          title={readOnly ? t('mcp.capabilities.readOnlyHint') : t('mcp.capabilities.resetHint')}
        >
          <Button
            icon={<RotateCcw size={14} />}
            data-testid="mcp-capability-reset"
            disabled={readOnly || overriddenKeys.length === 0}
            loading={saving}
            onClick={() => void onReset(overriddenKeys)}
          >
            {t('mcp.capabilities.reset')}
          </Button>
        </Tooltip>
      </Space>
    </Card>
  );
};

export default CapabilitySwitchesCard;
