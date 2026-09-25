import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  App,
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CloudDownload,
  FlaskConical,
  Pencil,
  Plus,
  RefreshCw,
  Star,
  Trash2,
} from 'lucide-react';

import {
  LLM_PROVIDER_IS_DEFAULT,
  LLMProviderApi,
  describeLLMProviderError,
  isLLMProviderFeatureDisabled,
  llmProviderErrorCode,
  type LLMCreateProviderRequest,
  type LLMProvider,
  type LLMProtocolOption,
  type LLMUpdateProviderRequest,
} from '@/lib/api/llm-provider-api';

const { Text } = Typography;
const { TextArea } = Input;

/**
 * 「系统管理 → 系统配置 → LLM 模型」页签（FE-2）。
 *
 * 契约：`docs/plan/multi-llm-provider-plan.md` §3.4，10 个端点全部要求 `system:write`。
 * 安全：列表只展示掩码密钥（`maskedApiKey`），新建/更新时密钥单向写入，不回显。
 * 灰度：开关关闭时整组路由 404 —— 本组件由宿主页 `useLLMProviderFeature` 门控，
 *       仅在探测成功（enabled=true）后挂载；此处再做一次防御性判定。
 *
 * 验收（§6.2 场景 10）：未实现协议选项置灰；422 错误码经 `describeLLMProviderError`
 * 转为可读中文提示。
 */

interface ProviderFormValues {
  name?: string;
  displayName: string;
  protocol: string;
  variant?: string;
  model: string;
  endpoint?: string;
  deployment?: string;
  apiKey?: string;
  adapterOptions?: string;
  enabled?: boolean;
  isDefault?: boolean;
  clearApiKey?: boolean;
  clearAdapterOptions?: boolean;
}

const parseAdapterOptions = (raw?: string): Record<string, unknown> | undefined => {
  const text = (raw ?? '').trim();
  if (!text) return undefined;
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new Error('高级参数必须是合法 JSON');
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error('高级参数必须是 JSON 对象（如 {"max_tokens": 1024}）');
  }
  return parsed as Record<string, unknown>;
};

const statusTag = (record: LLMProvider) => {
  if (record.status === 'ok') {
    return <Tag color="success">连通正常</Tag>;
  }
  if (record.status === 'error') {
    return (
      <Tooltip title={record.lastError || '最近一次连通性测试失败'}>
        <Tag color="error">连通失败</Tag>
      </Tooltip>
    );
  }
  return <Tag>{'未测试'}</Tag>;
};

