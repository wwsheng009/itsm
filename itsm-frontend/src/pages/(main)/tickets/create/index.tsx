import { useNavigate } from 'react-router';

/**
 * 工单创建页（富文本 / 附件增强版）
 *
 * 设计文档：docs/architecture/ticket-create-page-rich-input-optimization.md
 *  - §4.1 头部条 + 主区(lg=16) + 侧区(lg=8) 栅格布局，去掉常驻类型列表；
 *  - §4.2 类型选择改为弹层（TicketTypePickerModal），已选以紧凑徽标回显；
 *  - §4.3 描述接入 RichTextEditor，纯文本派生写入 description（列表/搜索/通知兼容）；
 *  - §4.4 粘贴/拖拽图片先以 blob 占位，工单创建成功后上传附件并回写正式地址；
 *  - §4.5 附件两段式：提交拿 ticketId → 后台上传 → 失败可重试；失败不阻断建单；
 *  - §4.6 自定义字段统一走 DynamicFieldRenderer；
 *  - §4.3 降级开关 VITE_RICH_TEXT=off 时回退 Input.TextArea。
 */

import React, { useCallback, useEffect, useMemo, useRef, useState, lazy, Suspense } from 'react';
import { Alert, App, Button, Card, Col, Divider, Form, Input, Row, Space, Spin, Tag, Typography } from 'antd';
import { AppstoreOutlined } from '@ant-design/icons';
import AppSelect from '@/components/ui/AppSelect';
import { ArrowLeft, Pencil, Paperclip, Sparkles } from 'lucide-react';
import { TicketApi } from '@/lib/api/ticket-api';
import { TicketCategoryApi } from '@/lib/api/ticket-category-api';
import {
  AttachmentApi,
  ticketAttachmentContentUrl,
  ticketAttachmentPreviewUrl,
} from '@/lib/api/attachment-api';
import { useI18n } from '@/lib/i18n';
import { httpClient } from '@/lib/api/http-client';
import { TicketTypeApi } from '@/lib/api/ticketTypeApi';
import type { CustomFieldDefinition } from '@/types/ticket-type';
import { htmlToPlainText, isRichTextEmpty, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';
import { DEFAULT_ATTACHMENT_MAX_SIZE_MB } from '@/lib/upload/types';

import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';
import AttachmentField, {
  uploadAttachmentItems,
  type AttachmentFieldItem,
  type AttachmentUploader,
} from '@/components/common/attachment/AttachmentField';
import TicketTypePickerModal, { type TicketTypePickerItem } from '@/components/business/TicketTypePickerModal';
import TicketTypeIcon from '@/components/business/TicketTypeIcon';
import DynamicFieldRenderer, { type ReferenceSelectOptions } from '@/components/business/DynamicFieldRenderer';

const { Title, Text } = Typography;
const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，
// 也就不会请求编辑器 chunk（方案 §NF-2 / AC-9）。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense
    fallback={
    <div className="rich-text-editor" style={{ minHeight: 220 }}>
      <Spin size="small" style={{ margin: 12 }} />
    </div>
    }
  >
    <RichTextEditorLazy {...props} />
  </Suspense>
);

type Priority = 'low' | 'medium' | 'high' | 'urgent' | 'critical';
type TicketCreateType = 'incident' | 'service_request' | 'change' | 'problem';
type RuntimeTicketType = {
  id: number;
  code: string;
  name: string;
  description: string;
  icon: string;
  color: string;
  priority: Priority;
  workflowDefinitionKey?: string;
  fields: CustomFieldDefinition[];
};

const ATTACHMENT_FIELD_TYPES = ['attachment', 'file', 'upload'];

const priorityLabel = (priority: string): string => {
  switch (priority) {
    case 'critical': return '严重';
    case 'urgent': return '紧急';
    case 'high': return '高';
    case 'medium': return '中';
    case 'low': return '低';
    default: return priority;
  }
};

const priorityColor = (priority: string): string => {
  switch (priority) {
    case 'critical': return 'magenta';
    case 'urgent': return 'red';
    case 'high': return 'orange';
    default: return 'blue';
  }
};

