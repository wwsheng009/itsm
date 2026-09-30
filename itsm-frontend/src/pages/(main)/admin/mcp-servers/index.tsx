import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert, App, Button, Card, Col, Descriptions, Drawer, Empty, Form, Input, InputNumber,
  List, Modal, Popconfirm, Row, Select, Space, Statistic, Steps, Switch, Table, Tabs, Tag, Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  Activity, FlaskConical, KeyRound, ListChecks, Plus, Power, PowerOff, RefreshCw, RotateCcw,
  Trash2,
} from 'lucide-react';
import { PageContainer } from '@/components/common/PageContainer';
import { UsageGuideCard } from '@/components/common/UsageGuideCard';
import { useI18n } from '@/lib/i18n/useI18n';
import { usePermissions } from '@/lib/hooks/use-permissions';
import mcpApi, {
  describeMCPError,
  isMCPFeatureDisabled,
  isMCPServiceUnavailable,
  type MCPConnectionTestResult,
  type MCPCreateServerRequest,
  type MCPRisk,
  type MCPServer,
  type MCPServerEvent,
  type MCPServerListResult,
  type MCPTool,
  type MCPTransport,
} from '@/lib/api/mcp-api';
import {
  SystemConfigAPI,
  type AICapabilities,
  type AICapabilitiesPatch,
  type AICapabilityKey,
} from '@/lib/api/system-config-api';
import CapabilitySwitchesCard from './CapabilitySwitchesCard';
import {
  PARALLEL_RANGE,
  RETRY_RANGE,
  SERVER_NAME_PATTERN,
  TIMEOUT_RANGE,
  credentialRequired,
  emptySummary,
  hasHealthAlert,
  maskedHint,
  pollExpired,
  runningStatusTone,
  shouldKeepPolling,
  toolStatusKey,
  validateServerURL,
} from './mcp-helpers';

const { Text } = Typography;

/** i18n 函数签名（与 useI18n 的 t 对齐）。 */
type TFunc = (key: string, params?: Record<string, string | number>) => string;

/** 键值对行 → Record；空数组返回 undefined（= 不修改/不使用）。 */
const toRecord = (rows?: Array<{ key?: string; value?: string }>): Record<string, string> | undefined => {
  const entries = (rows ?? []).filter((row) => row && typeof row.key === 'string' && row.key.trim() !== '');
  if (!entries.length) return undefined;
  const out: Record<string, string> = {};
  for (const row of entries) out[row.key!.trim()] = row.value ?? '';
  return out;
};

const POLL_INTERVAL_MS = 2000;
const POLL_TIMEOUT_MS = 30000;

/** 向导分步校验字段（下一步只校验当前步骤，避免跨步报错干扰）。 */
const WIZARD_STEP_FIELDS: string[][] = [
  ['name', 'display_name', 'transport', 'url'],
  ['credential_type', 'credential', 'headers'],
  ['timeout_ms', 'max_parallel_calls', 'max_retry', 'trust_level'],
];

const RISK_OPTIONS: MCPRisk[] = ['read', 'plan', 'act_low', 'act_medium', 'act_high'];
const TRANSPORT_OPTIONS: Array<{ value: MCPTransport; label: string }> = [
  { value: 'streamable', label: 'Streamable HTTP' },
  { value: 'sse', label: 'SSE（兼容）' },
];

/**
 * MCP 外部工具管理页（M0-12，Q3 独立页）。
 *
 * 三段式：服务器（列表 + 三步向导 + 测试连接 + 启停/重载）→ 工具治理（批量启停 +
 * 分类标注）→ 健康摘要（计数 + 生命周期事件）。
 *
 * 安全与状态口径：
 *  - 凭据只写不读回：编辑/轮换表单留空 = 不修改；已存凭据只展示掩码键名。
 *  - 启停为异步语义（202）：调用后按 2s 轮询回读，超时降级提示"仍在处理"。
 *  - 危险操作（禁用/删除/批量停用/全部启停）必须二次确认。
 */
