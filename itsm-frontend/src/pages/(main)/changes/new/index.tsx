import { useNavigate } from 'react-router';

import React, { lazy, Suspense, useCallback, useRef, useState } from 'react';
import {
  App,
  Button,
  Card,
  Col,
  DatePicker,
  Form,
  Input,
  Row,
  Select,
  Space,
  Typography,
} from 'antd';
import { ArrowLeft } from 'lucide-react';
import dayjs, { type Dayjs } from 'dayjs';
import { ChangeApi, type ChangeRequest } from '@/lib/api/change-api';
import { AttachmentApi, changeAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import { htmlToPlainText, isRichTextEmpty, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';
import { useI18n } from '@/lib/i18n';

const { Title, Text } = Typography;
const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，
// 也就不会请求编辑器 chunk（对齐服务请求 reason 范式）。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<div className="rich-text-editor" style={{ minHeight: 140 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

/** 富文本三字段纯文本长度上限（后端本轮放宽至 20000，标签不占额度） */
const RICH_TEXT_MAX_LENGTH = 20000;

/** 富文本字段名（两段式图片回写按字段分组） */
const RICH_TEXT_FIELD_NAMES = ['description', 'implementationPlan', 'rollbackPlan'] as const;
type RichTextFieldName = (typeof RICH_TEXT_FIELD_NAMES)[number];

/**
 * 必填 + 纯文本长度校验：HTML 只计纯文本（htmlToPlainText，不截断），标签不占额度；
 * 开关关闭时沿用原 TextArea 语义。提示文案与改造前保持一致。
 */
const validateRichTextValue = (value: unknown, requiredMessage: string, label: string) => {
  const raw = typeof value === 'string' ? value : '';
  const filled = isRichTextEnabled() ? !isRichTextEmpty(raw) : String(raw).trim().length > 0;
  if (!filled) return Promise.reject(new Error(requiredMessage));
  const plainLength = isRichTextEnabled()
    ? htmlToPlainText(raw, Number.MAX_SAFE_INTEGER).length
    : String(raw).trim().length;
  if (plainLength > RICH_TEXT_MAX_LENGTH) {
    return Promise.reject(new Error(`${label}最多 ${RICH_TEXT_MAX_LENGTH} 字，请精简后提交`));
  }
  return Promise.resolve();
};

// 表单值类型：DatePicker 用 Dayjs，affectedCis 用字符串
interface ChangeFormValues {
  title: string;
  description: string;
  justification: string;
  type: ChangeRequest['type'];
  priority: ChangeRequest['priority'];
  impactScope: ChangeRequest['impactScope'];
  riskLevel: ChangeRequest['riskLevel'];
  plannedRange?: [Dayjs, Dayjs];
  affectedCisText?: string;
  implementationPlan: string;
  rollbackPlan: string;
}

const TYPE_OPTIONS: Array<{ value: ChangeRequest['type']; label: string; hint?: string }> = [
  { value: 'normal', label: '普通变更', hint: '标准审批流程' },
  { value: 'standard', label: '标准变更', hint: '预授权、低风险' },
  { value: 'emergency', label: '紧急变更', hint: '走应急审批' },
];

const PRIORITY_OPTIONS: Array<{ value: ChangeRequest['priority']; label: string }> = [
  { value: 'critical', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
];

const IMPACT_OPTIONS: Array<{ value: ChangeRequest['impactScope']; label: string }> = [
  { value: 'high', label: '高（影响核心业务）' },
  { value: 'medium', label: '中（影响部分业务或用户）' },
  { value: 'low', label: '低（影响较小或无影响）' },
];

const RISK_OPTIONS: Array<{ value: ChangeRequest['riskLevel']; label: string }> = [
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
];

const CreateChangePage: React.FC = () => {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { message } = App.useApp();
  const [form] = Form.useForm<ChangeFormValues>();
  const [loading, setLoading] = useState(false);
  const richTextEnabled = isRichTextEnabled();

  // 富文本图片「先占位、后上传」：变更创建成功前拿不到 changeId，
  // 故先在编辑器内以 blob: 占位，提交后统一上传并把正式 HTML 回写字段。
  const stagedImagesRef = useRef<Map<string, File>>(new Map());
  const stagedImageSeqRef = useRef(0);

  const handleEditorImageUpload = useCallback(async (file: File): Promise<UploadedImage> => {
    stagedImageSeqRef.current += 1;
    const id = `${STAGED_ID_PREFIX}${Date.now().toString(36)}-${stagedImageSeqRef.current}`;
    stagedImagesRef.current.set(id, file);
    return { url: URL.createObjectURL(file), id, name: file.name };
  }, []);

  const handleSubmit = async (values: ChangeFormValues) => {
    setLoading(true);
    const uploadKey = 'change-inline-image-upload';
    try {
      const [start, end] = values.plannedRange ?? [];
      const affectedCis =
        values.affectedCisText
          ?.split(/[,，\s]+/)
          .map(s => s.trim())
          .filter(Boolean) ?? [];

      // 三个富文本字段落库 HTML，长度已按净化后纯文本口径校验（见 validateRichTextValue）；
      // 编辑器内的图片此时只有 blob: 占位，不能落库：先剔除，待创建拿到 changeId 后上传回写。
      const rawValues: Record<RichTextFieldName, string> = {
        description: values.description || '',
        implementationPlan: values.implementationPlan || '',
        rollbackPlan: values.rollbackPlan || '',
      };
      const stagedIdsByField = {} as Record<RichTextFieldName, string[]>;
      for (const field of RICH_TEXT_FIELD_NAMES) {
        stagedIdsByField[field] = richTextEnabled ? extractStagedImageIds(rawValues[field]) : [];
      }
      const hasStagedImages = RICH_TEXT_FIELD_NAMES.some(
        field => stagedIdsByField[field].length > 0
      );
      const htmlForCreate = (field: RichTextFieldName) =>
        stagedIdsByField[field].length > 0 ? stripStagedImages(rawValues[field]) : rawValues[field];

      const payload: ChangeRequest = {
        title: values.title.trim(),
        description: htmlForCreate('description').trim(),
        justification: values.justification.trim(),
        type: values.type,
        priority: values.priority,
        impactScope: values.impactScope,
        riskLevel: values.riskLevel,
        plannedStartDate: start ? start.toISOString() : undefined,
        plannedEndDate: end ? end.toISOString() : undefined,
        implementationPlan: htmlForCreate('implementationPlan').trim(),
        rollbackPlan: htmlForCreate('rollbackPlan').trim(),
        affectedCis,
        relatedTickets: [],
      };

      const created: any = await ChangeApi.createChange(payload);
      const createdId = Number(created?.id ?? created?.data?.id ?? 0);

      // 正文图片两段式：上传 → 用正式附件地址替换暂存占位 → 一次性回写涉及字段。
      // 单项失败不阻断创建结果（未被替换的占位图会被丢弃，不会把 blob: 写进库）。
      if (hasStagedImages && createdId > 0) {
        message.open({ key: uploadKey, type: 'loading', content: '正在上传正文图片…', duration: 0 });
        const patch: Partial<ChangeRequest> = {};
        let imageFailures = 0;

        for (const field of RICH_TEXT_FIELD_NAMES) {
          const stagedIds = stagedIdsByField[field];
          if (stagedIds.length === 0) continue;
          const replacements: Record<string, StagedImageReplacement> = {};
          for (const stagedId of stagedIds) {
            const file = stagedImagesRef.current.get(stagedId);
            if (!file) continue;
            try {
              const uploaded = await AttachmentApi.upload(file, {
                bizType: 'change',
                bizId: createdId,
                usage: 'inline_image',
              });
              replacements[stagedId] = {
                id: uploaded.id,
                url: uploaded.previewUrl || changeAttachmentPreviewUrl(createdId, uploaded.id),
                name: uploaded.fileName || file.name,
              };
              stagedImagesRef.current.delete(stagedId);
            } catch {
              imageFailures += 1;
            }
          }
          patch[field] = replaceStagedImages(rawValues[field], replacements);
        }

        try {
          await ChangeApi.updateChange(createdId, patch);
        } catch (e) {
          console.error('回写变更正文图片失败', e);
          imageFailures += 1;
        }
        message.destroy(uploadKey);
        if (imageFailures > 0) {
          message.warning(`${imageFailures} 张正文图片上传失败，可在详情页编辑补充。`);
        }
      }

      message.success(t('changes.createSuccess'));
      navigate('/changes');
    } catch (err) {
      message.destroy(uploadKey);
      console.error('提交变更失败:', err);
      message.error(t('changes.createFailed'));
    } finally {
      setLoading(false);
    }
  };

  const validateRange = (_: unknown, value?: [Dayjs, Dayjs]) => {
    if (!value || !value[0] || !value[1]) return Promise.resolve();
    if (value[1].isBefore(value[0])) {
      return Promise.reject(new Error('结束时间必须晚于开始时间'));
    }
    return Promise.resolve();
  };

  return (
    <div className="p-6 md:p-10 bg-gray-50 min-h-full">
      <div className="mb-6">
        <Button
          type="link"
          icon={<ArrowLeft size={16} />}
          onClick={() => navigate(-1)}
          className="!px-0"
        >
          返回变更列表
        </Button>
        <Title level={2} className="!mb-1 !mt-2">
          新建变更请求
        </Title>
        <Text type="secondary">提交新的 IT 基础设施或服务变更请求</Text>
      </div>

      <Card className="shadow-sm rounded-lg">
        <Form<ChangeFormValues>
          form={form}
          data-testid="change-create-form"
          layout="vertical"
          initialValues={{
            type: 'normal',
            priority: 'medium',
            impactScope: 'medium',
            riskLevel: 'medium',
          }}
          onFinish={handleSubmit}
          disabled={loading}
          scrollToFirstError
        >
          <Form.Item
            label="变更标题"
            name="title"
            rules={[
              { required: true, message: '请输入变更标题' },
              { max: 200, message: '标题不超过 200 字' },
            ]}
          >
            <Input
              placeholder="简要描述变更内容"
              allowClear
              data-testid="change-title-input"
            />
          </Form.Item>

          <Form.Item
            label="详细描述"
            name="description"
            rules={[
              { required: true, message: '请填写详细描述' },
              {
                validator: (_rule, value) =>
                  validateRichTextValue(value, '请填写详细描述', '详细描述'),
              },
            ]}
            extra={
              richTextEnabled
                ? '支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（提交后自动上传）。'
                : undefined
            }
          >
            {richTextEnabled ? (
              <RichTextEditor
                placeholder="请详细说明变更的目的、范围和内容..."
                minHeight={140}
                onUploadImage={handleEditorImageUpload}
                dataTestId="change-description-input"
              />
            ) : (
              <TextArea
                data-testid="change-description-input"
                rows={4}
                placeholder="请详细说明变更的目的、范围和内容..."
                showCount
                maxLength={2000}
              />
            )}
          </Form.Item>

          <Form.Item
            label="变更理由"
            name="justification"
            tooltip="解释为什么需要此变更，例如关联的问题或业务需求"
            rules={[{ required: true, message: '请填写变更理由' }]}
          >
            <TextArea
              data-testid="change-justification-input"
              rows={3}
              placeholder="例如：解决问题 PRB-XXXXX，满足新业务需求等"
              showCount
              maxLength={1000}
            />
          </Form.Item>

          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item
                label="变更类型"
                name="type"
                rules={[{ required: true, message: '请选择变更类型' }]}
              >
                <Select
                  options={TYPE_OPTIONS.map(o => ({
                    value: o.value,
                    label: (
                      <span>
                        {o.label}
                        {o.hint && (
                          <Text type="secondary" className="ml-2 text-xs">
                            {o.hint}
                          </Text>
                        )}
                      </span>
                    ),
                  }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item
                label="优先级"
                name="priority"
                rules={[{ required: true, message: '请选择优先级' }]}
              >
                <Select options={PRIORITY_OPTIONS} />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item
                label="影响范围"
                name="impactScope"
                rules={[{ required: true, message: '请选择影响范围' }]}
              >
                <Select options={IMPACT_OPTIONS} />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item
                label="风险等级"
                name="riskLevel"
                rules={[{ required: true, message: '请选择风险等级' }]}
              >
                <Select options={RISK_OPTIONS} />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item
            label="计划实施时间"
            name="plannedRange"
            tooltip="计划开始与结束时间"
            rules={[{ validator: validateRange }]}
          >
            <DatePicker.RangePicker
              showTime={{ format: 'HH:mm' }}
              format="YYYY-MM-DD HH:mm"
              disabledDate={current => !!current && current.isBefore(dayjs().startOf('day'))}
              placeholder={['开始时间', '结束时间']}
              className="w-full"
            />
          </Form.Item>

          <Form.Item
            label="受影响的配置项"
            name="affectedCisText"
            tooltip="多个 CI 用逗号或空格分隔"
          >
            <Input placeholder="例如：CI-ECS-001, CI-APP-CRM" allowClear />
          </Form.Item>

          <Form.Item
            label="实施计划"
            name="implementationPlan"
            rules={[
              { required: true, message: '请填写实施计划' },
              {
                validator: (_rule, value) =>
                  validateRichTextValue(value, '请填写实施计划', '实施计划'),
              },
            ]}
            extra={
              richTextEnabled
                ? '建议按步骤分条说明；可直接粘贴或拖拽图片（提交后自动上传）。'
                : undefined
            }
          >
            {richTextEnabled ? (
              <RichTextEditor
                placeholder="详细描述变更的实施步骤..."
                minHeight={160}
                onUploadImage={handleEditorImageUpload}
                dataTestId="change-implementation-input"
              />
            ) : (
              <TextArea
                data-testid="change-implementation-input"
                rows={5}
                placeholder="详细描述变更的实施步骤..."
                showCount
                maxLength={3000}
              />
            )}
          </Form.Item>

          <Form.Item
            label="回滚计划"
            name="rollbackPlan"
            tooltip="变更失败时如何回退"
            rules={[
              { required: true, message: '请填写回滚计划' },
              {
                validator: (_rule, value) =>
                  validateRichTextValue(value, '请填写回滚计划', '回滚计划'),
              },
            ]}
            extra={
              richTextEnabled
                ? '建议按步骤分条说明回退动作；可直接粘贴或拖拽图片（提交后自动上传）。'
                : undefined
            }
          >
            {richTextEnabled ? (
              <RichTextEditor
                placeholder="详细描述如果变更失败如何回滚..."
                minHeight={160}
                onUploadImage={handleEditorImageUpload}
                dataTestId="change-rollback-input"
              />
            ) : (
              <TextArea
                data-testid="change-rollback-input"
                rows={5}
                placeholder="详细描述如果变更失败如何回滚..."
                showCount
                maxLength={3000}
              />
            )}
          </Form.Item>

          <Form.Item className="!mb-0 mt-4">
            <Space className="w-full justify-end">
              <Button onClick={() => navigate(-1)} disabled={loading}>
                取消
              </Button>
              <Button
                type="primary"
                htmlType="submit"
                loading={loading}
                data-testid="change-submit-button"
              >
                提交变更请求
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
};

export default CreateChangePage;
