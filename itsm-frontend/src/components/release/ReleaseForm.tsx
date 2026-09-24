import { useNavigate, useParams } from 'react-router';

/**
 * 发布创建/编辑表单组件
 */

import React, { useState, useEffect, useCallback, useRef, lazy, Suspense } from 'react';
import {
  Alert,
  Card,
  Form,
  Input,
  Select,
  DatePicker,
  Button,
  Space,
  Switch,
  Divider,
  message,
  InputNumber,
} from 'antd';
import dayjs from 'dayjs';
import { ArrowLeft, Lock, Save } from 'lucide-react';

import type { Release, ReleaseRequest } from '@/lib/api/release-api';
import { ReleaseApi } from '@/lib/api/release-api';
import type { Dayjs } from 'dayjs';
import { htmlToPlainText, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import { useInlineImageUnbind } from '@/lib/rich-text/useInlineImageUnbind';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';
import { AttachmentApi, releaseAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，也就不会请求编辑器 chunk。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<div className="rich-text-editor" style={{ minHeight: 140 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

interface ReleaseFormValues {
  releaseNumber: string;
  title: string;
  description?: string;
  type?: Release['type'];
  environment?: Release['environment'];
  severity?: Release['severity'];
  changeId?: number;
  ownerId?: number;
  plannedReleaseDate?: Dayjs;
  plannedStartDate?: Dayjs;
  plannedEndDate?: Dayjs;
  releaseNotes?: string;
  rollbackProcedure?: string;
  validationCriteria?: string;
  affectedSystems?: string;
  affectedComponents?: string;
  deploymentSteps?: string;
  tags?: string[];
  isEmergency?: boolean;
  requiresApproval?: boolean;
}

/** 第三波接入富文本的四个字段（单字段 HTML 范式；长度按净化后纯文本口径校验） */
const RICH_FIELDS = ['description', 'releaseNotes', 'rollbackProcedure', 'validationCriteria'] as const;
const RICH_TEXT_MAX_LENGTH = 20000;

const splitLines = (value?: string): string[] | undefined => {
  const items = value
    ?.split(/\r?\n/)
    .map(item => item.trim())
    .filter(Boolean);
  return items?.length ? items : undefined;
};

// 不允许编辑的状态列表（已发布/已部署/已完成）
const READONLY_STATUSES = ['released', 'deployed', 'completed', 'cancelled'];

const ReleaseForm: React.FC = () => {
  const navigate = useNavigate();
  const { id } = useParams() as { id: string };
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [detail, setDetail] = useState<Release | null>(null);
  const [isReadonly, setIsReadonly] = useState(false);
  const isEdit = !!id;

  const richTextEnabled = isRichTextEnabled();
  const releaseId = isEdit ? Number(id) : 0;
  const uploadKey = 'release-content-images';
  // 编辑态正文内嵌图片解绑：加载时记基线，保存成功后 diff 出被移除的图片（失败仅告警）。
  // 创建态图片先以 blob: 占位（见下方 stagedImagesRef），不适用解绑。
  const { captureInlineImageBaseline, unbindRemovedInlineImages } = useInlineImageUnbind();

  // 正文图片：编辑态即时上传；创建态先以 blob: 占位，创建成功后统一上传并回写（两段式）。
  const stagedImagesRef = useRef<Map<string, File>>(new Map());
  const stagedImageSeqRef = useRef(0);

  const handleEditorImageUpload = useCallback(
    async (file: File): Promise<UploadedImage> => {
      if (releaseId > 0) {
        const uploaded = await AttachmentApi.upload(file, {
          bizType: 'release',
          bizId: releaseId,
          usage: 'inline_image',
        });
        return {
          url: uploaded.previewUrl || releaseAttachmentPreviewUrl(releaseId, uploaded.id),
          id: uploaded.id,
          name: uploaded.fileName || file.name,
        };
      }
      stagedImageSeqRef.current += 1;
      const stagedId = `${STAGED_ID_PREFIX}${Date.now().toString(36)}-${stagedImageSeqRef.current}`;
      stagedImagesRef.current.set(stagedId, file);
      return { url: URL.createObjectURL(file), id: stagedId, name: file.name };
    },
    [releaseId],
  );

  useEffect(() => {
    if (id) {
      loadDetail();
    }
  }, [id]);

  const loadDetail = async () => {
    setLoading(true);
    try {
      const data = await ReleaseApi.getRelease(Number(id));
      setDetail(data);

      // 状态守卫：已发布/已部署/已完成的发布不允许编辑
      if (READONLY_STATUSES.includes(data.status)) {
        setIsReadonly(true);
        message.warning('该发布已进入不可编辑状态，仅可查看');
        return;
      }

      // 设置表单值
      form.setFieldsValue({
        ...data,
        plannedReleaseDate: data.plannedReleaseDate
          ? dayjs(data.plannedReleaseDate)
          : undefined,
        plannedStartDate: data.plannedStartDate ? dayjs(data.plannedStartDate) : undefined,
        plannedEndDate: data.plannedEndDate ? dayjs(data.plannedEndDate) : undefined,
        deploymentSteps: data.deploymentSteps?.join('\n'),
        affectedSystems: data.affectedSystems?.join('\n'),
        affectedComponents: data.affectedComponents?.join('\n'),
      });
      captureInlineImageBaseline(RICH_FIELDS.map(key => data[key] ?? ''));
    } catch (error) {
      message.error('加载发布详情失败');
    } finally {
      setLoading(false);
    }
  };

  const onFinish = async (values: ReleaseFormValues) => {
    setLoading(true);
    try {
      // 富文本路径：长度按净化后纯文本口径校验（与后端 DTO 的 max=20000 对齐）。
      if (richTextEnabled) {
        for (const key of RICH_FIELDS) {
          if (
            htmlToPlainText(values[key] ?? '', Number.MAX_SAFE_INTEGER).length >
            RICH_TEXT_MAX_LENGTH
          ) {
            message.warning('正文最多 20000 字，请精简后再提交');
            return;
          }
        }
      }

      const data: ReleaseRequest = {
        releaseNumber: values.releaseNumber,
        title: values.title,
        description: values.description,
        type: values.type,
        environment: values.environment,
        severity: values.severity,
        changeId: values.changeId,
        ownerId: values.ownerId,
        plannedReleaseDate: values.plannedReleaseDate?.toISOString(),
        plannedStartDate: values.plannedStartDate?.toISOString(),
        plannedEndDate: values.plannedEndDate?.toISOString(),
        releaseNotes: values.releaseNotes,
        rollbackProcedure: values.rollbackProcedure,
        validationCriteria: values.validationCriteria,
        affectedSystems: splitLines(values.affectedSystems),
        affectedComponents: splitLines(values.affectedComponents),
        deploymentSteps: splitLines(values.deploymentSteps),
        tags: values.tags,
        isEmergency: values.isEmergency,
        requiresApproval: values.requiresApproval,
      };

      // 创建态：编辑器内的图片此刻只有 blob: 占位，先剔除占位，拿到 releaseId 后再上传回写。
      const stagedEntries = richTextEnabled
        ? RICH_FIELDS.flatMap(key =>
            extractStagedImageIds(values[key] ?? '').map(stagedId => ({ key, stagedId })),
          )
        : [];

      if (isEdit) {
        await ReleaseApi.updateRelease(releaseId, data);

        // 编辑器内被删除的图片：调用附件解绑接口（域内别名路由沿用 release 资源码，
        // 幂等、失败不阻断保存结果）。
        if (richTextEnabled) {
          await unbindRemovedInlineImages(
            { bizType: 'release', bizId: releaseId },
            RICH_FIELDS.map(key => values[key] ?? '')
          );
        }

        message.success('更新成功');
        navigate('/releases');
        return;
      }

      const createPayload: ReleaseRequest =
        stagedEntries.length > 0
          ? {
              ...data,
              description: stripStagedImages(values.description ?? ''),
              releaseNotes: stripStagedImages(values.releaseNotes ?? ''),
              rollbackProcedure: stripStagedImages(values.rollbackProcedure ?? ''),
              validationCriteria: stripStagedImages(values.validationCriteria ?? ''),
            }
          : data;

      const created = await ReleaseApi.createRelease(createPayload);
      const createdId = Number(created?.id ?? 0);

      // 正文图片两段式：上传 → 用正式地址替换暂存占位 → 回写四个字段。
      // 单项失败不阻断发布创建（未被替换的占位图会被丢弃，不会把 blob: 写进库）。
      if (stagedEntries.length > 0 && createdId > 0) {
        message.open({ key: uploadKey, type: 'loading', content: '正在上传正文图片…', duration: 0 });
        const replacements: Record<string, StagedImageReplacement> = {};
        let imageFailures = 0;

        for (const { stagedId } of stagedEntries) {
          const file = stagedImagesRef.current.get(stagedId);
          if (!file) continue;
          try {
            const uploaded = await AttachmentApi.upload(file, {
              bizType: 'release',
              bizId: createdId,
              usage: 'inline_image',
            });
            replacements[stagedId] = {
              id: uploaded.id,
              url: uploaded.previewUrl || releaseAttachmentPreviewUrl(createdId, uploaded.id),
              name: uploaded.fileName || file.name,
            };
            stagedImagesRef.current.delete(stagedId);
          } catch {
            imageFailures += 1;
          }
        }

        try {
          await ReleaseApi.updateRelease(createdId, {
            description: replaceStagedImages(values.description ?? '', replacements),
            releaseNotes: replaceStagedImages(values.releaseNotes ?? '', replacements),
            rollbackProcedure: replaceStagedImages(values.rollbackProcedure ?? '', replacements),
            validationCriteria: replaceStagedImages(values.validationCriteria ?? '', replacements),
          });
        } catch (e) {
          console.error('回写发布正文图片失败', e);
          imageFailures += 1;
        }
        message.destroy(uploadKey);
        if (imageFailures > 0) {
          message.warning(`${imageFailures} 张正文图片上传失败，可在编辑页补充。`);
        }
      }

      message.success('创建成功');
      navigate('/releases');
    } catch (error) {
      message.destroy(uploadKey);
      message.error(isEdit ? '更新失败' : '创建失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Card>
      {/* 状态守卫提示 */}
      {isReadonly && (
        <Alert
          type="warning"
          showIcon
          icon={<Lock />}
          message="该发布已进入不可编辑状态"
          description="已发布、已部署或已完成的发布不允许修改。"
          className="mb-4"
          action={
            <Button size="small" onClick={() => navigate(`/releases/${id}`)}>
              返回详情
            </Button>
          }
        />
      )}

      <Form
        form={form}
        data-testid="release-form"
        layout="vertical"
        onFinish={onFinish}
        disabled={isReadonly}
        initialValues={{
          type: 'minor',
          environment: 'staging',
          severity: 'medium',
          isEmergency: false,
          requiresApproval: true,
        }}
      >
        <div style={{ marginBottom: 16 }}>
          <Button icon={<ArrowLeft />} onClick={() => navigate('/releases')}>
            返回列表
          </Button>
        </div>

        <Divider>基本信息</Divider>

        <Form.Item
          name="releaseNumber"
          label="发布编号"
          rules={[{ required: true, message: '请输入发布编号' }]}
        >
          <Input
            placeholder="例如: REL-20260222-001"
            data-testid="release-number-input"
          />
        </Form.Item>

        <Form.Item
          name="title"
          label="标题"
          rules={[{ required: true, message: '请输入发布标题' }]}
        >
          <Input placeholder="发布标题" data-testid="release-title-input" />
        </Form.Item>

        <Form.Item
          name="description"
          label="描述"
          extra={
            richTextEnabled
              ? '支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（创建后自动上传）。'
              : undefined
          }
        >
          {richTextEnabled ? (
            <RichTextEditor
              placeholder="发布描述"
              minHeight={120}
              onUploadImage={handleEditorImageUpload}
              dataTestId="release-description-input"
            />
          ) : (
            <TextArea rows={3} placeholder="发布描述" data-testid="release-description-input" />
          )}
        </Form.Item>

        <Form.Item name="type" label="发布类型">
          <Select options={[
            { value: 'major', label: '主版本 (Major)' },
            { value: 'minor', label: '次版本 (Minor)' },
            { value: 'patch', label: '补丁 (Patch)' },
            { value: 'hotfix', label: '紧急修复 (Hotfix)' },
          ]} />
        </Form.Item>

        <Form.Item name="environment" label="目标环境">
          <Select options={[
            { value: 'dev', label: '开发环境' },
            { value: 'staging', label: '预发布环境' },
            { value: 'production', label: '生产环境' },
          ]} />
        </Form.Item>

        <Form.Item name="severity" label="严重程度">
          <Select options={[
            { value: 'low', label: '低' },
            { value: 'medium', label: '中' },
            { value: 'high', label: '高' },
            { value: 'critical', label: '严重' },
          ]} />
        </Form.Item>

        <Divider>计划信息</Divider>

        <Form.Item name="plannedReleaseDate" label="计划发布日期">
          <DatePicker showTime style={{ width: '100%' }} />
        </Form.Item>

        <Form.Item name="plannedStartDate" label="计划开始时间">
          <DatePicker showTime style={{ width: '100%' }} />
        </Form.Item>

        <Form.Item name="plannedEndDate" label="计划结束时间">
          <DatePicker showTime style={{ width: '100%' }} />
        </Form.Item>

        <Divider>发布内容</Divider>

        <Form.Item
          name="releaseNotes"
          label="发布说明"
          extra={
            richTextEnabled
              ? '支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（创建后自动上传）。'
              : undefined
          }
        >
          {richTextEnabled ? (
            <RichTextEditor
              placeholder="发布说明内容"
              minHeight={140}
              onUploadImage={handleEditorImageUpload}
              dataTestId="release-notes-input"
            />
          ) : (
            <TextArea rows={4} placeholder="发布说明内容" data-testid="release-notes-input" />
          )}
        </Form.Item>

        <Form.Item name="deploymentSteps" label="部署步骤">
          <TextArea rows={4} placeholder="每行一个步骤" />
        </Form.Item>

        <Form.Item name="affectedSystems" label="受影响的系统">
          <TextArea rows={2} placeholder="每行一个系统" />
        </Form.Item>

        <Form.Item name="affectedComponents" label="受影响的组件">
          <TextArea rows={2} placeholder="每行一个组件" />
        </Form.Item>

        <Divider>回滚与验证</Divider>

        <Form.Item
          name="rollbackProcedure"
          label="回滚程序"
          extra={
            richTextEnabled
              ? '支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（创建后自动上传）。'
              : undefined
          }
        >
          {richTextEnabled ? (
            <RichTextEditor
              placeholder="回滚步骤说明"
              minHeight={140}
              onUploadImage={handleEditorImageUpload}
              dataTestId="release-rollback-procedure-input"
            />
          ) : (
            <TextArea rows={4} placeholder="回滚步骤说明" data-testid="release-rollback-procedure-input" />
          )}
        </Form.Item>

        <Form.Item
          name="validationCriteria"
          label="验证标准"
          extra={
            richTextEnabled
              ? '支持加粗、列表、代码块等排版；可直接粘贴或拖拽图片（创建后自动上传）。'
              : undefined
          }
        >
          {richTextEnabled ? (
            <RichTextEditor
              placeholder="验证通过的标准"
              minHeight={120}
              onUploadImage={handleEditorImageUpload}
              dataTestId="release-validation-criteria-input"
            />
          ) : (
            <TextArea rows={3} placeholder="验证通过的标准" data-testid="release-validation-criteria-input" />
          )}
        </Form.Item>

        <Divider>其他选项</Divider>

        <Form.Item name="isEmergency" label="紧急发布" valuePropName="checked">
          <Switch />
        </Form.Item>

        <Form.Item name="requiresApproval" label="需要审批" valuePropName="checked">
          <Switch />
        </Form.Item>

        <Form.Item>
          <Space>
            <Button
              type="primary"
              htmlType="submit"
              icon={<Save />}
              loading={loading}
              data-testid="release-submit-button"
            >
              {isEdit ? '保存' : '创建'}
            </Button>
            <Button onClick={() => navigate('/releases')}>取消</Button>
          </Space>
        </Form.Item>
      </Form>
    </Card>
  );
};

export default ReleaseForm;