export default function MCPServersAdminPage() {
  const { t } = useI18n();
  const { message, modal } = App.useApp();
  const { hasPermission } = usePermissions();

  const [tab, setTab] = useState('servers');
  const [loading, setLoading] = useState(false);
  const [list, setList] = useState<MCPServerListResult>({ items: [], summary: emptySummary });
  const [blocked, setBlocked] = useState<'disabled' | 'unavailable' | null>(null);

  // 能力开关卡片（运行时三键；展示禁用态优先用列表响应下发的 capabilities 块）。
  const [capabilities, setCapabilities] = useState<AICapabilities | null>(null);
  const [capabilitiesLoading, setCapabilitiesLoading] = useState(false);
  const [capabilitiesSaving, setCapabilitiesSaving] = useState(false);
  const [capabilitiesError, setCapabilitiesError] = useState(false);
  const canWriteCapabilities = hasPermission('system_config', 'write');

  // 服务器 tab 交互态
  const [wizardOpen, setWizardOpen] = useState(false);
  const [wizardStep, setWizardStep] = useState(0);
  const [editing, setEditing] = useState<MCPServer | null>(null);
  const [testOpen, setTestOpen] = useState(false);
  const [testTarget, setTestTarget] = useState<MCPServer | null>(null);
  const [testResult, setTestResult] = useState<MCPConnectionTestResult | null>(null);
  const [testLoading, setTestLoading] = useState(false);
  const [rotateTarget, setRotateTarget] = useState<MCPServer | null>(null);
  const [eventsTarget, setEventsTarget] = useState<MCPServer | null>(null);
  const [events, setEvents] = useState<MCPServerEvent[]>([]);

  // 工具治理 tab 交互态
  const [toolsServerId, setToolsServerId] = useState<number | null>(null);
  const [tools, setTools] = useState<MCPTool[]>([]);
  const [toolsLoading, setToolsLoading] = useState(false);
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [draft, setDraft] = useState<
    Record<string, { read_only?: boolean; risk?: MCPRisk; category?: string }>
  >({});
  const [savingTool, setSavingTool] = useState<string | null>(null);

  const [wizardForm] = Form.useForm();
  const [rotateForm] = Form.useForm();
  const pollTimer = useRef<number | null>(null);

  /** 下一步：仅校验当前步骤字段，校验通过才前进（失败保持本步并展示错误）。 */
  const goNextStep = useCallback(async () => {
    try {
      await wizardForm.validateFields(WIZARD_STEP_FIELDS[wizardStep]);
      setWizardStep((step) => Math.min(step + 1, WIZARD_STEP_FIELDS.length - 1));
    } catch {
      // 校验失败：错误提示由 Form 渲染，保持在当前步骤。
    }
  }, [wizardForm, wizardStep]);

  const servers = list.items;
  const summary = list.summary ?? emptySummary;
  const toolsServer = useMemo(
    () => servers.find((s) => s.id === toolsServerId) ?? null,
    [servers, toolsServerId],
  );

  // 展示用能力块：优先列表响应下发（与 MCP 页同权限面），缺省回落到开关卡片快照，再缺省按未管控。
  const displayCapabilities = useMemo(
    () => ({
      mcpEnabled: list.capabilities?.mcp_enabled ?? capabilities?.mcpEnabled ?? true,
      mcpWriteEnabled: list.capabilities?.mcp_write_enabled ?? capabilities?.mcpWriteEnabled ?? true,
      botEnabled: list.capabilities?.bot_enabled ?? capabilities?.botEnabled ?? true,
    }),
    [capabilities, list.capabilities],
  );
  const mcpWriteBlocked = !displayCapabilities.mcpWriteEnabled;

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await mcpApi.listServers();
      setList(result);
      setBlocked(null);
    } catch (e) {
      if (isMCPFeatureDisabled(e)) {
        setBlocked('disabled');
      } else if (isMCPServiceUnavailable(e)) {
        setBlocked('unavailable');
      } else {
        message.error(describeMCPError(e, t('mcp.loadFailed')));
      }
    } finally {
      setLoading(false);
    }
  }, [message, t]);

  useEffect(() => {
    void load();
  }, [load]);

  /** 读取运行时能力开关（无 system_config:read / 服务未就绪时降级提示，不阻塞页面）。 */
  const loadCapabilities = useCallback(async () => {
    setCapabilitiesLoading(true);
    try {
      const snapshot = await SystemConfigAPI.getAICapabilities();
      setCapabilities(snapshot);
      setCapabilitiesError(false);
    } catch {
      setCapabilitiesError(true);
    } finally {
      setCapabilitiesLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadCapabilities();
  }, [loadCapabilities]);

  useEffect(
    () => () => {
      if (pollTimer.current !== null) window.clearTimeout(pollTimer.current);
    },
    [],
  );

  /** 轮询回读服务器状态（D8：202 + 2s 轮询，超时降级提示）。 */
  const pollServer = useCallback(
    (id: number, targetEnabled: boolean) => {
      const startedAt = Date.now();
      const tick = async () => {
        try {
          const fresh = await mcpApi.getServer(id);
          setList((prev) => ({
            ...prev,
            items: prev.items.map((item) => (item.id === id ? fresh : item)),
          }));
          if (shouldKeepPolling(fresh, targetEnabled) && !pollExpired(startedAt, Date.now(), POLL_TIMEOUT_MS)) {
            pollTimer.current = window.setTimeout(tick, POLL_INTERVAL_MS);
            return;
          }
          if (shouldKeepPolling(fresh, targetEnabled)) {
            message.warning(t('mcp.poll.timeout'));
          }
        } catch {
          // 状态回读失败不打断页面；下一次用户刷新或操作会重试。
        }
      };
      pollTimer.current = window.setTimeout(tick, POLL_INTERVAL_MS);
    },
    [message, t],
  );

  const handleToggle = useCallback(
    async (server: MCPServer, enabled: boolean) => {
      try {
        if (enabled) {
          await mcpApi.enableServer(server.id);
        } else {
          await mcpApi.disableServer(server.id);
        }
        message.success(t(enabled ? 'mcp.enable.accepted' : 'mcp.disable.accepted'));
        await load();
        pollServer(server.id, enabled);
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.actionFailed')));
      }
    },
    [load, message, pollServer, t],
  );

  const confirmToggle = useCallback(
    (server: MCPServer, enabled: boolean) => {
      if (enabled) {
        void handleToggle(server, true);
        return;
      }
      modal.confirm({
        title: t('mcp.disable.confirmTitle', { name: server.display_name || server.name }),
        content: t('mcp.disable.confirmContent'),
        okText: t('common.confirm'),
        cancelText: t('common.cancel'),
        onOk: () => handleToggle(server, false),
      });
    },
    [handleToggle, modal, t],
  );

  const handleDelete = useCallback(
    async (server: MCPServer) => {
      try {
        await mcpApi.deleteServer(server.id);
        message.success(t('mcp.delete.deleted'));
        if (toolsServerId === server.id) {
          setToolsServerId(null);
          setTools([]);
        }
        await load();
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.actionFailed')));
      }
    },
    [load, message, t, toolsServerId],
  );

  const handleReload = useCallback(
    async (server: MCPServer) => {
      try {
        await mcpApi.reloadServer(server.id);
        message.success(t('mcp.reload.accepted'));
        await load();
        pollServer(server.id, server.enabled);
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.actionFailed')));
      }
    },
    [load, message, pollServer, t],
  );

  const runTest = useCallback(
    async (server: MCPServer, overrides?: Record<string, unknown>) => {
      setTestLoading(true);
      setTestResult(null);
      try {
        const result = await mcpApi.testServer(server.id, overrides);
        setTestResult(result);
      } catch (e) {
        setTestResult({
          ok: false,
          protocol_version: '',
          server_name: '',
          server_version: '',
          tools: [],
          tool_count: 0,
          duration_ms: 0,
          error_code: describeMCPError(e, 'internal_error'),
        });
      } finally {
        setTestLoading(false);
      }
    },
    [],
  );

  const openTools = useCallback(async (server: MCPServer) => {
    setTab('tools');
    setToolsServerId(server.id);
    setSelectedTools([]);
    setDraft({});
  }, []);

  const loadTools = useCallback(
    async (serverId: number) => {
      setToolsLoading(true);
      try {
        const items = await mcpApi.listTools(serverId);
        setTools(items);
        setDraft({});
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.tools.loadFailed')));
      } finally {
        setToolsLoading(false);
      }
    },
    [message, t],
  );

  useEffect(() => {
    if (tab === 'tools' && toolsServerId !== null) {
      void loadTools(toolsServerId);
    }
  }, [tab, toolsServerId, loadTools]);
  // 继续：服务器工具栏与表格见附录 chunk

  const reportCapabilityError = useCallback(
    (e: unknown) => {
      const status = (e as { httpStatus?: number } | undefined)?.httpStatus;
      if (status === 403) {
        message.warning(t('mcp.capabilities.writeDenied'));
        return;
      }
      message.error(describeMCPError(e, t('mcp.capabilities.saveFailed')));
    },
    [message, t],
  );

  /** 能力开关保存成功后刷新页面数据（列表 + 正在查看的工具治理）。 */
  const refreshAfterCapabilityChange = useCallback(async () => {
    await load();
    if (tab === 'tools' && toolsServerId !== null) {
      await loadTools(toolsServerId);
    }
  }, [load, loadTools, tab, toolsServerId]);

  /** 保存能力开关：仅提交脏字段（字段缺省 = 不修改）。 */
  const saveCapabilities = useCallback(
    async (patch: AICapabilitiesPatch) => {
      setCapabilitiesSaving(true);
      try {
        const snapshot = await SystemConfigAPI.updateAICapabilities(patch);
        setCapabilities(snapshot);
        setCapabilitiesError(false);
        message.success(t('mcp.capabilities.saved'));
        await refreshAfterCapabilityChange();
      } catch (e) {
        reportCapabilityError(e);
      } finally {
        setCapabilitiesSaving(false);
      }
    },
    [message, refreshAfterCapabilityChange, reportCapabilityError, t],
  );

  /** 恢复默认：删除覆盖行、恢复跟随环境默认（仅重置已覆盖的键，二次确认防误触）。 */
  const resetCapabilities = useCallback(
    (keys: AICapabilityKey[]) => {
      if (!keys.length) return;
      modal.confirm({
        title: t('mcp.capabilities.resetConfirmTitle'),
        content: t('mcp.capabilities.resetConfirmContent'),
        okText: t('common.confirm'),
        cancelText: t('common.cancel'),
        onOk: async () => {
          setCapabilitiesSaving(true);
          try {
            const snapshot = await SystemConfigAPI.updateAICapabilities({ reset: keys });
            setCapabilities(snapshot);
            setCapabilitiesError(false);
            message.success(t('mcp.capabilities.saved'));
            await refreshAfterCapabilityChange();
          } catch (e) {
            reportCapabilityError(e);
          } finally {
            setCapabilitiesSaving(false);
          }
        },
      });
    },
    [message, modal, refreshAfterCapabilityChange, reportCapabilityError, t],
  );

  const saveToolClassification = useCallback(
    async (tool: MCPTool) => {
      if (toolsServerId === null) return;
      const patch = draft[tool.callable_name];
      if (!patch) return;
      setSavingTool(tool.callable_name);
      try {
        const updated = await mcpApi.setToolClassification(toolsServerId, tool.callable_name, patch);
        setTools((prev) => prev.map((item) => (item.callable_name === tool.callable_name ? updated : item)));
        setDraft((prev) => {
          const next = { ...prev };
          delete next[tool.callable_name];
          return next;
        });
        message.success(t('mcp.tools.classificationSaved'));
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.actionFailed')));
      } finally {
        setSavingTool(null);
      }
    },
    [draft, message, t, toolsServerId],
  );

  const toggleToolEnabled = useCallback(
    async (tool: MCPTool, enabled: boolean) => {
      if (toolsServerId === null) return;
      try {
        const updated = await mcpApi.setToolEnabled(toolsServerId, tool.callable_name, enabled);
        setTools((prev) => prev.map((item) => (item.callable_name === tool.callable_name ? updated : item)));
        message.success(t(enabled ? 'mcp.tools.enabled' : 'mcp.tools.disabled'));
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.actionFailed')));
      }
    },
    [message, t, toolsServerId],
  );

  const bulkSetTools = useCallback(
    async (enabled: boolean, onlySelected: boolean) => {
      if (toolsServerId === null) return;
      const payload = onlySelected && selectedTools.length ? { tools: selectedTools, enabled } : { enabled };
      try {
        const result = await mcpApi.bulkSetTools(toolsServerId, payload);
        message.success(t('mcp.tools.bulkDone', { count: result.affected, state: enabled ? t('mcp.tools.enabledState') : t('mcp.tools.disabledState') }));
        setSelectedTools([]);
        await loadTools(toolsServerId);
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.actionFailed')));
      }
    },
    [loadTools, message, selectedTools, t, toolsServerId],
  );

  const confirmBulk = useCallback(
    (enabled: boolean) => {
      const targets = selectedTools.length;
      const scope = targets > 0 ? t('mcp.tools.bulkScopeSelected', { count: targets }) : t('mcp.tools.bulkScopeAll');
      modal.confirm({
        title: t(enabled ? 'mcp.tools.bulkEnableTitle' : 'mcp.tools.bulkDisableTitle'),
        content: t('mcp.tools.bulkConfirm', { scope }),
        okText: t('common.confirm'),
        cancelText: t('common.cancel'),
        okButtonProps: enabled ? undefined : { danger: true },
        onOk: () => bulkSetTools(enabled, targets > 0),
      });
    },
    [bulkSetTools, modal, selectedTools.length, t],
  );

  const openRotate = useCallback(
    (server: MCPServer) => {
      setRotateTarget(server);
      rotateForm.resetFields();
      rotateForm.setFieldsValue({ credential_type: server.credential_type || 'none' });
    },
    [rotateForm],
  );

  const submitRotate = useCallback(async () => {
    if (!rotateTarget) return;
    try {
      const values = await rotateForm.validateFields();
      await mcpApi.rotateCredential(rotateTarget.id, {
        credential_type: values.credential_type,
        credential: toRecord(values.credential),
        headers: toRecord(values.headers),
      });
      message.success(t('mcp.rotate.done'));
      setRotateTarget(null);
      await load();
    } catch (e) {
      if (e && typeof e === 'object' && 'errorFields' in e) return; // 表单校验失败
      message.error(describeMCPError(e, t('mcp.actionFailed')));
    }
  }, [load, message, rotateForm, rotateTarget, t]);

  const openWizard = useCallback(
    (server?: MCPServer) => {
      setEditing(server ?? null);
      wizardForm.resetFields();
      setWizardStep(0);
      if (server) {
        wizardForm.setFieldsValue({
          name: server.name,
          display_name: server.display_name,
          transport: server.transport,
          url: server.url,
          credential_type: server.credential_type || 'none',
          timeout_ms: server.timeout_ms,
          max_parallel_calls: server.max_parallel_calls,
          max_retry: server.max_retry,
          trust_level: server.trust_level || 'untrusted',
        });
      } else {
        wizardForm.setFieldsValue({
          transport: 'streamable_http',
          credential_type: 'none',
          trust_level: 'untrusted',
        });
      }
      setWizardOpen(true);
    },
    [wizardForm],
  );

  const submitWizard = useCallback(async () => {
    try {
      const values = await wizardForm.validateFields();
      if (editing) {
        await mcpApi.updateServer(editing.id, {
          version: editing.version,
          display_name: values.display_name,
          url: values.url,
          transport: values.transport,
          credential_type: values.credential_type,
          headers: toRecord(values.headers),
          credential: toRecord(values.credential),
          timeout_ms: values.timeout_ms,
          max_parallel_calls: values.max_parallel_calls,
          max_retry: values.max_retry,
          trust_level: values.trust_level,
        });
        message.success(t('mcp.save.updated'));
      } else {
        const payload: MCPCreateServerRequest = {
          name: values.name,
          display_name: values.display_name,
          transport: values.transport,
          url: values.url,
          headers: toRecord(values.headers),
          credential_type: values.credential_type,
          credential: toRecord(values.credential),
          timeout_ms: values.timeout_ms,
          max_parallel_calls: values.max_parallel_calls,
          max_retry: values.max_retry,
          trust_level: values.trust_level,
        };
        const created = await mcpApi.createServer(payload);
        message.success(t('mcp.save.created'));
        setTestTarget(created);
        setTestResult(null);
        setTestOpen(true);
      }
      setWizardOpen(false);
      await load();
    } catch (e) {
      if (e && typeof e === 'object' && 'errorFields' in e) return;
      message.error(describeMCPError(e, t('mcp.actionFailed')));
    }
  }, [editing, load, message, t, wizardForm]);

  const openEvents = useCallback(
    async (server: MCPServer) => {
      setEventsTarget(server);
      setEvents([]);
      try {
        setEvents(await mcpApi.listEvents(server.id));
      } catch (e) {
        message.error(describeMCPError(e, t('mcp.actionFailed')));
      }
    },
    [message, t],
  );

  const serverColumns: ColumnsType<MCPServer> = [
    {
      title: t('mcp.columns.name'),
      dataIndex: 'display_name',
      key: 'name',
      render: (_: unknown, server) => (
        <Space direction="vertical" size={0}>
          <Text strong>{server.display_name || server.name}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>{server.name}</Text>
        </Space>
      ),
    },
    {
      title: t('mcp.columns.transport'),
      dataIndex: 'transport',
      key: 'transport',
      width: 150,
      render: (value: string) => <Tag>{value === 'sse' ? 'SSE' : 'Streamable HTTP'}</Tag>,
    },
    {
      title: t('mcp.columns.url'),
      dataIndex: 'url',
      key: 'url',
      ellipsis: true,
      render: (value: string) => (
        <Tooltip title={value}>
          <Text code>{value}</Text>
        </Tooltip>
      ),
    },
    {
      title: t('mcp.columns.status'),
      key: 'status',
      width: 190,
      render: (_: unknown, server) => (
        <Space direction="vertical" size={2}>
          <Tag
            data-testid={`mcp-status-${server.id}`}
            color={
              runningStatusTone(server) === 'success'
                ? 'success'
                : runningStatusTone(server) === 'warning'
                  ? 'warning'
                  : runningStatusTone(server) === 'error'
                    ? 'error'
                    : 'default'
            }
          >
            {server.enabled ? server.running_status || server.status : t('mcp.status.disabled')}
          </Tag>
          {server.last_error ? (
            <Tooltip title={server.last_error}>
              <Text type="danger" style={{ fontSize: 12 }} ellipsis>
                {server.last_error}
              </Text>
            </Tooltip>
          ) : null}
          {server.protocol_version ? (
            <Text type="secondary" style={{ fontSize: 12 }}>{server.protocol_version}</Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('mcp.columns.tools'),
      key: 'tools',
      width: 150,
      render: (_: unknown, server) => (
        <Space direction="vertical" size={0}>
          <Text>{`${server.enabled_tool_count}/${server.tool_count}`}</Text>
          {server.quarantined_tool_count > 0 ? (
            <Tag color="warning" data-testid={`mcp-quarantine-${server.id}`}>
              {t('mcp.tools.quarantinedCount', { count: server.quarantined_tool_count })}
            </Tag>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('mcp.columns.enabled'),
      key: 'enabled',
      width: 110,
      render: (_: unknown, server) => (
        <Switch
          aria-label={t('mcp.columns.enabled')}
          checked={server.enabled}
          onChange={(checked) => confirmToggle(server, checked)}
        />
      ),
    },
    {
      title: t('mcp.columns.actions'),
      key: 'actions',
      width: 330,
      render: (_: unknown, server) => (
        <Space wrap size={4}>
          <Button
            size="small"
            icon={<FlaskConical size={14} />}
            onClick={() => {
              setTestTarget(server);
              setTestOpen(true);
              setTestResult(null);
              void runTest(server);
            }}
          >
            {t('mcp.actions.test')}
          </Button>
          <Button size="small" icon={<ListChecks size={14} />} onClick={() => void openTools(server)}>
            {t('mcp.actions.tools')}
          </Button>
          <Button size="small" onClick={() => openWizard(server)}>{t('mcp.actions.edit')}</Button>
          <Button size="small" icon={<KeyRound size={14} />} onClick={() => openRotate(server)}>
            {t('mcp.actions.rotate')}
          </Button>
          <Button size="small" icon={<RotateCcw size={14} />} onClick={() => void handleReload(server)}>
            {t('mcp.actions.reload')}
          </Button>
          <Button size="small" icon={<Activity size={14} />} onClick={() => void openEvents(server)}>
            {t('mcp.actions.events')}
          </Button>
          <Popconfirm
            title={t('mcp.delete.confirmTitle', { name: server.display_name || server.name })}
            description={t('mcp.delete.confirmContent')}
            okText={t('common.confirm')}
            cancelText={t('common.cancel')}
            okButtonProps={{ danger: true }}
            onConfirm={() => handleDelete(server)}
          >
            <Button size="small" danger icon={<Trash2 size={14} />}>{t('mcp.actions.delete')}</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const toolColumns: ColumnsType<MCPTool> = [
    {
      title: t('mcp.columns.callable'),
      dataIndex: 'callable_name',
      key: 'callable_name',
      render: (value: string, tool) => (
        <Space direction="vertical" size={0}>
          <Text code>{value}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>{tool.raw_name}</Text>
          {mcpWriteBlocked && !tool.read_only ? (
            <Tooltip title={t('mcp.capabilities.writeToolHint')}>
              <Tag color="warning" data-testid={`mcp-tool-write-blocked-${tool.callable_name}`}>
                {t('mcp.capabilities.writeToolBadge')}
              </Tag>
            </Tooltip>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('mcp.columns.description'),
      dataIndex: 'description',
      key: 'description',
      ellipsis: true,
    },
    {
      title: t('mcp.columns.readOnly'),
      key: 'read_only',
      width: 110,
      render: (_: unknown, tool) => (
        <Switch
          size="small"
          checked={draft[tool.callable_name]?.read_only ?? tool.read_only}
          onChange={(checked) =>
            setDraft((prev) => ({ ...prev, [tool.callable_name]: { ...prev[tool.callable_name], read_only: checked } }))
          }
        />
      ),
    },
    {
      title: t('mcp.columns.risk'),
      key: 'risk',
      width: 150,
      render: (_: unknown, tool) => (
        <Select<MCPRisk>
          size="small"
          style={{ width: 130 }}
          value={draft[tool.callable_name]?.risk ?? (tool.risk as MCPRisk)}
          options={RISK_OPTIONS.map((risk) => ({ value: risk, label: t(`mcp.risk.${risk}`) }))}
          onChange={(risk) =>
            setDraft((prev) => ({ ...prev, [tool.callable_name]: { ...prev[tool.callable_name], risk } }))
          }
        />
      ),
    },
    {
      title: t('mcp.columns.category'),
      key: 'category',
      width: 160,
      render: (_: unknown, tool) => (
        <Input
          size="small"
          maxLength={32}
          value={draft[tool.callable_name]?.category ?? tool.category}
          onChange={(event) =>
            setDraft((prev) => ({
              ...prev,
              [tool.callable_name]: { ...prev[tool.callable_name], category: event.target.value },
            }))
          }
        />
      ),
    },
    {
      title: t('mcp.columns.toolState'),
      key: 'state',
      width: 170,
      render: (_: unknown, tool) => {
        const status = toolStatusKey(tool);
        return (
          <Space direction="vertical" size={2}>
            <Tag
              data-testid={`mcp-tool-status-${tool.callable_name}`}
              color={status === 'active' ? 'success' : status === 'quarantined' ? 'error' : status === 'unhealthy' ? 'warning' : 'default'}
            >
              {t(`mcp.toolState.${status}`)}
            </Tag>
            {tool.quarantined && tool.quarantine_reason ? (
              <Tooltip title={t('mcp.tools.quarantineHint')}>
                <Text type="secondary" style={{ fontSize: 12 }}>{tool.quarantine_reason}</Text>
              </Tooltip>
            ) : null}
            {tool.last_error ? (
              <Text type="danger" style={{ fontSize: 12 }} ellipsis>{tool.last_error}</Text>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: t('mcp.columns.toolEnabled'),
      key: 'enabled',
      width: 100,
      render: (_: unknown, tool) => (
        <Switch
          size="small"
          aria-label={t('mcp.columns.toolEnabled')}
          checked={tool.configured_enabled}
          disabled={tool.quarantined}
          onChange={(checked) => void toggleToolEnabled(tool, checked)}
        />
      ),
    },
    {
      title: t('mcp.columns.actions'),
      key: 'actions',
      width: 130,
      render: (_: unknown, tool) => (
        <Button
          size="small"
          type={draft[tool.callable_name] ? 'primary' : 'default'}
          disabled={!draft[tool.callable_name]}
          loading={savingTool === tool.callable_name}
          onClick={() => void saveToolClassification(tool)}
        >
          {t('mcp.tools.saveClassification')}
        </Button>
      ),
    },
  ];

  const healthColumns: ColumnsType<MCPServer> = [
    { title: t('mcp.columns.name'), dataIndex: 'name', key: 'name' },
    {
      title: t('mcp.columns.status'),
      key: 'status',
      render: (_: unknown, server) => (
        <Tag color={runningStatusTone(server) === 'success' ? 'success' : runningStatusTone(server) === 'error' ? 'error' : 'default'}>
          {server.status}
        </Tag>
      ),
    },
    {
      title: t('mcp.columns.lastError'),
      dataIndex: 'last_error',
      key: 'last_error',
      ellipsis: true,
      render: (value: string) => (value ? <Text type="danger">{value}</Text> : <Text type="secondary">-</Text>),
    },
    {
      title: t('mcp.columns.protocolVersion'),
      dataIndex: 'protocol_version',
      key: 'protocol_version',
      width: 160,
      render: (value: string) => value || '-',
    },
    {
      title: t('mcp.columns.updatedAt'),
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 200,
    },
    {
      title: t('mcp.columns.actions'),
      key: 'actions',
      width: 130,
      render: (_: unknown, server) => (
        <Button size="small" icon={<Activity size={14} />} onClick={() => void openEvents(server)}>
          {t('mcp.actions.events')}
        </Button>
      ),
    },
  ];

  if (blocked) {
    return (
      <PageContainer
        header={{
          title: t('mcp.title'),
          breadcrumb: { items: [{ title: t('mcp.breadcrumbHome') }, { title: t('mcp.breadcrumbCurrent') }] },
        }}
      >
        <Alert
          data-testid="mcp-unavailable"
          type="warning"
          showIcon
          message={t(blocked === 'disabled' ? 'mcp.featureDisabled' : 'mcp.serviceUnavailable')}
          description={t(blocked === 'disabled' ? 'mcp.featureDisabledHint' : 'mcp.serviceUnavailableHint')}
          action={<Button size="small" onClick={() => void load()}>{t('mcp.retry')}</Button>}
        />
      </PageContainer>
    );
  }

  return (
    <PageContainer
      header={{
        title: t('mcp.title'),
        breadcrumb: { items: [{ title: t('mcp.breadcrumbHome') }, { title: t('mcp.breadcrumbCurrent') }] },
      }}
      extra={
        <Space>
          <Button icon={<RefreshCw size={14} />} loading={loading} onClick={() => void load()}>
            {t('mcp.refresh')}
          </Button>
          <Button type="primary" icon={<Plus size={14} />} onClick={() => openWizard()}>
            {t('mcp.add')}
          </Button>
        </Space>
      }
    >
      <UsageGuideCard
        style={{ marginBottom: 16 }}
        title={t('mcp.guide.title')}
        intro={t('mcp.guide.intro')}
        steps={[t('mcp.guide.step1'), t('mcp.guide.step2'), t('mcp.guide.step3'), t('mcp.guide.step4')]}
      />

      <CapabilitySwitchesCard
        capabilities={capabilities}
        canWrite={canWriteCapabilities}
        error={capabilitiesError}
        loading={capabilitiesLoading}
        saving={capabilitiesSaving}
        t={t}
        onSave={saveCapabilities}
        onReset={resetCapabilities}
      />

      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'servers',
            label: t('mcp.tabs.servers'),
            children: (
              <>
                <SummaryCards summary={summary} t={t} />
                {hasHealthAlert(summary) ? (
                  <Alert
                    style={{ marginBottom: 16 }}
                    type="warning"
                    showIcon
                    message={t('mcp.health.alert', { error: summary.error, auth: summary.auth_required })}
                  />
                ) : null}
                <Table<MCPServer>
                  rowKey="id"
                  size="small"
                  onRow={(record) => ({ 'data-testid': `mcp-row-${record.id}` }) as React.HTMLAttributes<HTMLElement>}
                  loading={loading}
                  dataSource={servers}
                  columns={serverColumns}
                  pagination={false}
                  locale={{
                    emptyText: (
                      <Empty
                        image={Empty.PRESENTED_IMAGE_SIMPLE}
                        description={
                          <Space direction="vertical" size={4}>
                            <Text>{t('mcp.empty.title')}</Text>
                            <Text type="secondary">{t('mcp.empty.hint')}</Text>
                          </Space>
                        }
                      >
                        <Button type="primary" icon={<Plus size={14} />} onClick={() => openWizard()}>
                          {t('mcp.add')}
                        </Button>
                      </Empty>
                    ),
                  }}
                />
              </>
            ),
          },
          {
            key: 'tools',
            label: t('mcp.tabs.tools'),
            children: (
              <>
                {mcpWriteBlocked ? (
                  <Alert
                    data-testid="mcp-tools-write-blocked"
                    style={{ marginBottom: 16 }}
                    type="warning"
                    showIcon
                    message={t('mcp.capabilities.writeDisabledAlert')}
                    description={t('mcp.capabilities.writeDisabledAlertHint')}
                  />
                ) : null}
                <Space style={{ marginBottom: 16 }} wrap>
                  <Select
                    style={{ minWidth: 260 }}
                    placeholder={t('mcp.tools.selectServer')}
                    value={toolsServerId ?? undefined}
                    options={servers.map((server) => ({
                      value: server.id,
                      label: `${server.display_name || server.name} (${server.name})`,
                    }))}
                    onChange={(value: number) => {
                      setToolsServerId(value);
                      setSelectedTools([]);
                    }}
                  />
                  <Tooltip title={selectedTools.length ? t('mcp.tools.bulkScopeSelected', { count: selectedTools.length }) : t('mcp.tools.bulkScopeAll')}>
                    <Space>
                      <Button
                        icon={<Power size={14} />}
                        disabled={toolsServerId === null}
                        onClick={() => confirmBulk(true)}
                      >
                        {t('mcp.tools.bulkEnable')}
                      </Button>
                      <Button
                        danger
                        icon={<PowerOff size={14} />}
                        disabled={toolsServerId === null}
                        onClick={() => confirmBulk(false)}
                      >
                        {t('mcp.tools.bulkDisable')}
                      </Button>
                    </Space>
                  </Tooltip>
                  <Button
                    icon={<RefreshCw size={14} />}
                    disabled={toolsServerId === null}
                    onClick={() => toolsServerId !== null && void loadTools(toolsServerId)}
                  >
                    {t('mcp.refresh')}
                  </Button>
                </Space>
                {toolsServer?.running_status && toolsServer.running_status !== 'connected' ? (
                  <Alert
                    style={{ marginBottom: 16 }}
                    type="info"
                    showIcon
                    message={t('mcp.tools.notConnected', { state: toolsServer.running_status })}
                  />
                ) : null}
                <Table<MCPTool>
                  rowKey="callable_name"
                  size="small"
                  onRow={(record) =>
                    ({ 'data-testid': `mcp-tool-row-${record.callable_name}` }) as React.HTMLAttributes<HTMLElement>
                  }
                  loading={toolsLoading}
                  dataSource={tools}
                  columns={toolColumns}
                  pagination={false}
                  rowSelection={{
                    selectedRowKeys: selectedTools,
                    onChange: (keys) => setSelectedTools(keys as string[]),
                  }}
                  locale={{
                    emptyText: (
                      <Empty
                        image={Empty.PRESENTED_IMAGE_SIMPLE}
                        description={
                          toolsServerId === null ? t('mcp.tools.emptySelect') : t('mcp.tools.empty')
                        }
                      />
                    ),
                  }}
                />
              </>
            ),
          },
          {
            key: 'health',
            label: t('mcp.tabs.health'),
            children: (
              <>
                <SummaryCards summary={summary} t={t} />
                <Table<MCPServer>
                  rowKey="id"
                  size="small"
                  loading={loading}
                  dataSource={servers}
                  columns={healthColumns}
                  pagination={false}
                  locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('mcp.empty.title')} /> }}
                />
              </>
            ),
          },
        ]}
      />
      {/* 继续：弹窗与抽屉见附录 */}

      <Modal
        open={wizardOpen}
        title={editing ? t('mcp.wizard.editTitle', { name: editing.name }) : t('mcp.wizard.createTitle')}
        width={720}
        onCancel={() => setWizardOpen(false)}
        onOk={() => (wizardStep < WIZARD_STEP_FIELDS.length - 1 ? void goNextStep() : void submitWizard())}
        okText={wizardStep < 2 ? t('mcp.wizard.next') : t('common.confirm')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Steps
          size="small"
          current={wizardStep}
          style={{ marginBottom: 16 }}
          items={[
            { title: t('mcp.wizard.stepBasic') },
            { title: t('mcp.wizard.stepAuth') },
            { title: t('mcp.wizard.stepAdvanced') },
          ]}
        />
        <Form form={wizardForm} layout="vertical">
          <div style={{ display: wizardStep === 0 ? 'block' : 'none' }}>
            <Form.Item
              name="name"
              label={t('mcp.wizard.name')}
              tooltip={t('mcp.wizard.nameHint')}
              rules={[
                { required: true, message: t('mcp.validation.nameRequired') },
                { pattern: SERVER_NAME_PATTERN, message: t('mcp.validation.namePattern') },
              ]}
            >
              <Input disabled={!!editing} placeholder="gitlab" maxLength={32} />
            </Form.Item>
            <Form.Item name="display_name" label={t('mcp.wizard.displayName')}>
              <Input placeholder={t('mcp.wizard.displayNamePlaceholder')} />
            </Form.Item>
            <Form.Item name="transport" label={t('mcp.wizard.transport')} rules={[{ required: true }]}>
              <Select options={TRANSPORT_OPTIONS} />
            </Form.Item>
            <Form.Item
              name="url"
              label={t('mcp.wizard.url')}
              rules={[
                {
                  validator: (_rule, value) => {
                    const key = validateServerURL(value);
                    return key ? Promise.reject(new Error(t(`mcp.${key}`))) : Promise.resolve();
                  },
                },
              ]}
            >
              <Input placeholder="https://mcp.example.com/mcp" />
            </Form.Item>
          </div>

          <div style={{ display: wizardStep === 1 ? 'block' : 'none' }}>
            <Form.Item name="credential_type" label={t('mcp.wizard.credentialType')}>
              <Select
                options={[
                  { value: 'none', label: t('mcp.credentialType.none') },
                  { value: 'static_header', label: t('mcp.credentialType.staticHeader') },
                  { value: 'oauth2', label: t('mcp.credentialType.oauth2') },
                ]}
              />
            </Form.Item>
            {editing && maskedHint(editing.credential_masked) ? (
              <Alert
                style={{ marginBottom: 16 }}
                type="info"
                showIcon
                message={t('mcp.wizard.maskedHint', { keys: maskedHint(editing.credential_masked) })}
              />
            ) : null}
            <Form.Item noStyle shouldUpdate={(prev, cur) => prev.credential_type !== cur.credential_type}>
              {({ getFieldValue }) => (
                <Form.Item
                  name="credential"
                  label={t('mcp.wizard.credential')}
                  tooltip={t('mcp.wizard.credentialHint')}
                  rules={[
                    {
                      validator: (_rule, value) => {
                        if (!credentialRequired(getFieldValue('credential_type'))) return Promise.resolve();
                        if (editing) return Promise.resolve(); // 编辑态留空 = 不修改
                        const rows = Array.isArray(value) ? value.filter((row) => row && row.key) : [];
                        return rows.length ? Promise.resolve() : Promise.reject(new Error(t('mcp.validation.credentialRequired')));
                      },
                    },
                  ]}
                >
                  <KeyValueList t={t} addLabel={t('mcp.wizard.addRow')} />
                </Form.Item>
              )}
            </Form.Item>
            {editing && Object.keys(editing.headers_masked ?? {}).length ? (
              <Alert
                style={{ marginBottom: 16 }}
                type="info"
                showIcon
                message={t('mcp.wizard.headersMaskedHint', { keys: maskedHint(editing.headers_masked) })}
              />
            ) : null}
            <Form.Item name="headers" label={t('mcp.wizard.headers')} tooltip={t('mcp.wizard.headersHint')}>
              <KeyValueList t={t} addLabel={t('mcp.wizard.addRow')} />
            </Form.Item>
          </div>

          <div style={{ display: wizardStep === 2 ? 'block' : 'none' }}>
            <Form.Item name="timeout_ms" label={t('mcp.wizard.timeout')}>
              <InputNumber min={TIMEOUT_RANGE.min} max={TIMEOUT_RANGE.max} step={1000} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="max_parallel_calls" label={t('mcp.wizard.parallel')}>
              <InputNumber min={PARALLEL_RANGE.min} max={PARALLEL_RANGE.max} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="max_retry" label={t('mcp.wizard.retry')}>
              <InputNumber min={RETRY_RANGE.min} max={RETRY_RANGE.max} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="trust_level" label={t('mcp.wizard.trustLevel')} tooltip={t('mcp.wizard.trustLevelHint')}>
              <Select
                options={[
                  { value: 'untrusted', label: t('mcp.trustLevel.untrusted') },
                  { value: 'trusted', label: t('mcp.trustLevel.trusted') },
                ]}
              />
            </Form.Item>
          </div>
        </Form>
      </Modal>

      <Modal
        open={testOpen}
        title={t('mcp.test.title', { name: testTarget?.display_name || testTarget?.name || '' })}
        onCancel={() => setTestOpen(false)}
        footer={[
          <Button key="close" onClick={() => setTestOpen(false)}>{t('mcp.close')}</Button>,
          <Button
            key="retry"
            type="primary"
            loading={testLoading}
            onClick={() => testTarget && void runTest(testTarget)}
          >
            {t('mcp.test.retry')}
          </Button>,
        ]}
        width={640}
      >
        {testLoading ? <Text>{t('mcp.test.running')}</Text> : null}
        {!testLoading && testResult ? (
          testResult.ok ? (
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <Descriptions size="small" column={2} bordered>
                <Descriptions.Item label={t('mcp.test.protocol')}>{testResult.protocol_version || '-'}</Descriptions.Item>
                <Descriptions.Item label={t('mcp.test.server')}>
                  {testResult.server_name || '-'} {testResult.server_version ? `v${testResult.server_version}` : ''}
                </Descriptions.Item>
                <Descriptions.Item label={t('mcp.test.duration')}>{`${testResult.duration_ms} ms`}</Descriptions.Item>
                <Descriptions.Item label={t('mcp.test.toolCount')}>{testResult.tool_count}</Descriptions.Item>
              </Descriptions>
              <div>
                <Text strong>{t('mcp.test.preview')}</Text>
                <List
                  size="small"
                  dataSource={testResult.tools}
                  locale={{ emptyText: t('mcp.test.noTools') }}
                  renderItem={(tool) => (
                    <List.Item>
                      <Space direction="vertical" size={0}>
                        <Text code>{tool.callable_name}</Text>
                        <Text type="secondary" style={{ fontSize: 12 }}>{tool.description}</Text>
                      </Space>
                    </List.Item>
                  )}
                />
              </div>
            </Space>
          ) : (
            <Alert
              data-testid="mcp-test-failed"
              type="error"
              showIcon
              message={t('mcp.test.failed')}
              description={`${testResult.error_code ? describeMCPError({ errorCode: testResult.error_code }, testResult.error_code) : ''} ${testResult.message ?? ''}`.trim()}
            />
          )
        ) : null}
      </Modal>

      <Modal
        open={!!rotateTarget}
        title={t('mcp.rotate.title', { name: rotateTarget?.display_name || rotateTarget?.name || '' })}
        onCancel={() => setRotateTarget(null)}
        onOk={() => void submitRotate()}
        okText={t('common.confirm')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Alert style={{ marginBottom: 16 }} type="warning" showIcon message={t('mcp.rotate.hint')} />
        <Form form={rotateForm} layout="vertical">
          <Form.Item name="credential_type" label={t('mcp.wizard.credentialType')} rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'none', label: t('mcp.credentialType.none') },
                { value: 'static_header', label: t('mcp.credentialType.staticHeader') },
                { value: 'oauth2', label: t('mcp.credentialType.oauth2') },
              ]}
            />
          </Form.Item>
          <Form.Item name="credential" label={t('mcp.wizard.credential')} tooltip={t('mcp.rotate.onlyWrite')}>
            <KeyValueList t={t} addLabel={t('mcp.wizard.addRow')} />
          </Form.Item>
          <Form.Item name="headers" label={t('mcp.wizard.headers')}>
            <KeyValueList t={t} addLabel={t('mcp.wizard.addRow')} />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        open={!!eventsTarget}
        width={520}
        title={t('mcp.events.title', { name: eventsTarget?.display_name || eventsTarget?.name || '' })}
        onClose={() => setEventsTarget(null)}
      >
        <List
          size="small"
          dataSource={events}
          locale={{ emptyText: t('mcp.events.empty') }}
          renderItem={(event) => (
            <List.Item>
              <Space direction="vertical" size={2}>
                <Space size={8}>
                  <Tag color={event.Type.includes('failed') || event.Type.includes('error') ? 'error' : 'blue'}>
                    {event.Type}
                  </Tag>
                  <Text type="secondary" style={{ fontSize: 12 }}>{event.At}</Text>
                </Space>
                <Text>{event.Detail}</Text>
                {event.Tool ? <Text code style={{ fontSize: 12 }}>{event.Tool}</Text> : null}
              </Space>
            </List.Item>
          )}
        />
      </Drawer>
    </PageContainer>
  );
}

/** 汇总卡：服务器总数 / 启用 / 已连接 / 异常 / 工具 / 隔离。 */
function SummaryCards({ summary, t }: { summary: MCPServerListResult['summary']; t: TFunc }) {
  const cards: Array<{ key: string; label: string; value: number; tone?: 'error' | 'warning' }> = [
    { key: 'total', label: t('mcp.summary.total'), value: summary.total },
    { key: 'enabled', label: t('mcp.summary.enabled'), value: summary.enabled },
    { key: 'connected', label: t('mcp.summary.connected'), value: summary.connected },
    { key: 'error', label: t('mcp.summary.error'), value: summary.error, tone: summary.error > 0 ? 'error' : undefined },
    { key: 'auth', label: t('mcp.summary.authRequired'), value: summary.auth_required, tone: summary.auth_required > 0 ? 'warning' : undefined },
    { key: 'tools', label: t('mcp.summary.tools'), value: summary.enabled_tools },
    { key: 'quarantined', label: t('mcp.summary.quarantined'), value: summary.quarantined_tools, tone: summary.quarantined_tools > 0 ? 'warning' : undefined },
  ];
  return (
    <Row gutter={[12, 12]} style={{ marginBottom: 16 }}>
      {cards.map((card) => (
        <Col key={card.key} xs={12} sm={8} md={6} lg={4} xl={3}>
          <Card size="small" data-testid={`mcp-summary-${card.key}`}>
            <Statistic
              title={card.label}
              value={card.value}
              valueStyle={card.tone === 'error' ? { color: '#cf1322' } : card.tone === 'warning' ? { color: '#d48806' } : undefined}
            />
          </Card>
        </Col>
      ))}
    </Row>
  );
}

interface KeyValueRow {
  key?: string;
  value?: string;
}

/**
 * 键值对编辑器（凭据/请求头）。
 *
 * 作为 antd `Form.Item` 的受控子组件使用（`value`/`onChange` 由 Form 注入）；
 * 值为密码输入框且不回显已存内容——编辑态留空 = 不修改（凭据只写不读回）。
 */
function KeyValueList({
  value,
  onChange,
  t,
  addLabel,
}: {
  value?: KeyValueRow[];
  onChange?: (rows: KeyValueRow[]) => void;
  t: TFunc;
  addLabel: string;
}) {
  const rows = value ?? [];
  const update = (next: KeyValueRow[]) => onChange?.(next);
  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      {rows.map((row, index) => (
        <Space key={index} align="start" style={{ width: '100%' }}>
          <Input
            placeholder={t('mcp.kv.key')}
            value={row.key}
            style={{ width: 200 }}
            onChange={(event) =>
              update(rows.map((item, i) => (i === index ? { ...item, key: event.target.value } : item)))
            }
          />
          <Input.Password
            placeholder={t('mcp.kv.value')}
            value={row.value}
            style={{ width: 300 }}
            onChange={(event) =>
              update(rows.map((item, i) => (i === index ? { ...item, value: event.target.value } : item)))
            }
          />
          <Button
            icon={<Trash2 size={14} />}
            aria-label={t('mcp.kv.remove')}
            onClick={() => update(rows.filter((_item, i) => i !== index))}
          />
        </Space>
      ))}
      <Button size="small" icon={<Plus size={14} />} onClick={() => update([...rows, { key: '', value: '' }])}>
        {addLabel}
      </Button>
    </Space>
  );
}
