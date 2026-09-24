import { useNavigate, useParams } from 'react-router';

import React, { useState, useEffect, useCallback, lazy, Suspense } from 'react';
import { Button, Card, Form, Input, Select, App, Row, Col, Space, Divider } from 'antd';
import { ArrowLeft, Save } from 'lucide-react';
import { ProblemApi } from '@/lib/api/problem-api';
import {
  ProblemCategoryOptions,
  isKnownProblemCategory,
} from '@/constants/problem';
import { useI18n } from '@/lib/i18n';
import { htmlToPlainText, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import { useInlineImageUnbind } from '@/lib/rich-text/useInlineImageUnbind';
import { AttachmentApi, problemAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，
// 也就不会请求编辑器 chunk（对齐服务请求表单 / 事件表单范式 §4.3 / §NF-2）。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<div className="rich-text-editor" style={{ minHeight: 160 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

export default function ProblemEditPage() {
  const navigate = useNavigate();
  const params = useParams();
  const id = params?.id as string;
  const { message } = App.useApp();
  const { t } = useI18n();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [problemData, setProblemData] = useState<any>(null);
  const richTextEnabled = isRichTextEnabled();
  const [editorUploading, setEditorUploading] = useState(false);
  // 编辑态正文内嵌图片解绑：加载时记基线，保存成功后 diff 出被移除的图片（失败仅告警）
  const { captureInlineImageBaseline, unbindRemovedInlineImages } = useInlineImageUnbind();

  /** 编辑态问题 ID 已存在：图片即时上传，直接返回可渲染的域内预览地址 */
  const handleUploadImage = useCallback(
    async (file: File): Promise<UploadedImage> => {
      const problemId = Number(id);
      if (!Number.isFinite(problemId) || problemId <= 0) {
        throw new Error('问题 ID 非法，无法上传图片');
      }
      const uploaded = await AttachmentApi.upload(file, {
        bizType: 'problem',
        bizId: problemId,
        usage: 'inline_image',
      });
      return {
        id: uploaded.id,
        url: uploaded.previewUrl || problemAttachmentPreviewUrl(problemId, uploaded.id),
        name: uploaded.fileName || file.name,
      };
    },
    [id]
  );

  // Fetch problem data
  useEffect(() => {
    if (!id) return;

    const fetchProblem = async () => {
      setFetching(true);
      try {
        const resp = await ProblemApi.getProblem(Number(id));
        const data = resp as any;
        setProblemData(data);
        // 后端可能带不在前枚举里的 category（旧数据 / 脏数据），
        // antd v6 Select 不识别时表现为“空白”，此处直接显示原始字符串 + 后缀提示。
        const rawCategory = typeof data.category === 'string' ? data.category : '';
        const safeCategory = isKnownProblemCategory(rawCategory) ? rawCategory : '';
        form.setFieldsValue({
          title: data.title,
          description: data.description,
          priority: data.priority,
          category: safeCategory,
          status: data.status,
          rootCause: data.rootCause,
          impact: data.impact,
        });
        captureInlineImageBaseline(data.description);
        if (rawCategory && !safeCategory) {
          message.warning(`原分类 “${rawCategory}” 不在当前枚举内，请重新选择`);
        }
      } catch (error) {
        message.error(t('problems.getFailed'));
        navigate('/problems');
      } finally {
        setFetching(false);
      }
    };

    fetchProblem();
  }, [id, form, navigate, captureInlineImageBaseline]);

  const handleSubmit = async (values: any) => {
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
        message.warning('问题描述最多 20000 字，请精简后再提交');
        return;
      }
    }

    setLoading(true);
    try {
      await ProblemApi.updateProblem(Number(id), values);

      // 编辑器内被删除的图片：调用附件解绑接口（域内别名路由沿用 problem:delete，
      // 幂等、失败不阻断保存结果）。
      if (richTextEnabled) {
        await unbindRemovedInlineImages(
          { bizType: 'problem', bizId: Number(id) },
          typeof values.description === 'string' ? values.description : ''
        );
      }

      message.success(t('problems.updateSuccess'));
      navigate(`/problems/${id}`);
    } catch (error) {
      message.error(t('problems.updateFailed'));
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
          <span className="text-lg font-medium">编辑问题 - #{problemData?.id}</span>
        }
        loading={fetching}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            priority: 'medium',
            status: 'open',
          }}
        >
          <Row gutter={24}>
            <Col span={24}>
              <Form.Item
                name="title"
                label="问题标题"
                rules={[{ required: true, message: '请输入问题标题' }]}
              >
                <Input placeholder="请输入问题标题" />
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
                <Select placeholder="请选择状态" options={[{ value: "open", label: "待处理" }, { value: "investigating", label: "调查中" }, { value: "resolved", label: "已解决" }, { value: "closed", label: "已关闭" }]} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                name="priority"
                label="优先级"
                rules={[{ required: true, message: '请选择优先级' }]}
              >
                <Select placeholder="请选择优先级" options={[{ value: "low", label: "低" }, { value: "medium", label: "中" }, { value: "high", label: "高" }, { value: "critical", label: "紧急" }]} />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={24}>
              <Form.Item name="category" label="分类">
                <Select
                  placeholder="请选择分类"
                  allowClear
                  // 旧数据 category 可能不在前枚举里，先把原始值附加为额外选项，
                  // 这样既能保留原有内容、又能让 Select 正常显示旧值。
                  options={[
                    ...ProblemCategoryOptions,
                    ...(problemData &&
                    problemData.category &&
                    !isKnownProblemCategory(problemData.category)
                      ? [
                          {
                            value: problemData.category,
                            label: `${problemData.category}（旧值）`,
                          },
                        ]
                      : []),
                  ]}
                />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={24}>
              <Form.Item
                name="description"
                label="问题描述"
                rules={[
                  {
                    validator: (_rule, value) => {
                      if (!richTextEnabled) return Promise.resolve();
                      const plainLength = htmlToPlainText(
                        typeof value === 'string' ? value : '',
                        Number.MAX_SAFE_INTEGER
                      ).length;
                      return plainLength > 20000
                        ? Promise.reject(new Error('问题描述最多 20000 字'))
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
                    placeholder="请详细描述问题情况"
                    minHeight={160}
                    onUploadImage={handleUploadImage}
                    onUploadingChange={setEditorUploading}
                    dataTestId="problem-description-input"
                  />
                ) : (
                  <TextArea rows={4} maxLength={20000} placeholder="请详细描述问题情况" />
                )}
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={24}>
              <Form.Item name="rootCause" label="根本原因分析">
                <TextArea rows={4} placeholder="请详细描述问题的根本原因" />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={24}>
            <Col span={24}>
              <Form.Item name="impact" label="影响范围">
                <TextArea rows={3} placeholder="请描述问题的影响范围" />
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
