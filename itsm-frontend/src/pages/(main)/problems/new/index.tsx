import { useNavigate, useSearchParams } from 'react-router';

import React, { useState, useEffect, useCallback, useRef, lazy, Suspense } from 'react';
import { ArrowLeft } from 'lucide-react';
import { Form, Input, Select, Button, Card, message, Alert, Spin } from 'antd';
import { ProblemApi } from '@/lib/api/problem-api';
import { ProblemPriority, ProblemCategoryOptions } from '@/constants/problem';
import { useI18n } from '@/lib/i18n';
import { htmlToPlainText, isRichTextEmpty, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';
import { AttachmentApi, problemAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，
// 也就不会请求编辑器 chunk（对齐服务请求表单 / 事件表单范式 §4.3 / §NF-2）。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<div className="rich-text-editor" style={{ minHeight: 140 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

/**
 * 事件转办的问题描述模板是纯文本（含换行）。
 * 富文本路径下先转义再按行包 `<p>`：既保留分段，又避免事件标题/描述里的
 * `<`、`>` 被 TipTap 当成标签解析（编辑器不执行脚本，但会静默吞掉这些字符）。
 */
function plainTextToEditorHtml(text: string): string {
  const escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
  return escaped
    .split(/\r?\n/)
    .map(line => `<p>${line || '<br/>'}</p>`)
    .join('');
}

const CreateProblemPageContent = () => {
  const navigate = useNavigate();
  const { t } = useI18n();
  const [searchParams] = useSearchParams();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const richTextEnabled = isRichTextEnabled();
  const uploadKey = 'problem-description-images';

  // 富文本图片「先占位、后上传」：问题创建成功前拿不到 problemId，
  // 故先在编辑器内以 blob: 占位，提交后统一上传并回写正式地址。
  const stagedImagesRef = useRef<Map<string, File>>(new Map());
  const stagedImageSeqRef = useRef(0);

  const handleEditorImageUpload = useCallback(async (file: File): Promise<UploadedImage> => {
    stagedImageSeqRef.current += 1;
    const id = `${STAGED_ID_PREFIX}${Date.now().toString(36)}-${stagedImageSeqRef.current}`;
    stagedImagesRef.current.set(id, file);
    return { url: URL.createObjectURL(file), id, name: file.name };
  }, []);

  useEffect(() => {
    const incidentId = searchParams.get('fromIncidentId');
    const incidentTitle = searchParams.get('incidentTitle');
    const incidentDescription = searchParams.get('incidentDescription');

    if (incidentId) {
      const descriptionText = `此问题由以下事件引发：\n事件ID: ${incidentId}\n事件标题: ${
        incidentTitle || ''
      }\n事件描述: ${incidentDescription || ''}\n\n请在此处填写问题的详细描述和根本原因分析...`;
      form.setFieldsValue({
        title: `由事件 ${incidentId} 引起的问题: ${incidentTitle || ''}`,
        description: richTextEnabled ? plainTextToEditorHtml(descriptionText) : descriptionText,
      });
    }
  }, [searchParams, form, richTextEnabled]);

  const handleSubmit = async (values: any) => {
    setLoading(true);
    try {
      // 富文本路径：description 落库 HTML（单字段范式），长度按净化后纯文本口径校验。
      const rawDescription = typeof values.description === 'string' ? values.description : '';
      const plainDescription = richTextEnabled
        ? htmlToPlainText(rawDescription, Number.MAX_SAFE_INTEGER)
        : String(values.description || '').trim();

      if (plainDescription.length > 20000) {
        message.warning('问题描述最多 20000 字，请精简后再提交');
        return;
      }

      // 编辑器内粘贴/拖拽的图片此刻只有 blob: 占位，不能落库：
      // 先剔除占位，待创建拿到 problemId 后上传并用正式地址回写。
      const stagedImageIds = richTextEnabled ? extractStagedImageIds(rawDescription) : [];
      const descriptionForCreate =
        stagedImageIds.length > 0 ? stripStagedImages(rawDescription) : rawDescription;

      const created = await ProblemApi.createProblem({
        title: values.title,
        description: descriptionForCreate,
        priority: values.priority,
        category: values.category,
        rootCause: values.rootCause,
        impact: values.impact,
        assigneeId: values.assigneeId,
      });

      // 正文图片两段式：上传 → 用正式地址替换暂存占位 → 回写 description。
      // 单项失败不阻断问题创建（未被替换的占位图会被丢弃，不会把 blob: 写进库）。
      const createdId = Number(created?.id ?? 0);
      if (stagedImageIds.length > 0 && createdId > 0) {
        message.open({ key: uploadKey, type: 'loading', content: '正在上传正文图片…', duration: 0 });
        const replacements: Record<string, StagedImageReplacement> = {};
        let imageFailures = 0;

        for (const stagedId of stagedImageIds) {
          const file = stagedImagesRef.current.get(stagedId);
          if (!file) continue;
          try {
            const uploaded = await AttachmentApi.upload(file, {
              bizType: 'problem',
              bizId: createdId,
              usage: 'inline_image',
            });
            replacements[stagedId] = {
              id: uploaded.id,
              url: uploaded.previewUrl || problemAttachmentPreviewUrl(createdId, uploaded.id),
              name: uploaded.fileName || file.name,
            };
            stagedImagesRef.current.delete(stagedId);
          } catch {
            imageFailures += 1;
          }
        }

        try {
          await ProblemApi.updateProblem(createdId, {
            description: replaceStagedImages(rawDescription, replacements),
          });
        } catch (e) {
          console.error('回写问题正文图片失败', e);
          imageFailures += 1;
        }
        message.destroy(uploadKey);
        if (imageFailures > 0) {
          message.warning(`${imageFailures} 张正文图片上传失败，可在详情页编辑补充。`);
        }
      }

      message.success(t('problems.createSuccess'));
      navigate('/problems');
    } catch (error) {
      message.destroy(uploadKey);
      console.error('创建问题失败:', error);
      message.error(t('problems.createFailed'));
    } finally {
      setLoading(false);
    }
  };

  const priorityOptions = [
    { value: ProblemPriority.LOW, label: '低' },
    { value: ProblemPriority.MEDIUM, label: '中' },
    { value: ProblemPriority.HIGH, label: '高' },
    { value: ProblemPriority.CRITICAL, label: '紧急' },
  ];

  return (
    <div className="p-10 bg-gray-50 min-h-full">
      <header className="mb-8">
        <button
          onClick={() => navigate(-1)}
          className="flex items-center text-blue-600 hover:underline mb-4"
          aria-label="返回问题列表"
        >
          <ArrowLeft className="w-5 h-5 mr-2" />
          返回问题列表
        </button>
        <h2 className="text-4xl font-bold text-gray-800">新建问题</h2>
        <p className="text-gray-500 mt-1">识别、分析和解决IT服务的根本原因</p>
      </header>

      <Card className="shadow-md">
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            priority: ProblemPriority.MEDIUM,
            category: '系统问题',
          }}
        >
          {searchParams.get('fromIncidentId') && (
            <Alert
              message={`此问题由事件 ${searchParams.get('fromIncidentId')} 触发`}
              type="info"
              showIcon
              className="mb-6"
            />
          )}

          <Form.Item
            label="问题标题"
            name="title"
            rules={[
              { required: true, message: '请输入问题标题' },
              { min: 2, max: 200, message: '标题长度应在2-200字符之间' },
            ]}
          >
            <Input placeholder="简要描述问题内容" />
          </Form.Item>

          <Form.Item
            label="详细描述"
            name="description"
            rules={[
              {
                required: true,
                validator: (_rule, value) => {
                  const richValue = typeof value === 'string' ? value : '';
                  const filled = richTextEnabled
                    ? !isRichTextEmpty(richValue)
                    : String(value || '').trim().length > 0;
                  if (!filled) {
                    return Promise.reject(new Error('请输入问题详细描述'));
                  }
                  // 长度按纯文本口径（不含标签/图片属性），上限与后端对齐 20000。
                  const plainLength = richTextEnabled
                    ? htmlToPlainText(richValue, Number.MAX_SAFE_INTEGER).length
                    : String(value || '').trim().length;
                  if (plainLength < 10) {
                    return Promise.reject(new Error('描述长度应不少于10个字符'));
                  }
                  if (plainLength > 20000) {
                    return Promise.reject(new Error('问题描述最多 20000 字'));
                  }
                  return Promise.resolve();
                },
              },
            ]}
            extra={
              richTextEnabled
                ? '支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（创建后自动上传）。'
                : undefined
            }
          >
            {richTextEnabled ? (
              <RichTextEditor
                placeholder="请提供问题的详细信息，包括影响范围、发生时间、已观察到的现象等..."
                minHeight={140}
                onUploadImage={handleEditorImageUpload}
                dataTestId="problem-description-input"
              />
            ) : (
              <TextArea
                rows={6}
                maxLength={20000}
                placeholder="请提供问题的详细信息，包括影响范围、发生时间、已观察到的现象等..."
                data-testid="problem-description-input"
              />
            )}
          </Form.Item>

          <Form.Item
            label="优先级"
            name="priority"
            rules={[{ required: true, message: '请选择优先级' }]}
          >
            <Select placeholder="选择优先级" options={priorityOptions} />
          </Form.Item>

          <Form.Item
            label="分类"
            name="category"
            rules={[{ required: true, message: '请选择分类' }]}
          >
            <Select placeholder="选择分类" options={ProblemCategoryOptions} />
          </Form.Item>

          <Form.Item
            label="根本原因分析 (RCA)"
            name="rootCause"
            rules={[
              { required: true, message: '请输入根本原因分析' },
              { min: 10, max: 5000, message: '内容长度应在10-5000字符之间' },
            ]}
          >
            <TextArea rows={4} placeholder="请详细说明问题的根本原因..." />
          </Form.Item>

          <Form.Item
            label="影响范围"
            name="impact"
            rules={[
              { required: true, message: '请输入影响范围' },
              { min: 10, max: 5000, message: '内容长度应在10-5000字符之间' },
            ]}
          >
            <TextArea rows={3} placeholder="请描述问题的影响范围，如影响哪些用户、服务或业务..." />
          </Form.Item>

          <Form.Item className="mb-0">
            <div className="flex justify-end space-x-4">
              <Button onClick={() => navigate(-1)}>取消</Button>
              <Button type="primary" htmlType="submit" loading={loading}>
                创建问题
              </Button>
            </div>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
};

const CreateProblemPage = () => {
  return (
    <Suspense
      fallback={
        <div className="flex items-center justify-center min-h-screen">
          <Spin size="large" />
        </div>
      }
    >
      <CreateProblemPageContent />
    </Suspense>
  );
};

export default CreateProblemPage;
