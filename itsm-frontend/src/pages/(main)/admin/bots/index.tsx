import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert, App, AutoComplete, Button, Card, Descriptions, Drawer, Empty, Form, Input, Modal, Popconfirm,
  Select, Space, Spin, Table, Tag, Tooltip, Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ListChecks, Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import { useNavigate } from 'react-router';
import { PageContainer } from '@/components/common/PageContainer';
import { useI18n } from '@/lib/i18n/useI18n';
import { aiListToolCatalog, type ToolCatalogItem } from '@/lib/api/ai-api';
import botApi, {
  BOT_AUDIENCES,
  BOT_ENTRYPOINT_SUGGESTIONS,
  BOT_RISKS,
  BOT_RISK_RANK,
  BOT_SLUG_HINT_PATTERN,
  BOT_STATUSES,
  buildImpactPreview,
  describeBotError,
  grantRiskExceeds,
  isBotFeatureDisabled,
  isBotPermissionDenied,
  parseEntrypoints,
  serializeEntrypoints,
  type BotRisk,
  type BotGrant,
  type BotTemplate,
  type ImpactWarning,
} from '@/lib/api/bot-api';

const { Text, Paragraph } = Typography;

/** i18n 函数签名（与 useI18n 的 t 对齐）。 */
type TFunc = (key: string, params?: Record<string, string | number>) => string;

/** 模板表单值。 */
interface TemplateFormValues {
  slug: string;
  name: string;
  audience: string;
  riskLimit: string;
  entrypoints: string[];
  systemPromptRef?: string;
  status: string;
}

/** 授权表单值。 */
interface GrantFormValues {
  toolName: string;
  riskLimit: string;
  argsPolicyJson?: string;
}

const statusColor: Record<string, string> = { draft: 'default', pilot: 'blue', ga: 'green' };

/**
 * Bot 模板与工具授权管理页（B2-03，挂载 `/admin/bots`）。
 *
 * 三段式：模板列表（状态/受众/风险上限/入口）→ 新建/编辑（含**影响面预览**）→
 * 授权抽屉（工具 × 风险上限 × 参数策略，风险上限不得超过模板上限）。
 *
 * 权限与开关：
 *  - 读 `ai:read`、写 `ai:write`（后端 403 → 前端提示"只读"）；`bot.enabled=false` 时整组路由
 *    404 → 页面降级为"功能未启用"引导（读列表即触发判定）；
 *  - 变更审计在服务端（B2-01），前端不做任何本地持久化。
 */