const inferTicketType = (selectedType: RuntimeTicketType | null): TicketCreateType => {
  if (!selectedType) {
    return 'incident';
  }

  const value = `${selectedType.id} ${selectedType.code} ${selectedType.name} ${selectedType.workflowDefinitionKey || ''}`;
  if (/change|变更|ddl|firewall|domain/i.test(value)) {
    return 'change';
  }
  if (/problem|问题/i.test(value)) {
    return 'problem';
  }
  return 'service_request';
};

export default function CreateTicketPage() {
  const navigate = useNavigate();
  const { message, modal } = App.useApp();
  const { t } = useI18n();
  const [form] = Form.useForm();
  const richTextEnabled = isRichTextEnabled();

  const [loading, setLoading] = useState(false);
  const [ticketTypes, setTicketTypes] = useState<RuntimeTicketType[]>([]);
  const [ticketTypesLoading, setTicketTypesLoading] = useState(true);
  const [ticketTypesError, setTicketTypesError] = useState<string | null>(null);

  // 选中的工单类型 + 类型选择弹层
  const [selectedType, setSelectedType] = useState<RuntimeTicketType | null>(null);
  const [typePickerOpen, setTypePickerOpen] = useState(false);

  const [referenceOptions, setReferenceOptions] = useState<Record<'user' | 'department' | 'ci', { label: string; value: number }[]>>({ user: [], department: [], ci: [] });

  // 附件（暂存态）：工单创建成功后才真正上传
  const [stagedAttachments, setStagedAttachments] = useState<AttachmentFieldItem[]>([]);
  const [createdTicketId, setCreatedTicketId] = useState<number | null>(null);

  // 富文本粘贴/拖拽图片的暂存区：staged id -> File
  const stagedImagesRef = useRef<Map<string, File>>(new Map());
  const stagedImageSeqRef = useRef(0);

  // AI 分类建议
  const [aiSuggestions, setAiSuggestions] = useState<{
    category?: string;
    priority?: string;
    urgency?: string;
    confidence?: number;
    reasoning?: string;
  } | null>(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiError, setAiError] = useState<string | null>(null);
  // 分类下拉选项：从后端 TicketCategory 主数据动态拉取，避免前后端分类词表零重合
  const [categoryOptions, setCategoryOptions] = useState<{ label: string; value: string }[]>([]);
  const [categoryLoading, setCategoryLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    TicketTypeApi.list({ status: 'active', page: 1, pageSize: 100 })
      .then(result => {
        if (cancelled) return;
        setTicketTypes(result.types.map(type => ({
          id: type.id, code: type.code, name: type.name,
          description: type.description ?? '', icon: type.icon ?? 'FileText', color: type.color ?? '#1677ff',
          priority: type.defaultPriority ?? 'medium', workflowDefinitionKey: type.workflowDefinitionKey,
          fields: type.customFields.filter(field => field.visible !== false),
        })));
      })
      .catch(error => { if (!cancelled) setTicketTypesError(error instanceof Error ? error.message : '工单类型加载失败'); })
      .finally(() => { if (!cancelled) setTicketTypesLoading(false); });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    Promise.allSettled([
      httpClient.get<any>('/api/v1/users', { page: 1, pageSize: 200, status: 'active' }),
      httpClient.get<any>('/api/v1/departments', { page: 1, pageSize: 200 }),
      httpClient.get<any>('/api/v1/configuration-items', { page: 1, size: 200 }),
    ]).then(([users, departments, cis]) => setReferenceOptions({
      user: users.status === 'fulfilled' ? (users.value.users ?? users.value.items ?? []).map((item: any) => ({ label: item.name ?? item.username, value: item.id })) : [],
      department: departments.status === 'fulfilled' ? (departments.value.departments ?? departments.value.items ?? departments.value ?? []).map((item: any) => ({ label: item.name, value: item.id })) : [],
      ci: cis.status === 'fulfilled' ? (cis.value.items ?? cis.value.cis ?? []).map((item: any) => ({ label: item.name, value: item.id })) : [],
    }));
  }, []);

  useEffect(() => {
    let cancelled = false;
    const loadCategories = async () => {
      setCategoryLoading(true);
      try {
        const res = await TicketCategoryApi.getCategories({ isActive: true, pageSize: 200 });
        if (cancelled) return;
        const list = res.items;
        setCategoryOptions(
          list
            .filter(c => c.isActive !== false)
            .map(c => ({ label: c.name, value: c.name })),
        );
      } catch (e) {
        console.error('加载工单分类失败', e);
      } finally {
        if (!cancelled) setCategoryLoading(false);
      }
    };
    loadCategories();
    return () => {
      cancelled = true;
    };
  }, []);

  // 跟踪上一次选中类型的动态字段名，切换类型时仅清理「新类型不存在」的字段值（按 name 交集保留）
  const prevFieldNames = useRef<string[]>([]);

  useEffect(() => {
    const nextNames = new Set(selectedType?.fields.map(f => f.name) ?? []);
    const staleNames = prevFieldNames.current.filter(name => !nextNames.has(name));
    if (staleNames.length > 0) {
      form.resetFields(staleNames);
    }
    prevFieldNames.current = selectedType?.fields.map(f => f.name) ?? [];
    if (selectedType) {
      form.setFieldValue('priority', selectedType.priority);
    }
  }, [selectedType, form]);

  // 提交/上传过程中防止误关闭页面丢失上传
  useEffect(() => {
    if (!loading) return;
    const handler = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [loading]);

  const normalizedReferenceOptions: ReferenceSelectOptions = useMemo(() => ({
    user: referenceOptions.user,
    department: referenceOptions.department,
    ci: referenceOptions.ci,
  }), [referenceOptions]);

  /**
   * 富文本图片「先占位、后上传」：此时工单尚不存在，返回 blob: 占位地址，
   * 提交后统一上传附件并回写正式地址（见 lib/rich-text/staged-images.ts）。
   */
  const handleEditorImageUpload = useCallback(async (file: File): Promise<UploadedImage> => {
    stagedImageSeqRef.current += 1;
    const id = `${STAGED_ID_PREFIX}${Date.now().toString(36)}-${stagedImageSeqRef.current}`;
    stagedImagesRef.current.set(id, file);
    return { url: URL.createObjectURL(file), id, name: file.name };
  }, []);

  const makeAttachmentUploader = useCallback((ticketId: number): AttachmentUploader => {
    // 经 AttachmentApi.uploader() 注入唯一上传实现（FE-1 工厂 + FE-4 传输层）；
    // 工单 + 缺省 usage 仍解析为 `/api/v1/tickets/:id/attachments`，URL 与权限不变。
    const upload = AttachmentApi.uploader();
    return async (file, onProgress) => {
      const uploaded = await upload(file, { bizType: 'ticket', bizId: ticketId }, onProgress);
      return {
        id: uploaded.id,
        url: uploaded.fileUrl || ticketAttachmentContentUrl(ticketId, uploaded.id),
      };
    };
  }, []);

  // 工单创建后，附件区切换为「即时上传 + 失败重试」模式
  const boundUploader = useMemo(
    () => (createdTicketId ? makeAttachmentUploader(createdTicketId) : undefined),
    [createdTicketId, makeAttachmentUploader],
  );

  const boundDeleter = useMemo(
    () => (createdTicketId
      ? async (item: AttachmentFieldItem) => {
        if (item.attachmentId) {
          await AttachmentApi.removeById(item.attachmentId, {
            bizType: 'ticket',
            bizId: createdTicketId,
          });
        }
      }
      : undefined),
    [createdTicketId],
  );

  const patchAttachment = useCallback((uid: string, patch: Partial<AttachmentFieldItem>) => {
    setStagedAttachments(prev => prev.map(item => (item.uid === uid ? { ...item, ...patch } : item)));
  }, []);

  /** 是否已填写内容（用于更换类型前的二次确认，见文档 §4.2） */
  const hasUserInput = useCallback((): boolean => {
    const values = form.getFieldsValue();
    const titleFilled = Boolean((values.title || '').toString().trim());
    const rawDescription = String(values.description || '');
    const descriptionFilled = richTextEnabled
      ? !isRichTextEmpty(rawDescription)
      : Boolean(rawDescription.trim());
    const customFilled = (selectedType?.fields ?? []).some((field) => {
      const value = values[field.name];
      return value !== undefined && value !== null && value !== '' && !(Array.isArray(value) && value.length === 0);
    });
    return titleFilled || descriptionFilled || customFilled;
  }, [form, richTextEnabled, selectedType]);

  const applyType = useCallback((type: RuntimeTicketType) => {
    setSelectedType(type);
    setTypePickerOpen(false);
  }, []);

  const handleSelectType = useCallback((item: TicketTypePickerItem) => {
    if (selectedType?.id === item.id) {
      setTypePickerOpen(false);
      return;
    }
    const full = ticketTypes.find(type => type.id === item.id) ?? (item as unknown as RuntimeTicketType);
    if (hasUserInput()) {
      modal.confirm({
        title: '更换工单类型',
        content: '更换类型会重新渲染自定义字段，已填写的通用字段（标题/描述）会保留。是否继续？',
        okText: '继续更换',
        cancelText: '取消',
        onOk: () => applyType(full),
      });
      return;
    }
    applyType(full);
  }, [applyType, hasUserInput, modal, selectedType, ticketTypes]);

  // 处理提交
  const handleSubmit = async () => {
    let values: Record<string, any>;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }

    setLoading(true);
    const uploadKey = 'ticket-post-create-upload';

    try {
      const rawHtml = typeof values.description === 'string' ? values.description : '';
      const plainFromEditor = richTextEnabled
        ? htmlToPlainText(rawHtml, 20000)
        : String(values.description || '').trim();

      if (plainFromEditor.length < 10) {
        message.warning('描述至少需要10个字符，请详细描述问题');
        return;
      }

      // 构建描述：基础描述（富文本转纯文本） + 预设字段明细（保持既有后端语义）
      let description = plainFromEditor;
      if (selectedType?.fields && selectedType.fields.length > 0) {
        const fieldDetails = selectedType.fields
          .map(field => {
            const value = values[field.name];
            if (value === undefined || value === null || value === '') return null;
            if (ATTACHMENT_FIELD_TYPES.includes(field.type)) {
              const names = (Array.isArray(value) ? value : []).map((item: AttachmentFieldItem) => item?.name).filter(Boolean);
              return names.length > 0 ? `${field.label}: ${names.join('、')}` : null;
            }
            if (Array.isArray(value)) {
              const labels = value.map(v => field.options?.find(opt => opt.value === v)?.label || String(v));
              return labels.length > 0 ? `${field.label}: ${labels.join('、')}` : null;
            }
            const optionLabel = field.options?.find(opt => opt.value === value)?.label || value;
            return `${field.label}: ${optionLabel}`;
          })
          .filter(Boolean)
          .join('\n');

        if (fieldDetails) {
          description = `[${selectedType.name}]\n${fieldDetails}\n\n---\n${description}`;
        }
      }

      const title = values.title || (selectedType ? `${selectedType.name}请求` : '新建工单');
      const priority = values.priority || (selectedType ? selectedType.priority : 'medium');

      // field.name 已经是 schema 存储 key（snake_case），直接透传，不做驼峰转换。
      const formFields = selectedType ? selectedType.fields.reduce<Record<string, unknown>>((fields, field) => {
        const value = values[field.name];
        if (value === undefined || value === null || value === '') {
          return fields;
        }
        let normalized: unknown = value;
        if (ATTACHMENT_FIELD_TYPES.includes(field.type)) {
          // 附件条目含 File 对象，提交前降级为可序列化元数据
          normalized = (Array.isArray(value) ? value : []).map((item: AttachmentFieldItem) => ({
            uid: item.uid,
            fileName: item.name,
            size: item.size,
            attachmentId: item.attachmentId,
          }));
        } else if (field.type === 'date' && typeof value === 'object' && value && typeof (value as { format?: unknown }).format === 'function') {
          normalized = (value as { format: (fmt: string) => string }).format('YYYY-MM-DD');
        } else if (field.type === 'datetime' && typeof value === 'object' && value && typeof (value as { toISOString?: unknown }).toISOString === 'function') {
          normalized = (value as { toISOString: () => string }).toISOString();
        }
        fields[field.name] = normalized;
        return fields;
      }, {}) : undefined;

      // 创建请求中的 HTML 先剔除 blob 占位图，避免把前端临时地址写进库
      const strippedHtml = richTextEnabled ? stripStagedImages(rawHtml) : '';
      const stagedImageIds = richTextEnabled ? extractStagedImageIds(rawHtml) : [];

      const created = await TicketApi.createTicket({
        title,
        description,
        descriptionHtml: strippedHtml || undefined,
        descriptionFormat: strippedHtml ? 'html' : 'plain',
        priority,
        type: inferTicketType(selectedType),
        ticketTypeId: selectedType?.id,
        category: values.category || undefined,
        formFields,
      });

      const createdId = created.id;
      setCreatedTicketId(createdId);

      const failureNames: string[] = [];
      const hasPendingAttachments = stagedAttachments.some(item => item.status !== 'done' && item.file);
      if (hasPendingAttachments || stagedImageIds.length > 0) {
        message.open({ key: uploadKey, type: 'loading', content: '正在上传附件…', duration: 0 });
      }

      // 1) 附件字段：两段式上传，单项失败不阻断其他项
      if (hasPendingAttachments) {
        const uploaded = await uploadAttachmentItems(
          stagedAttachments,
          makeAttachmentUploader(createdId),
          patchAttachment,
        );
        setStagedAttachments(uploaded);
        uploaded.filter(item => item.status === 'error').forEach(item => failureNames.push(item.name));
      }

      // 2) 正文内粘贴/拖拽图片：替换为正式附件地址并回写 descriptionHtml
      let editorImageFailures = 0;
      if (stagedImageIds.length > 0) {
        const replacements: Record<string, StagedImageReplacement> = {};
        for (const stagedId of stagedImageIds) {
          const file = stagedImagesRef.current.get(stagedId);
          if (!file) continue;
          try {
            // 正文内嵌图片：usage=inline_image，经域内端点透传（BE-10；通用 A1 的兜底码
            // attachment:write 仅 admin/sysadmin 持有，普通用户走通用路由会 403）
            const uploaded = await AttachmentApi.upload(file, {
              bizType: 'ticket',
              bizId: createdId,
              usage: 'inline_image',
            });
            replacements[stagedId] = {
              id: uploaded.id,
              // 内嵌图片优先走 preview（inline）；fileUrl 仅作兜底，避免落到
              // 带 Content-Disposition: attachment 的下载地址导致 <img> 不显示。
              url: uploaded.previewUrl || ticketAttachmentPreviewUrl(createdId, uploaded.id),
              name: uploaded.fileName || file.name,
            };
            stagedImagesRef.current.delete(stagedId);
          } catch {
            editorImageFailures += 1;
          }
        }

        const finalHtml = replaceStagedImages(rawHtml, replacements);
        try {
          await TicketApi.updateTicket(createdId, {
            description: htmlToPlainText(finalHtml, 20000),
            descriptionHtml: finalHtml,
            descriptionFormat: 'html',
            ...(typeof created.version === 'number' ? { version: created.version } : {}),
          } as any);
        } catch (e) {
          console.error('回写富文本描述失败', e);
          failureNames.push('正文富文本保存');
        }
      }

      message.destroy(uploadKey);

      if (failureNames.length > 0) {
        message.warning(`工单已创建，但以下内容上传失败：${failureNames.join('、')}。可在附件区点击「重试」，或直接进入工单详情。`);
        return;
      }
      if (editorImageFailures > 0) {
        message.warning(`${editorImageFailures} 张正文图片上传失败，请在工单详情页重新上传。`);
      }

      message.success('工单创建成功');
      navigate(`/tickets/${createdId}`);
    } catch (e: unknown) {
      message.destroy(uploadKey);
      console.error('Create ticket error:', e);
      const errorObj = e as { message?: string; error?: { message?: string } };
      const errorMsg =
        errorObj?.message || errorObj?.error?.message || '创建工单失败，请检查输入或重新登录';
      message.error(errorMsg);
    } finally {
      setLoading(false);
    }
  };

  // AI 智能分类
  const handleAITriage = async () => {
    try {
      const values = await form.validateFields();
      const title = values.title || (selectedType ? `${selectedType.name}请求` : '');
      const description = htmlToPlainText(String(values.description || ''), 500);

      if (!title) {
        message.warning('请先填写标题');
        return;
      }

      setAiLoading(true);
      setAiError(null);

      const response = await httpClient.post<any>('/api/v1/ai/triage', {
        title,
        description,
        category: values.category,
        priority: values.priority,
      });

      if (response?.suggestions) {
        setAiSuggestions(response.suggestions);
        // 自动应用建议：分类仅在与后端主数据匹配时回填，
        // 否则 AI 返回的英文 slug（database/network/…）与 TicketCategory 名称不匹配会导致提交失败
        if (response.suggestions.category && categoryOptions.some(o => o.value === response.suggestions.category)) {
          form.setFieldValue('category', response.suggestions.category);
        }
        if (response.suggestions.priority) {
          form.setFieldValue('priority', response.suggestions.priority);
        }
        message.success('AI 分类建议已应用');
      }
    } catch (e: unknown) {
      console.error('AI triage error:', e);
      setAiError('AI 分类服务暂时不可用');
    } finally {
      setAiLoading(false);
    }
  };

  return (
    <div className="max-w-6xl mx-auto p-4 md:p-6" role="main" aria-label="创建工单页面">
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        {/* A 头部条：类型只以「已选摘要」形式回显，点击弹层更换 */}
        <Card styles={{ body: { padding: '12px 16px' } }} data-testid="ticket-create-header">
          <div className="flex flex-wrap items-center gap-3">
            <Button
              icon={<ArrowLeft className="w-4 h-4" />}
              onClick={() => navigate(-1)}
              aria-label="返回上一页"
            >
              返回
            </Button>
            <div style={{ flex: 1, minWidth: 200 }}>
              <Title level={5} style={{ margin: 0 }}>
                新建工单
              </Title>
              <Text type="secondary" style={{ fontSize: 12 }}>
                选择工单类型，填写详细信息后提交
              </Text>
            </div>

            {selectedType ? (
              <Space size={8} wrap data-testid="selected-ticket-type">
                <span style={{ color: selectedType.color }} aria-hidden="true">
                  <TicketTypeIcon icon={selectedType.icon} />
                </span>
                <Text strong>{selectedType.name}</Text>
                <Tag color={selectedType.color} style={{ marginInlineEnd: 0 }}>已选</Tag>
                <Tag color={priorityColor(selectedType.priority)} style={{ marginInlineEnd: 0 }}>
                  {priorityLabel(selectedType.priority)}
                </Tag>
                {selectedType.workflowDefinitionKey && (
                  <Tag color="blue" style={{ marginInlineEnd: 0 }}>
                    {selectedType.workflowDefinitionKey}
                  </Tag>
                )}
                <Button
                  type="link"
                  icon={<Pencil className="w-3.5 h-3.5" />}
                  onClick={() => setTypePickerOpen(true)}
                  aria-label="更换工单类型"
                  data-testid="change-ticket-type"
                >
                  更换类型
                </Button>
              </Space>
            ) : (
              <Button
                type="primary"
                size="large"
                icon={<AppstoreOutlined />}
                onClick={() => setTypePickerOpen(true)}
                data-testid="open-type-picker"
              >
                选择工单类型
              </Button>
            )}
          </div>
        </Card>

        <Form form={form} layout="vertical" aria-label="工单表单" requiredMark="optional">
          <Row gutter={[16, 16]}>
            {/* B 主区：标题 / 描述（富文本）/ 自定义字段 */}
            <Col xs={24} lg={16}>
              <Card title="工单信息" aria-label="基础工单信息表单" data-testid="ticket-form">
                <Alert
                  type="info"
                  showIcon
                  className="mb-4"
                  message="填写最少信息即可提交；附件与正文图片会在工单创建成功后自动上传。"
                />

                <Form.Item
                  name="title"
                  label="标题"
                  rules={[
                    { required: true, message: '请输入标题' },
                    { min: 2, message: '标题至少需要2个字符' },
                  ]}
                >
                  <Input
                    placeholder="例如：VPN 无法连接"
                    aria-required="true"
                    aria-describedby="title-help"
                    data-testid="ticket-title-input"
                  />
                </Form.Item>

                <Form.Item
                  name="description"
                  label="详细描述"
                  rules={
                    richTextEnabled
                      ? [{ required: true, message: '请输入描述（至少10个字符）' }]
                      : [
                        { required: true, message: '请输入描述（至少10个字符）' },
                        { min: 10, message: '描述至少需要10个字符' },
                      ]
                  }
                  extra="支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（创建成功后自动上传）。"
                >
                  {richTextEnabled ? (
                    <RichTextEditor
                      placeholder="请详细描述问题/需求与影响范围..."
                      minHeight={220}
                      onUploadImage={handleEditorImageUpload}
                      dataTestId="ticket-description-input"
                    />
                  ) : (
                    <TextArea
                      rows={6}
                      placeholder="请详细描述问题/需求与影响范围..."
                      aria-required="true"
                      data-testid="ticket-description-input"
                    />
                  )}
                </Form.Item>

                {selectedType?.fields && selectedType.fields.length > 0 && (
                  <>
                    <Divider titlePlacement="left" style={{ marginTop: 8 }}>
                      {selectedType.name} · 类型专属字段
                    </Divider>
                    <Row gutter={[16, 0]}>
                      {selectedType.fields.map(field => (
                        <Col xs={24} md={12} key={field.name}>
                          <Form.Item
                            name={field.name}
                            label={field.label}
                            initialValue={field.defaultValue}
                            extra={field.description}
                            rules={
                              field.required
                                ? [{ required: true, message: `请填写${field.label}` }]
                                : []
                            }
                          >
                            <DynamicFieldRenderer
                              field={{
                                id: field.id,
                                name: field.name,
                                type: field.type,
                                required: field.required,
                                placeholder: field.placeholder,
                                description: field.description,
                                defaultValue: field.defaultValue,
                                options: field.options,
                                min: field.validation?.min,
                                max: field.validation?.max,
                                readonly: field.readonly,
                              }}
                              disabled={Boolean(field.readonly)}
                              referenceOptions={normalizedReferenceOptions}
                              onUploadImage={handleEditorImageUpload}
                              editorMinHeight={140}
                            />
                          </Form.Item>
                        </Col>
                      ))}
                    </Row>
                  </>
                )}

                <Row gutter={[16, 16]}>
                  <Col xs={24} sm={12}>
                    <Form.Item
                      name="priority"
                      label="优先级"
                      initialValue="medium"
                      rules={[{ required: true }]}
                    >
                      <AppSelect
                        allowClear
                        options={[
                          { label: '低', value: 'low' },
                          { label: '中', value: 'medium' },
                          { label: '高', value: 'high' },
                          { label: '紧急', value: 'urgent' },
                          { label: '严重', value: 'critical' },
                        ]}
                        placeholder="选择优先级"
                        aria-label="选择工单优先级"
                      />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={12}>
                    <Form.Item name="category" label="分类">
                      <AppSelect
                        allowClear
                        loading={categoryLoading}
                        options={categoryOptions}
                        placeholder={categoryLoading ? '加载分类中…' : '选择分类'}
                        aria-label="选择工单分类"
                      />
                    </Form.Item>
                  </Col>
                </Row>
              </Card>
            </Col>

            {/* C 侧区：附件 + AI 辅助 */}
            <Col xs={24} lg={8}>
              <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                <Card
                  size="small"
                  title={<Space size={6}><Paperclip className="w-4 h-4" />附件</Space>}
                  data-testid="ticket-attachments-card"
                >
                  <AttachmentField
                    value={stagedAttachments}
                    onChange={setStagedAttachments}
                    maxCount={10}
                    maxSizeMB={DEFAULT_ATTACHMENT_MAX_SIZE_MB}
                    uploader={boundUploader}
                    onDeleteUploaded={boundDeleter}
                    showTitle={false}
                    dataTestId="ticket-attachment-field"
                  />
                  {!createdTicketId && stagedAttachments.length > 0 && (
                    <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
                      工单创建成功后自动上传，失败可重试。
                    </Text>
                  )}
                </Card>

                <Card
                  size="small"
                  title={
                    <span className="flex items-center gap-2">
                      <Sparkles className="w-4 h-4 text-yellow-500" />
                      AI 辅助分类
                    </span>
                  }
                  data-testid="ticket-ai-card"
                >
                  <Spin spinning={aiLoading}>
                    {aiError && <Alert title={aiError} type="warning" showIcon className="mb-2" />}
                    {aiSuggestions ? (
                      <div className="space-y-2">
                        <div className="flex flex-wrap gap-2">
                          {aiSuggestions.category && (
                            <Tag color="blue">分类: {aiSuggestions.category}</Tag>
                          )}
                          {aiSuggestions.priority && (
                            <Tag color={aiSuggestions.priority === 'urgent' ? 'red' : 'orange'}>
                              优先级: {aiSuggestions.priority}
                            </Tag>
                          )}
                          {aiSuggestions.urgency && (
                            <Tag color="purple">紧急度: {aiSuggestions.urgency}</Tag>
                          )}
                        </div>
                        {aiSuggestions.reasoning && (
                          <Text type="secondary" className="text-sm">
                            {aiSuggestions.reasoning}
                          </Text>
                        )}
                        {aiSuggestions.confidence && (
                          <Text type="secondary" className="text-xs">
                            置信度: {Math.round(aiSuggestions.confidence * 100)}%
                          </Text>
                        )}
                      </div>
                    ) : (
                      <Text type="secondary">点击下方按钮获取AI智能分类建议</Text>
                    )}
                  </Spin>
                  <Button
                    type="default"
                    icon={<Sparkles className="w-4 h-4" />}
                    onClick={handleAITriage}
                    loading={aiLoading}
                    className="mt-2"
                    block
                  >
                    获取 AI 建议
                  </Button>
                </Card>
              </Space>
            </Col>
          </Row>

          {/* D 底部操作条 */}
          <div className="flex flex-col-reverse gap-3 sm:flex-row sm:justify-end mt-4" role="group" aria-label="表单操作按钮">
            {createdTicketId ? (
              <>
                <Text type="secondary" className="self-center">
                  工单已创建，附件上传失败可点击「重试」。
                </Text>
                <Button
                  type="primary"
                  size="large"
                  onClick={() => navigate(`/tickets/${createdTicketId}`)}
                  data-testid="goto-ticket-detail"
                >
                  进入工单详情
                </Button>
              </>
            ) : (
              <>
                <Button
                  onClick={() => navigate('/tickets')}
                  size="large"
                  aria-label="取消创建，返回工单列表"
                >
                  {t('common.cancel')}
                </Button>
                <Button
                  type="primary"
                  onClick={handleSubmit}
                  loading={loading}
                  size="large"
                  aria-busy={loading}
                  data-testid="ticket-submit-button"
                >
                  创建工单
                </Button>
              </>
            )}
          </div>
        </Form>
      </Space>

      <TicketTypePickerModal
        open={typePickerOpen}
        types={ticketTypes}
        value={selectedType}
        loading={ticketTypesLoading}
        error={ticketTypesError}
        onCancel={() => setTypePickerOpen(false)}
        onChange={handleSelectType}
      />
    </div>
  );
}