export const LLMProviderSettings: React.FC = () => {
  const { message } = App.useApp();
  const [form] = Form.useForm<ProviderFormValues>();

  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [disabled, setDisabled] = useState(false);
  const [items, setItems] = useState<LLMProvider[]>([]);
  const [protocolOptions, setProtocolOptions] = useState<LLMProtocolOption[]>([]);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<LLMProvider | null>(null);
  const [testingId, setTestingId] = useState<number | null>(null);

  // 监听表单值：协议切换时重置 variant；启用状态与默认互斥。
  const selectedProtocol = Form.useWatch('protocol', form);
  const enabledValue = Form.useWatch('enabled', form);
  const clearApiKey = Form.useWatch('clearApiKey', form);
  const clearAdapterOptions = Form.useWatch('clearAdapterOptions', form);

  const activeProtocol = useMemo(
    () => protocolOptions.find(p => p.protocol === selectedProtocol),
    [protocolOptions, selectedProtocol]
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await LLMProviderApi.listProviders();
      setItems(res.items ?? []);
      setProtocolOptions(res.protocolOptions ?? []);
      setDisabled(false);
    } catch (err) {
      if (isLLMProviderFeatureDisabled(err)) {
        setDisabled(true);
      } else {
        message.error(describeLLMProviderError(err, '加载 LLM 实例列表失败'));
      }
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = useCallback(() => {
    setEditing(null);
    form.resetFields();
    const firstImplemented = protocolOptions.find(p => p.implemented);
    form.setFieldsValue({
      protocol: firstImplemented?.protocol ?? protocolOptions[0]?.protocol,
      variant: firstImplemented?.variants?.[0] ?? '',
      enabled: true,
      isDefault: false,
      clearApiKey: false,
      clearAdapterOptions: false,
    });
    setModalOpen(true);
  }, [form, protocolOptions]);

  const openEdit = useCallback(
    (record: LLMProvider) => {
      setEditing(record);
      form.resetFields();
      form.setFieldsValue({
        name: record.key,
        displayName: record.displayName,
        protocol: record.protocol,
        variant: record.variant || undefined,
        model: record.model,
        endpoint: record.endpoint,
        deployment: record.deployment,
        apiKey: '',
        adapterOptions: record.adapterOptions ? JSON.stringify(record.adapterOptions, null, 2) : '',
        enabled: record.enabled,
        clearApiKey: false,
        clearAdapterOptions: false,
      });
      setModalOpen(true);
    },
    [form]
  );

  const handleSubmit = useCallback(async () => {
    let values: ProviderFormValues;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }

    let adapterOptions: Record<string, unknown> | undefined;
    try {
      adapterOptions = parseAdapterOptions(values.adapterOptions);
    } catch (err) {
      form.setFields([{ name: 'adapterOptions', errors: [(err as Error).message] }]);
      return;
    }

    setSubmitting(true);
    try {
      if (editing) {
        const req: LLMUpdateProviderRequest = {
          displayName: values.displayName.trim(),
          protocol: values.protocol,
          variant: values.variant ?? '',
          model: values.model.trim(),
          endpoint: values.endpoint?.trim() ?? '',
          deployment: values.deployment?.trim() ?? '',
          enabled: values.enabled,
        };
        if (values.clearApiKey) {
          req.apiKey = '';
        } else if (values.apiKey) {
          req.apiKey = values.apiKey;
        }
        if (values.clearAdapterOptions) {
          req.adapterOptions = {};
        } else if (adapterOptions) {
          req.adapterOptions = adapterOptions;
        }
        await LLMProviderApi.updateProvider(editing.id, req);
        message.success('已保存修改');
      } else {
        const req: LLMCreateProviderRequest = {
          name: (values.name ?? '').trim(),
          displayName: values.displayName.trim(),
          protocol: values.protocol,
          variant: values.variant ?? '',
          model: values.model.trim(),
          endpoint: values.endpoint?.trim() || undefined,
          deployment: values.deployment?.trim() || undefined,
          apiKey: values.apiKey || undefined,
          adapterOptions,
          enabled: values.enabled,
          isDefault: Boolean(values.isDefault) && values.enabled !== false,
        };
        await LLMProviderApi.createProvider(req);
        message.success('实例已创建');
      }
      setModalOpen(false);
      await load();
    } catch (err) {
      message.error(describeLLMProviderError(err, editing ? '保存失败' : '创建失败'));
    } finally {
      setSubmitting(false);
    }
  }, [editing, form, load, message]);

  const handleTest = useCallback(
    async (record: LLMProvider) => {
      setTestingId(record.id);
      try {
        const res = await LLMProviderApi.testProvider(record.id);
        if (res.ok) {
          message.success(`「${record.displayName}」连通成功`);
        } else {
          message.error(`「${record.displayName}」连通失败：${res.error || '未知原因'}`);
        }
        await load();
      } catch (err) {
        message.error(describeLLMProviderError(err, '连通性测试失败'));
      } finally {
        setTestingId(null);
      }
    },
    [load, message]
  );

  const handleSetDefault = useCallback(
    async (record: LLMProvider) => {
      try {
        await LLMProviderApi.setDefaultProvider(record.id);
        message.success(`已将「${record.displayName}」设为租户默认`);
        await load();
      } catch (err) {
        message.error(describeLLMProviderError(err, '设置默认实例失败'));
      }
    },
    [load, message]
  );

  const handleToggleEnabled = useCallback(
    async (record: LLMProvider, next: boolean) => {
      try {
        await LLMProviderApi.updateProvider(record.id, { enabled: next });
        message.success(next ? '实例已启用' : '实例已禁用');
        await load();
      } catch (err) {
        message.error(describeLLMProviderError(err, '更新启停状态失败'));
      }
    },
    [load, message]
  );

  const handleDelete = useCallback(
    async (record: LLMProvider) => {
      try {
        await LLMProviderApi.deleteProvider(record.id);
        message.success(`已删除「${record.displayName}」`);
        await load();
      } catch (err) {
        if (llmProviderErrorCode(err) === LLM_PROVIDER_IS_DEFAULT) {
          message.error('默认实例不能删除，请先将其他实例设为默认');
        } else {
          message.error(describeLLMProviderError(err, '删除失败'));
        }
      }
    },
    [load, message]
  );

  const handleImportStatic = useCallback(async () => {
    try {
      const res = await LLMProviderApi.importStatic();
      message.success(res.created ? '已从静态配置导入实例' : '静态配置已同步（更新现有实例）');
      await load();
    } catch (err) {
      message.error(describeLLMProviderError(err, '导入静态配置失败'));
    }
  }, [load, message]);

  const columns: ColumnsType<LLMProvider> = useMemo(
    () => [
      {
        title: '实例',
        dataIndex: 'displayName',
        key: 'displayName',
        render: (_: unknown, record) => (
          <Space direction="vertical" size={0}>
            <Space size={6} wrap>
              <Text strong>{record.displayName}</Text>
              {record.isDefault ? <Tag color="gold">租户默认</Tag> : null}
              {record.enabled ? <Tag color="success">启用</Tag> : <Tag>已禁用</Tag>}
            </Space>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {record.key}
            </Text>
          </Space>
        ),
      },
      {
        title: '协议 / 变体',
        dataIndex: 'protocol',
        key: 'protocol',
        render: (_: unknown, record) => (
          <Space size={4} wrap>
            <Tag color="blue">{record.protocol}</Tag>
            {record.variant ? <Tag>{record.variant}</Tag> : null}
          </Space>
        ),
      },
      {
        title: '模型',
        dataIndex: 'model',
        key: 'model',
        render: (value: string, record) => (
          <Space direction="vertical" size={0}>
            <Text style={{ fontSize: 13 }}>{value}</Text>
            {record.endpoint ? (
              <Text type="secondary" style={{ fontSize: 12 }} ellipsis={{ tooltip: record.endpoint }}>
                {record.endpoint}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        title: 'API Key',
        dataIndex: 'hasApiKey',
        key: 'hasApiKey',
        render: (_: unknown, record) =>
          record.hasApiKey ? (
            <Text code>{record.maskedApiKey || '****'}</Text>
          ) : (
            <Tag color="warning">未配置</Tag>
          ),
      },
      {
        title: '连通状态',
        dataIndex: 'status',
        key: 'status',
        render: (_: unknown, record) => (
          <Space direction="vertical" size={0}>
            {statusTag(record)}
            <Text type="secondary" style={{ fontSize: 12 }}>
              {record.lastTestedAt ? new Date(record.lastTestedAt).toLocaleString('zh-CN') : '尚未测试'}
            </Text>
          </Space>
        ),
      },
      {
        title: '操作',
        key: 'actions',
        width: 320,
        render: (_: unknown, record) => (
          <Space size={4} wrap>
            <Button
              size="small"
              icon={<FlaskConical size={13} />}
              loading={testingId === record.id}
              onClick={() => handleTest(record)}
            >
              测试
            </Button>
            <Button
              size="small"
              icon={<Star size={13} />}
              disabled={record.isDefault || !record.enabled}
              onClick={() => handleSetDefault(record)}
            >
              设为默认
            </Button>
            <Button size="small" icon={<Pencil size={13} />} onClick={() => openEdit(record)}>
              编辑
            </Button>
            <Button size="small" onClick={() => handleToggleEnabled(record, !record.enabled)}>
              {record.enabled ? '禁用' : '启用'}
            </Button>
            <Popconfirm
              title={`删除「${record.displayName}」？`}
              description="删除后使用该实例的会话将回退到租户默认/静态配置。"
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={() => handleDelete(record)}
            >
              <Button size="small" danger icon={<Trash2 size={13} />} />
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [handleDelete, handleSetDefault, handleTest, handleToggleEnabled, openEdit, testingId]
  );

  // 灰度开关关闭（或不可达）时静默：宿主页不会挂载本组件，这里仅作防御。
  if (disabled) return null;

  return (
    <div className="space-y-4">
      <Alert
        type="info"
        showIcon
        message="多 LLM Provider 实例"
        description="为当前租户配置可选的大模型实例。API Key 仅单向写入（列表只显示掩码），不会回显明文；协议/变体决定请求编排方式，未接入的协议在接入前不可选。"
      />

      <Space>
        <Button type="primary" icon={<Plus size={14} />} onClick={openCreate}>
          新建实例
        </Button>
        <Button icon={<RefreshCw size={14} />} onClick={() => void load()}>
          刷新
        </Button>
        <Popconfirm
          title="从静态配置导入？"
          description="按实例名幂等导入/更新，不会重复创建。"
          okText="导入"
          cancelText="取消"
          onConfirm={handleImportStatic}
        >
          <Button icon={<CloudDownload size={14} />}>导入静态配置</Button>
        </Popconfirm>
      </Space>

      <Card className="rounded-lg shadow-sm border border-gray-200" styles={{ body: { padding: 0 } }}>
        <Table<LLMProvider>
          rowKey="id"
          size="middle"
          loading={loading}
          columns={columns}
          dataSource={items}
          pagination={false}
          locale={{ emptyText: '暂无实例，点击「新建实例」或「导入静态配置」开始。' }}
        />
      </Card>

      <Modal
        open={modalOpen}
        title={editing ? `编辑实例 · ${editing.displayName}` : '新建 LLM 实例'}
        width={720}
        onCancel={() => setModalOpen(false)}
        onOk={handleSubmit}
        okText={editing ? '保存' : '创建'}
        confirmLoading={submitting}
        destroyOnHidden
      >
        <Form<ProviderFormValues> form={form} layout="vertical" initialValues={{ enabled: true }}>
          <Form.Item
            label="实例标识（key）"
            name="name"
            rules={[{ required: true, message: '请输入实例标识' }]}
            extra={editing ? '实例标识不可修改（如需变更请删除后重建）' : '仅支持字母、数字、下划线、连字符，租户内唯一'}
          >
            <Input placeholder="例如 openai-gateway" disabled={Boolean(editing)} maxLength={64} />
          </Form.Item>

          <Form.Item
            label="显示名称"
            name="displayName"
            rules={[
              { required: true, message: '请输入显示名称' },
              { max: 100, message: '显示名称长度必须 ≤100' },
            ]}
          >
            <Input placeholder="例如 兼容网关（生产）" maxLength={100} />
          </Form.Item>

          <Form.Item
            label="协议"
            name="protocol"
            rules={[{ required: true, message: '请选择协议' }]}
            extra="未接入的协议（灰色）需要独立计划落地后才可使用"
          >
            <Select
              placeholder="请选择协议"
              options={protocolOptions.map(option => ({
                value: option.protocol,
                label: option.implemented ? option.protocol : `${option.protocol}（未接入）`,
                disabled: !option.implemented,
              }))}
              onChange={() => form.setFieldsValue({ variant: undefined })}
            />
          </Form.Item>

          <Form.Item label="协议变体（variant）" name="variant" extra="留空使用协议默认编排；可选值由协议决定">
            <Select
              allowClear
              placeholder="默认变体"
              disabled={!activeProtocol || activeProtocol.variants.length === 0}
              options={(activeProtocol?.variants ?? []).map(v => ({ value: v, label: v }))}
            />
          </Form.Item>

          <Form.Item
            label="模型"
            name="model"
            rules={[{ required: true, message: '请输入模型名' }]}
          >
            <Input placeholder="例如 gpt-4o-mini / deepseek-chat" />
          </Form.Item>

          <Form.Item label="Endpoint" name="endpoint" extra="留空回退协议内置地址；Azure/Ollama 等按需填写">
            <Input placeholder="https://api.example.com/v1" />
          </Form.Item>

          <Form.Item label="Deployment" name="deployment" extra="Azure OpenAI 等需要 deployment 的协议填写">
            <Input placeholder="例如 my-deployment" />
          </Form.Item>

          {editing ? (
            <Form.Item label="清空现有密钥" name="clearApiKey" valuePropName="checked">
              <Switch />
            </Form.Item>
          ) : null}

          <Form.Item
            label="API Key"
            name="apiKey"
            extra={editing ? '留空保持不变；输入新值则替换（单向写入，不回显）' : '仅写入加密存储，不回显明文'}
          >
            <Input.Password
              placeholder={editing ? '留空保持不变' : 'sk-...'}
              disabled={Boolean(clearApiKey)}
            />
          </Form.Item>

          <Form.Item
            label="高级参数（adapterOptions）"
            name="adapterOptions"
            extra='JSON 对象，≤4KB，敏感键（api_key / authorization 等）禁止写入'
          >
            <TextArea
              rows={3}
              placeholder='{"max_tokens": 1024}'
              disabled={Boolean(clearAdapterOptions)}
            />
          </Form.Item>

          {editing ? (
            <Form.Item label="清空高级参数" name="clearAdapterOptions" valuePropName="checked">
              <Switch />
            </Form.Item>
          ) : null}

          <Form.Item label="启用" name="enabled" valuePropName="checked">
            <Switch />
          </Form.Item>

          {!editing ? (
            <Form.Item
              label="设为租户默认"
              name="isDefault"
              valuePropName="checked"
              extra="同一租户仅一个默认实例，设置后会解绑原默认"
            >
              <Switch disabled={enabledValue === false} />
            </Form.Item>
          ) : null}
        </Form>
      </Modal>
    </div>
  );
};

export default LLMProviderSettings;