const BotTemplatesPage: React.FC = () => {
  const { t } = useI18n();
  const tt = t as TFunc;
  const { message } = App.useApp();

  const [templates, setTemplates] = useState<BotTemplate[]>([]);
  const [loading, setLoading] = useState(false);
  const [featureDisabled, setFeatureDisabled] = useState(false);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<BotTemplate | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<TemplateFormValues>();

  const [grantsFor, setGrantsFor] = useState<BotTemplate | null>(null);
  const [grants, setGrants] = useState<BotGrant[]>([]);
  const [grantsLoading, setGrantsLoading] = useState(false);
  const [grantForm] = Form.useForm<GrantFormValues>();

  const navigate = useNavigate();
  // 授权抽屉的工具目录（内置 + MCP 统一投影；后端按 RBAC 过滤）：失败静默，仍可手工输入名称。
  const [toolCatalog, setToolCatalog] = useState<ToolCatalogItem[]>([]);
  const [toolCatalogLoading, setToolCatalogLoading] = useState(false);
  const [pickedTool, setPickedTool] = useState<ToolCatalogItem | null>(null);
  const toolCatalogLoadedRef = useRef(false);
  const toolSearchTimer = useRef<number | null>(null);

  const loadToolCatalog = useCallback(async (keyword: string) => {
    setToolCatalogLoading(true);
    try {
      const res = await aiListToolCatalog({ q: keyword || undefined, limit: 200 });
      setToolCatalog(Array.isArray(res?.items) ? res.items : []);
      toolCatalogLoadedRef.current = true;
    } catch {
      // 目录不可用（无权限/后端未装配）不阻塞授权流程：保留手工输入能力。
      setToolCatalog([]);
    } finally {
      setToolCatalogLoading(false);
    }
  }, []);

  const handleToolSearch = useCallback(
    (value: string) => {
      if (toolSearchTimer.current) window.clearTimeout(toolSearchTimer.current);
      toolSearchTimer.current = window.setTimeout(() => void loadToolCatalog(value.trim()), 300);
    },
    [loadToolCatalog]
  );

  useEffect(
    () => () => {
      if (toolSearchTimer.current) window.clearTimeout(toolSearchTimer.current);
    },
    []
  );

  /** 统一写操作错误处理：403 = 无 ai:write（只读提示）；其余走 describeBotError。 */
  const handleWriteError = useCallback(
    (err: unknown) => {
      if (isBotPermissionDenied(err)) {
        message.warning(tt('botsAdmin.permissionWriteDenied'));
        return;
      }
      message.error(describeBotError(err, tt('botsAdmin.actionFailed')));
    },
    [message, tt]
  );

  const loadTemplates = useCallback(async () => {
    setLoading(true);
    try {
      const res = await botApi.listTemplates();
      setTemplates(Array.isArray(res?.items) ? res.items : []);
      setFeatureDisabled(false);
    } catch (err) {
      if (isBotFeatureDisabled(err)) {
        setFeatureDisabled(true);
        setTemplates([]);
      } else if (isBotPermissionDenied(err)) {
        message.warning(tt('botsAdmin.permissionReadDenied'));
      } else {
        message.error(describeBotError(err, tt('botsAdmin.loadFailed')));
      }
    } finally {
      setLoading(false);
    }
  }, [message, tt]);

  useEffect(() => {
    void loadTemplates();
  }, [loadTemplates]);

  const openCreate = useCallback(() => {
    setEditing(null);
    form.setFieldsValue({
      slug: '',
      name: '',
      audience: 'internal',
      riskLimit: 'act_low',
      entrypoints: [],
      systemPromptRef: '',
      status: 'draft',
    });
    setEditorOpen(true);
  }, [form]);

  const openEdit = useCallback(
    (record: BotTemplate) => {
      setEditing(record);
      form.setFieldsValue({
        slug: record.slug,
        name: record.name,
        audience: record.audience || 'internal',
        riskLimit: record.riskLimit || 'act_low',
        entrypoints: parseEntrypoints(record.entrypointsJson),
        systemPromptRef: record.systemPromptRef ?? '',
        status: record.status || 'draft',
      });
      setEditorOpen(true);
    },
    [form]
  );

  const submitTemplate = useCallback(async () => {
    let values: TemplateFormValues;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }
    const payload = {
      name: values.name.trim(),
      audience: values.audience,
      riskLimit: values.riskLimit,
      entrypoints: (values.entrypoints ?? []).map(v => String(v).trim()).filter(Boolean),
      systemPromptRef: values.systemPromptRef?.trim() ?? '',
      status: values.status,
    };
    setSaving(true);
    try {
      if (editing) {
        await botApi.updateTemplate(editing.id, payload);
        message.success(tt('botsAdmin.saved'));
      } else {
        await botApi.createTemplate({ ...payload, slug: values.slug.trim() });
        message.success(tt('botsAdmin.created'));
      }
      setEditorOpen(false);
      await loadTemplates();
    } catch (err) {
      handleWriteError(err);
    } finally {
      setSaving(false);
    }
  }, [editing, form, handleWriteError, loadTemplates, message, tt]);

  const removeTemplate = useCallback(
    async (record: BotTemplate) => {
      try {
        await botApi.deleteTemplate(record.id);
        message.success(tt('botsAdmin.deleted'));
        await loadTemplates();
      } catch (err) {
        handleWriteError(err);
      }
    },
    [handleWriteError, loadTemplates, message, tt]
  );

  const loadGrants = useCallback(
    async (template: BotTemplate) => {
      setGrantsLoading(true);
      try {
        const res = await botApi.listGrants(template.id);
        setGrants(Array.isArray(res?.items) ? res.items : []);
      } catch (err) {
        if (isBotPermissionDenied(err)) {
          message.warning(tt('botsAdmin.permissionReadDenied'));
        } else {
          message.error(describeBotError(err, tt('botsAdmin.loadFailed')));
        }
        setGrants([]);
      } finally {
        setGrantsLoading(false);
      }
    },
    [message, tt]
  );

  const openGrants = useCallback(
    (record: BotTemplate) => {
      setGrantsFor(record);
      setGrants([]);
      setPickedTool(null);
      grantForm.resetFields();
      grantForm.setFieldsValue({ toolName: '', riskLimit: record.riskLimit || 'read', argsPolicyJson: '' });
      void loadGrants(record);
      void loadToolCatalog('');
    },
    [grantForm, loadGrants, loadToolCatalog]
  );

  const submitGrant = useCallback(async () => {
    if (!grantsFor) return;
    let values: GrantFormValues;
    try {
      values = await grantForm.validateFields();
    } catch {
      return;
    }
    if (grantRiskExceeds(grantsFor.riskLimit, values.riskLimit)) {
      message.error(tt('botsAdmin.grantsDrawer.riskLimitHint'));
      return;
    }
    try {
      await botApi.upsertGrant(grantsFor.id, {
        toolName: values.toolName.trim(),
        riskLimit: values.riskLimit,
        argsPolicyJson: values.argsPolicyJson?.trim() ?? '',
      });
      message.success(tt('botsAdmin.grantSaved'));
      grantForm.setFieldsValue({ toolName: '', argsPolicyJson: '' });
      await loadGrants(grantsFor);
    } catch (err) {
      handleWriteError(err);
    }
  }, [grantForm, grantsFor, handleWriteError, loadGrants, message, tt]);

  const removeGrant = useCallback(
    async (grant: BotGrant) => {
      if (!grantsFor) return;
      try {
        await botApi.deleteGrant(grantsFor.id, grant.id);
        message.success(tt('botsAdmin.grantDeleted'));
        await loadGrants(grantsFor);
      } catch (err) {
        handleWriteError(err);
      }
    },
    [grantsFor, handleWriteError, loadGrants, message, tt]
  );

  // 影响面预览：随表单值实时计算（未选择入口/草稿态/零授权都会显式告警）。
  const editorValues = Form.useWatch([], form) as TemplateFormValues | undefined;
  const impact = useMemo(
    () =>
      buildImpactPreview(
        {
          audience: editorValues?.audience ?? 'internal',
          entrypoints: editorValues?.entrypoints ?? [],
          status: editorValues?.status ?? 'draft',
        },
        editing ? grants.length : 0
      ),
    [editorValues, editing, grants.length]
  );

  const riskLabel = useCallback((risk: string) => tt(`botsAdmin.risk.${risk}`), [tt]);
  const statusLabel = useCallback((status: string) => tt(`botsAdmin.status.${status}`), [tt]);
  const audienceLabel = useCallback((audience: string) => tt(`botsAdmin.audience.${audience}`), [tt]);

  /** 工具目录下拉项：名称 + 来源（内置/MCP·服务器）+ 风险，便于选择时判断边界。 */
  const toolOptions = useMemo(
    () =>
      toolCatalog.map(item => {
        const source = item.provider === 'mcp' ? tt('toolsCatalog.mcpTag') : tt('toolsCatalog.builtin');
        const risk = item.risk ? riskLabel(item.risk) : tt('toolsCatalog.riskUnknown');
        const server = item.serverName ? ` · ${item.serverName}` : '';
        return { value: item.name, label: `${item.name}（${source}${server} · ${risk}）` };
      }),
    [riskLabel, toolCatalog, tt]
  );

  const catalogSummary = useMemo(() => {
    const mcp = toolCatalog.filter(item => item.provider === 'mcp').length;
    return { total: toolCatalog.length, mcp, builtin: toolCatalog.length - mcp };
  }, [toolCatalog]);

  /**
   * 选中目录条目：记录来源（MCP 提示）+ 按「工具风险 ≤ 模板上限」预填授权风险上限
   * （用户随后仍可手改；越界由前端预检 + 后端强校验双重拦截）。
   */
  const handleToolPick = useCallback(
    (value: string) => {
      const picked = toolCatalog.find(item => item.name === value) ?? null;
      setPickedTool(picked);
      if (!picked?.risk || !grantsFor) return;
      const toolRank = BOT_RISK_RANK[picked.risk as BotRisk];
      const limitRank = BOT_RISK_RANK[grantsFor.riskLimit as BotRisk];
      if (toolRank === undefined) return;
      const chosen: string =
        limitRank !== undefined && toolRank > limitRank ? grantsFor.riskLimit : picked.risk;
      grantForm.setFieldsValue({ riskLimit: chosen });
    },
    [grantForm, grantsFor, toolCatalog]
  );

  const columns: ColumnsType<BotTemplate> = [
    {
      title: tt('botsAdmin.columns.name'),
      dataIndex: 'name',
      key: 'name',
      render: (_, record) => (
        <Space direction="vertical" size={0}>
          <Text strong>{record.name}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {record.slug}
          </Text>
        </Space>
      ),
    },
    {
      title: tt('botsAdmin.columns.status'),
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (value: string) => <Tag color={statusColor[value] ?? 'default'}>{statusLabel(value)}</Tag>,
    },
    {
      title: tt('botsAdmin.columns.audience'),
      dataIndex: 'audience',
      key: 'audience',
      width: 130,
      render: (value: string) => audienceLabel(value || 'internal'),
    },
    {
      title: tt('botsAdmin.columns.riskLimit'),
      dataIndex: 'riskLimit',
      key: 'riskLimit',
      width: 120,
      render: (value: string) => <Tag>{riskLabel(value || 'read')}</Tag>,
    },
    {
      title: tt('botsAdmin.columns.entrypoints'),
      dataIndex: 'entrypointsJson',
      key: 'entrypoints',
      render: (value: string) => {
        const items = parseEntrypoints(value);
        return items.length ? (
          <Space size={4} wrap>
            {items.map(entry => (
              <Tag key={entry} color="geekblue">
                {entry}
              </Tag>
            ))}
          </Space>
        ) : (
          <Text type="secondary">{tt('botsAdmin.columns.entrypointsNone')}</Text>
        );
      },
    },
    {
      title: tt('botsAdmin.columns.actions'),
      key: 'actions',
      width: 240,
      render: (_, record) => (
        <Space size={4}>
          <Button size="small" type="link" onClick={() => openEdit(record)}>
            {tt('botsAdmin.edit')}
          </Button>
          <Button
            size="small"
            type="link"
            icon={<ListChecks size={14} />}
            onClick={() => openGrants(record)}
          >
            {tt('botsAdmin.grants')}
          </Button>
          <Popconfirm
            title={tt('botsAdmin.deleteConfirm')}
            onConfirm={() => void removeTemplate(record)}
            okText={tt('botsAdmin.confirm')}
            cancelText={tt('botsAdmin.cancel')}
          >
            <Button size="small" type="link" danger icon={<Trash2 size={14} />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const warningText = useCallback((code: ImpactWarning) => tt(`botsAdmin.impact.warn.${code}`), [tt]);

  return (
    <PageContainer
      header={{
        title: tt('botsAdmin.title'),
        breadcrumb: {
          items: [
            { title: tt('botsAdmin.breadcrumbHome') },
            { title: tt('botsAdmin.breadcrumbCurrent') },
          ],
        },
      }}
    >
      {featureDisabled ? (
        <Alert
          type="info"
          showIcon
          message={tt('botsAdmin.featureDisabledTitle')}
          description={tt('botsAdmin.featureDisabledHint')}
        />
      ) : (
        <Card
          title={tt('botsAdmin.title')}
          extra={
            <Space>
              <Button icon={<RefreshCw size={14} />} onClick={() => void loadTemplates()} loading={loading}>
                {tt('botsAdmin.refresh')}
              </Button>
              <Button type="primary" icon={<Plus size={14} />} onClick={openCreate}>
                {tt('botsAdmin.create')}
              </Button>
            </Space>
          }
        >
          <Table<BotTemplate>
            rowKey="id"
            size="small"
            loading={loading}
            dataSource={templates}
            columns={columns}
            pagination={false}
            locale={{ emptyText: <Empty description={tt('botsAdmin.empty')} /> }}
          />
        </Card>
      )}

      <Modal
        open={editorOpen}
        title={editing ? tt('botsAdmin.form.editTitle') : tt('botsAdmin.form.createTitle')}
        onCancel={() => setEditorOpen(false)}
        onOk={() => void submitTemplate()}
        confirmLoading={saving}
        okText={tt('botsAdmin.save')}
        cancelText={tt('botsAdmin.cancel')}
        destroyOnClose
        width={720}
      >
        <Form<TemplateFormValues> form={form} layout="vertical" preserve={false}>
          <Form.Item
            name="slug"
            label={tt('botsAdmin.form.slug')}
            rules={editing ? [] : [{ required: true, message: tt('botsAdmin.form.slugRequired') }]}
            extra={editing ? tt('botsAdmin.form.slugImmutable') : tt('botsAdmin.form.slugHint')}
          >
            <Input disabled={Boolean(editing)} placeholder="ticket-helper" />
          </Form.Item>
          <Form.Item
            name="name"
            label={tt('botsAdmin.form.name')}
            rules={[{ required: true, message: tt('botsAdmin.form.nameRequired') }]}
          >
            <Input maxLength={64} />
          </Form.Item>
          <Space size={12} style={{ display: 'flex' }} align="start">
            <Form.Item name="audience" label={tt('botsAdmin.form.audience')} style={{ minWidth: 180 }}>
              <Select
                options={BOT_AUDIENCES.map(value => ({ value, label: audienceLabel(value) }))}
              />
            </Form.Item>
            <Form.Item name="riskLimit" label={tt('botsAdmin.form.riskLimit')} style={{ minWidth: 180 }}>
              <Select options={BOT_RISKS.map(value => ({ value, label: riskLabel(value) }))} />
            </Form.Item>
            <Form.Item name="status" label={tt('botsAdmin.form.status')} style={{ minWidth: 180 }}>
              <Select options={BOT_STATUSES.map(value => ({ value, label: statusLabel(value) }))} />
            </Form.Item>
          </Space>
          <Form.Item
            name="entrypoints"
            label={tt('botsAdmin.form.entrypoints')}
            extra={tt('botsAdmin.form.entrypointsHint')}
          >
            <Select
              mode="tags"
              placeholder={BOT_ENTRYPOINT_SUGGESTIONS.join(', ')}
              options={BOT_ENTRYPOINT_SUGGESTIONS.map(value => ({ value, label: value }))}
            />
          </Form.Item>
          <Form.Item name="systemPromptRef" label={tt('botsAdmin.form.systemPromptRef')}>
            <Input placeholder="default-assistant" />
          </Form.Item>
          {/* slug 建议格式：仅软提示，不阻塞提交（后端只要求非空 + 租户唯一）。 */}
          {!editing && editorValues?.slug && !BOT_SLUG_HINT_PATTERN.test(editorValues.slug) ? (
            <Alert type="warning" showIcon message={tt('botsAdmin.form.slugFormatHint')} />
          ) : null}

          <Descriptions
            size="small"
            column={1}
            title={
              <Space size={6}>
                <ShieldCheck size={14} />
                {tt('botsAdmin.impact.title')}
              </Space>
            }
            style={{ marginTop: 8 }}
          >
            <Descriptions.Item label={tt('botsAdmin.impact.audiences')}>
              {impact.audiences.map(value => (
                <Tag key={value}>{audienceLabel(value)}</Tag>
              ))}
            </Descriptions.Item>
            <Descriptions.Item label={tt('botsAdmin.impact.entrypoints')}>
              {impact.entrypoints.length ? (
                <Space size={4} wrap>
                  {impact.entrypoints.map(value => (
                    <Tag key={value} color="geekblue">
                      {value}
                    </Tag>
                  ))}
                </Space>
              ) : (
                <Text type="secondary">{tt('botsAdmin.impact.none')}</Text>
              )}
            </Descriptions.Item>
            {impact.warnings.length ? (
              <Descriptions.Item label={tt('botsAdmin.impact.warnings')}>
                <Space direction="vertical" size={2}>
                  {impact.warnings.map(code => (
                    <Text key={code} type="warning">
                      {warningText(code)}
                    </Text>
                  ))}
                </Space>
              </Descriptions.Item>
            ) : null}
          </Descriptions>
        </Form>
      </Modal>

      <Drawer
        open={Boolean(grantsFor)}
        onClose={() => setGrantsFor(null)}
        width={760}
        title={tt('botsAdmin.grantsDrawer.title', { name: grantsFor?.name ?? '' })}
      >
        {grantsFor ? (
          <Space direction="vertical" size={12} style={{ display: 'flex' }}>
            <Alert
              type="info"
              showIcon
              message={tt('botsAdmin.grantsDrawer.templateLimit', {
                risk: riskLabel(grantsFor.riskLimit),
              })}
            />
            <Space direction="vertical" size={2} style={{ display: 'flex' }}>
              <Text type="secondary">{tt('botsAdmin.grantsDrawer.toolSearchHint')}</Text>
              <Space size={8} wrap>
                <Text type="secondary">
                  {tt('botsAdmin.grantsDrawer.toolFaceSummary', {
                    total: catalogSummary.total,
                    builtin: catalogSummary.builtin,
                    mcp: catalogSummary.mcp,
                  })}
                </Text>
                <Button type="link" size="small" onClick={() => navigate('/admin/tools')}>
                  {tt('botsAdmin.grantsDrawer.toolCatalogLink')}
                </Button>
              </Space>
            </Space>
            {pickedTool?.provider === 'mcp' ? (
              <Alert type="warning" showIcon message={tt('botsAdmin.grantsDrawer.toolPickedMcpHint')} />
            ) : null}
            <Form<GrantFormValues> form={grantForm} layout="inline" onFinish={() => void submitGrant()}>
              <Form.Item
                name="toolName"
                rules={[{ required: true, message: tt('botsAdmin.grantsDrawer.toolNameRequired') }]}
                style={{ minWidth: 280 }}
              >
                <AutoComplete
                  options={toolOptions}
                  onFocus={() => {
                    if (!toolCatalogLoadedRef.current) void loadToolCatalog('');
                  }}
                  onSearch={handleToolSearch}
                  onSelect={handleToolPick}
                  filterOption={(input, option) =>
                    String(option?.value ?? '').toLowerCase().includes(input.toLowerCase()) ||
                    String(option?.label ?? '').toLowerCase().includes(input.toLowerCase())
                  }
                  notFoundContent={toolCatalogLoading ? <Spin size="small" /> : null}
                >
                  <Input placeholder={tt('botsAdmin.grantsDrawer.toolName')} allowClear />
                </AutoComplete>
              </Form.Item>
              <Form.Item name="riskLimit" style={{ minWidth: 160 }}>
                <Select
                  aria-label={tt('botsAdmin.grantsDrawer.riskLimit')}
                  options={BOT_RISKS.map(value => ({ value, label: riskLabel(value) }))}
                />
              </Form.Item>
              <Form.Item name="argsPolicyJson" style={{ minWidth: 200 }}>
                <Input placeholder={tt('botsAdmin.grantsDrawer.argsPolicyHint')} />
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" icon={<Plus size={14} />}>
                  {tt('botsAdmin.grantsDrawer.upsert')}
                </Button>
              </Form.Item>
            </Form>
            <Table<BotGrant>
              rowKey="id"
              size="small"
              loading={grantsLoading}
              dataSource={grants}
              pagination={false}
              locale={{ emptyText: <Empty description={tt('botsAdmin.grantsDrawer.empty')} /> }}
              columns={[
                { title: tt('botsAdmin.grantsDrawer.toolName'), dataIndex: 'toolName', key: 'toolName' },
                {
                  title: tt('botsAdmin.grantsDrawer.riskLimit'),
                  dataIndex: 'riskLimit',
                  key: 'riskLimit',
                  width: 140,
                  render: (value: string) => (
                    <Tooltip title={grantRiskExceeds(grantsFor.riskLimit, value) ? tt('botsAdmin.grantsDrawer.riskLimitHint') : ''}>
                      <Tag color={grantRiskExceeds(grantsFor.riskLimit, value) ? 'red' : undefined}>
                        {riskLabel(value)}
                      </Tag>
                    </Tooltip>
                  ),
                },
                {
                  title: tt('botsAdmin.grantsDrawer.argsPolicyJson'),
                  dataIndex: 'argsPolicyJson',
                  key: 'argsPolicyJson',
                  render: (value: string) =>
                    value ? <Paragraph code>{value}</Paragraph> : <Text type="secondary">—</Text>,
                },
                {
                  title: tt('botsAdmin.columns.actions'),
                  key: 'actions',
                  width: 90,
                  render: (_, record) => (
                    <Popconfirm
                      title={tt('botsAdmin.deleteGrantConfirm')}
                      onConfirm={() => void removeGrant(record)}
                      okText={tt('botsAdmin.confirm')}
                      cancelText={tt('botsAdmin.cancel')}
                    >
                      <Button size="small" type="link" danger icon={<Trash2 size={14} />} />
                    </Popconfirm>
                  ),
                },
              ]}
            />
          </Space>
        ) : null}
      </Drawer>
    </PageContainer>
  );
};

export default BotTemplatesPage;
