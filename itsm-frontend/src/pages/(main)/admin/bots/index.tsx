import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert, App, Button, Card, Descriptions, Drawer, Empty, Form, Input, Modal, Popconfirm,
  Select, Space, Table, Tag, Tooltip, Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ListChecks, Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import { PageContainer } from '@/components/common/PageContainer';
import { useI18n } from '@/lib/i18n/useI18n';
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
  type BotGrant,
  type BotRisk,
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
      grantForm.resetFields();
      grantForm.setFieldsValue({ toolName: '', riskLimit: record.riskLimit || 'read', argsPolicyJson: '' });
      void loadGrants(record);
    },
    [grantForm, loadGrants]
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
            <Form<GrantFormValues> form={grantForm} layout="inline" onFinish={() => void submitGrant()}>
              <Form.Item
                name="toolName"
                rules={[{ required: true, message: tt('botsAdmin.grantsDrawer.toolNameRequired') }]}
                style={{ minWidth: 200 }}
              >
                <Input placeholder={tt('botsAdmin.grantsDrawer.toolName')} />
              </Form.Item>
              <Form.Item name="riskLimit" style={{ minWidth: 160 }}>
                <Select options={BOT_RISKS.map(value => ({ value, label: riskLabel(value) }))} />
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
