import { useNavigate, useParams } from 'react-router';

import React, { useState, useEffect, useCallback, lazy, Suspense } from 'react';
import { Button, Card, Form, Input, Select, message, Row, Col, Space, Divider } from 'antd';
import { ArrowLeft, Save } from 'lucide-react';
import { IncidentAPI } from '@/lib/api/incident-api';
import type { Incident, UpdateIncidentRequest } from '@/lib/api/incident-api';
import { IncidentCategoryOptions } from '@/constants/taxonomy';
import { useI18n } from '@/lib/i18n';
import { htmlToPlainText, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import { useInlineImageUnbind } from '@/lib/rich-text/useInlineImageUnbind';
import { AttachmentApi, incidentAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，
// 也就不会请求编辑器 chunk（对齐服务请求表单 §4.3 / §NF-2）。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<div className="rich-text-editor" style={{ minHeight: 220 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

interface IncidentFormValues {
  title: string;
  description?: string;
  status: string;
  priority: string;
  severity: string;
  category?: string;
  subcategory?: string;
  source?: string;
}

export default function IncidentEditPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const params = useParams();
  const id = params?.id as string;
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [incidentData, setIncidentData] = useState<Incident | null>(null);
  const richTextEnabled = isRichTextEnabled();
  const [editorUploading, setEditorUploading] = useState(false);
  // 编辑态正文内嵌图片解绑：加载时记基线，保存成功后 diff 出被移除的图片（失败仅告警）
  const { captureInlineImageBaseline, unbindRemovedInlineImages } = useInlineImageUnbind();

  /** 编辑态事件 ID 已存在：图片即时上传，直接返回可渲染的域内预览地址 */
  const handleUploadImage = useCallback(
    async (file: File): Promise<UploadedImage> => {
      const incidentId = Number(id);
      if (!Number.isFinite(incidentId) || incidentId <= 0) {
        throw new Error('事件 ID 非法，无法上传图片');
      }
      const uploaded = await AttachmentApi.upload(file, {
        bizType: 'incident',
        bizId: incidentId,
        usage: 'inline_image',
      });
      return {
        id: uploaded.id,
        url: uploaded.previewUrl || incidentAttachmentPreviewUrl(incidentId, uploaded.id),
        name: uploaded.fileName || file.name,
      };
    },
    [id]
  );

  // Fetch incident data
  useEffect(() => {
    if (!id) return;

    let isMounted = true;
    const fetchIncident = async () => {
      setFetching(true);
      try {
        const resp = await IncidentAPI.getIncident(Number(id));
        if (!isMounted) return;
        const data = resp as any;
        setIncidentData(data);
        form.setFieldsValue({
          title: data.title,
          description: data.description,
          priority: data.priority,
          severity: data.severity,
          category: data.category,
          subcategory: data.subcategory,
          status: data.status,
        });
        captureInlineImageBaseline(data.description);
      } catch (error) {
        if (isMounted) {
          message.error(t('common.getFailed'));
          navigate('/incidents');
        }
      } finally {
        if (isMounted) {
          setFetching(false);
        }
      }
    };

    fetchIncident();
    return () => {
      isMounted = false;
    };
  }, [id, form, navigate, captureInlineImageBaseline]);

  const handleSubmit = async (values: IncidentFormValues) => {
    if (!id) return;

    // 图片仍在上传时提交会丢掉刚插入的图片，先阻断并提示。
    if (editorUploading) {
      message.warning('正文图片正在上传，请稍候再保存');
      return;
    }
    if (richTextEnabled) {
      const plainLength = htmlToPlainText(
        typeof values.description === 'string' ? values.description : '',
        Number.MAX_SAFE_INTEGER
      ).length;
      if (plainLength > 20000) {
        message.warning('事件描述最多 20000 字，请精简后再提交');
        return;
      }
    }

    setLoading(true);
    try {
      // source 不在后端 UpdateIncidentRequest 契约内，转发会被静默丢弃，故不提交。
      const payload: UpdateIncidentRequest = {
        title: values.title,
        description: values.description,
        status: values.status,
        priority: values.priority,
        severity: values.severity,
        category: values.category,
        subcategory: values.subcategory,
        // 乐观锁：回传读取时的版本，后端据此判定并发冲突并返回 4090。
        version: incidentData?.version,
      };
      await IncidentAPI.updateIncident(Number(id), payload);

      // 编辑器内被删除的图片：调用附件解绑接口（域内别名路由沿用 incident:delete，
      // 幂等、失败不阻断保存结果）。
      if (richTextEnabled) {
        await unbindRemovedInlineImages(
          { bizType: 'incident', bizId: Number(id) },
          values.description
        );
      }

      message.success(t('incidents.updateSuccess'));
      navigate(`/incidents/${id}`);
    } catch (error) {
      // 后端已把冲突（4090）、越权（2003）、非法状态迁移映射成语义化业务码并给出
      // 面向用户的文案；httpClient 只保留 message，故优先展示它，兜底才用通用文案。
      const detail = error instanceof Error ? error.message : '';
      message.error(detail || t('incidents.updateFailed'));
    } finally {
      setLoading(false);
    }
  };

  const handleCancel = () => {
    navigate(-1);
  };

  return (
    <div className="p-6 min-h-screen bg-gray-50">
      <div className="mb-6">
        <Button
          type="link"
          icon={<ArrowLeft />}
          onClick={() => navigate(-1)}
          style={{ paddingLeft: 0, color: '#666' }}
        >
          返回
        </Button>
      </div>

      <Card
        title={
          <span className="text-lg font-medium">编辑事件 - {incidentData?.incidentNumber}</span>
        }
        loading={fetching}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            priority: 'medium',
            severity: 'medium',
            status: 'new',
          }}
        >
          <Row gutter={24}>
            <Col span={24}>
              <Form.Item
                name="title"
                label="事件标题"
                rules={[{ required: true, message: '请输入事件标题' }]}
              >
                <Input placeholder="请输入事件标题" />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={12}>
              <Form.Item
                name="status"
                label="状态"
                rules={[{ required: true, message: '请选择状态' }]}
              >
                <Select placeholder="请选择状态" options={[
                  { value: 'new', label: '新建' },
                  { value: 'in_progress', label: '进行中' },
                  { value: 'resolved', label: '已解决' },
                  { value: 'closed', label: '已关闭' },
                ]} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                name="priority"
                label="优先级"
                rules={[{ required: true, message: '请选择优先级' }]}
              >
                <Select placeholder="请选择优先级" options={[
                  { value: 'low', label: '低' },
                  { value: 'medium', label: '中' },
                  { value: 'high', label: '高' },
                  { value: 'urgent', label: '紧急' },
                ]} />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={12}>
              <Form.Item
                name="severity"
                label="严重程度"
                rules={[{ required: true, message: '请选择严重程度' }]}
              >
                <Select placeholder="请选择严重程度" options={[
                  { value: 'low', label: '低' },
                  { value: 'medium', label: '中' },
                  { value: 'high', label: '高' },
                  { value: 'critical', label: '严重' },
                ]} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="category" label="分类">
                <Select placeholder="请选择分类" allowClear options={IncidentCategoryOptions} />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={12}>
              <Form.Item name="subcategory" label="子分类">
                <Input placeholder="请输入子分类" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="source" label="来源">
                <Select placeholder="请选择来源" allowClear options={[
                  { value: 'manual', label: '手动创建' },
                  { value: 'monitoring', label: '监控系统' },
                  { value: 'email', label: '邮件' },
                  { value: 'phone', label: '电话' },
                  { value: 'chat', label: '在线聊天' },
                  { value: 'api', label: 'API' },
                ]} />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={24}>
              <Form.Item
                name="description"
                label="事件描述"
                rules={[
                  {
                    validator: (_rule, value) => {
                      if (!richTextEnabled) return Promise.resolve();
                      const plainLength = htmlToPlainText(
                        typeof value === 'string' ? value : '',
                        Number.MAX_SAFE_INTEGER
                      ).length;
                      return plainLength > 20000
                        ? Promise.reject(new Error('事件描述最多 20000 字'))
                        : Promise.resolve();
                    },
                  },
                ]}
                extra={
                  richTextEnabled
                    ? '支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（上传后立即生效）。'
                    : undefined
                }
              >
                {richTextEnabled ? (
                  <RichTextEditor
                    placeholder="请详细描述事件情况"
                    minHeight={220}
                    onUploadImage={handleUploadImage}
                    onUploadingChange={setEditorUploading}
                    dataTestId="incident-description-input"
                  />
                ) : (
                  <TextArea rows={6} maxLength={20000} placeholder="请详细描述事件情况" />
                )}
              </Form.Item>
            </Col>
          </Row>

          <Divider />

          <Form.Item>
            <Space>
              <Button
                type="primary"
                htmlType="submit"
                icon={<Save />}
                loading={loading}
                disabled={editorUploading}
              >
                保存
              </Button>
              <Button onClick={handleCancel}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
