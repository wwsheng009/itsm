import { useNavigate, useParams } from 'react-router';

/**
 * 服务目录申请页面
 * Bug 2 修复：原本 /service-catalog/request/[id] 路由 404
 * B10 修复：表单加上 compliance_ack / expire_at / delivery_time
 */

import React, { useState, useEffect, useCallback, useRef, lazy, Suspense } from 'react';
import {
  Card,
  Form,
  Input,
  Button,
  Space,
  message,
  Typography,
  Breadcrumb,
  Checkbox,
  DatePicker,
  Select,
  Spin,
  Alert,
  Tag,
  Divider,
} from 'antd';
import { ArrowLeft, Clock, Send } from 'lucide-react';
import type { Dayjs } from 'dayjs';
import dayjs from 'dayjs';
import { ServiceCatalogApi } from '@/lib/api/service-catalog-api';
import { httpClient } from '@/lib/api/http-client';
import { useAuthStore } from '@/lib/store/auth-store';
import { htmlToPlainText, isRichTextEmpty, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';
import { AttachmentApi, serviceRequestAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { Title, Text, Paragraph } = Typography;
const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，
// 也就不会请求编辑器 chunk（对齐工单创建页 §4.3 / §NF-2）。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense
    fallback={
      <div className="rich-text-editor" style={{ minHeight: 140 }} />
    }
  >
    <RichTextEditorLazy {...props} />
  </Suspense>
);

export default function ServiceCatalogRequestPage() {
  const params = useParams();
  const navigate = useNavigate();
  const id = Number(params?.id);
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [catalog, setCatalog] = useState<any>(null);
  const [fetching, setFetching] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const user = useAuthStore(state => state.user);
  const richTextEnabled = isRichTextEnabled();

  // 富文本图片「先占位、后上传」：服务请求创建成功前拿不到 requestId，
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
    if (!id) {
      setFetching(false);
      setFetchError('服务标识无效，请返回服务目录重新选择');
      return;
    }
    setFetching(true);
    setFetchError(null);
    // 拉取服务目录详情
    httpClient
      .get<any>(`/api/v1/service-catalogs/${id}`)
      .then((data: any) => {
        setCatalog(data?.data || data);
      })
      .catch(() => {
        // 兜底：列表接口
        return httpClient.get<any>('/api/v1/service-catalogs', { page: 1, size: 100 }).then((list: any) => {
          const items = list?.data?.items || list?.items || [];
          const found = items.find((it: any) => it.id === id);
          if (found) {
            setCatalog(found);
          } else {
            setFetchError('未找到所选服务，该服务可能已下架');
          }
        });
      })
      .catch(() => setFetchError('服务信息加载失败，请稍后重试'))
      .finally(() => setFetching(false));
  }, [id]);

  useEffect(() => {
    if (user) {
      form.setFieldsValue({ requesterName: user.name, requesterEmail: user.email });
    }
  }, [form, user]);

  const onFinish = async (values: any) => {
    setLoading(true);
    const uploadKey = 'service-request-inline-image-upload';
    try {
      // 富文本路径：reason 落库 HTML（单字段范式），长度按净化后纯文本口径校验。
      const rawReason = typeof values.reason === 'string' ? values.reason : '';
      const plainReason = richTextEnabled
        ? htmlToPlainText(rawReason, 20000)
        : String(values.reason || '').trim();

      if (plainReason.length < 5) {
        message.warning('申请理由至少 5 个字，请说明业务场景');
        return;
      }

      // 上限按纯文本口径（后端 reason 为 HTML，按 20000 字符兜底）
      if (plainReason.length > 4000) {
        message.warning('申请理由最多 4000 字，请精简后提交');
        return;
      }

      // 编辑器内粘贴/拖拽的图片此刻只有 blob: 占位，不能落库：
      // 先剔除占位，待创建拿到 requestId 后上传并用正式地址回写。
      const stagedImageIds = richTextEnabled ? extractStagedImageIds(rawReason) : [];
      const reasonForCreate = stagedImageIds.length > 0 ? stripStagedImages(rawReason) : rawReason;

      const expireAt: Dayjs | undefined = values.expireAt;
      const payload: any = {
        serviceId: id,
        formData: {
          requesterName: values.requesterName,
          requesterEmail: values.requesterEmail,
          title: values.title,
          reason: reasonForCreate,
          quantity: values.quantity || 1,
          expectedAt: values.expectedAt ? values.expectedAt.toISOString() : undefined,
          costCenter: values.costCenter,
          dataClassification: values.dataClassification || 'internal',
          needsPublicIp: values.needsPublicIp || false,
          sourceIpWhitelist: values.sourceIpWhitelist
            ? values.sourceIpWhitelist.split(',').map((s: string) => s.trim()).filter(Boolean)
            : undefined,
          // B10: 合规确认 + 过期时间
          complianceAck: !!values.complianceAck,
          expireAt: expireAt ? expireAt.toISOString() : undefined,
        },
      };

      const created: any = await ServiceCatalogApi.createServiceRequest(payload);
      const createdId = Number(created?.id ?? created?.data?.id ?? 0);

      // 正文图片两段式：上传 → 用正式地址替换暂存占位 → 回写 reason。
      // 单项失败不阻断申请提交（未被替换的占位图会被丢弃，不会把 blob: 写进库）。
      if (stagedImageIds.length > 0 && createdId > 0) {
        message.open({ key: uploadKey, type: 'loading', content: '正在上传正文图片…', duration: 0 });
        const replacements: Record<string, StagedImageReplacement> = {};
        let imageFailures = 0;

        for (const stagedId of stagedImageIds) {
          const file = stagedImagesRef.current.get(stagedId);
          if (!file) continue;
          try {
            const uploaded = await AttachmentApi.upload(file, {
              bizType: 'service_request',
              bizId: createdId,
              usage: 'inline_image',
            });
            replacements[stagedId] = {
              id: uploaded.id,
              url:
                uploaded.previewUrl ||
                serviceRequestAttachmentPreviewUrl(createdId, uploaded.id),
              name: uploaded.fileName || file.name,
            };
            stagedImagesRef.current.delete(stagedId);
          } catch {
            imageFailures += 1;
          }
        }

        try {
          await ServiceCatalogApi.updateServiceRequest(createdId, {
            reason: replaceStagedImages(rawReason, replacements),
          });
        } catch (e) {
          console.error('回写服务请求正文图片失败', e);
          imageFailures += 1;
        }
        message.destroy(uploadKey);
        if (imageFailures > 0) {
          message.warning(`${imageFailures} 张正文图片上传失败，可在详情页编辑补充。`);
        }
      }

      message.success('申请已提交，等待审批');
      navigate('/my-requests');
    } catch (e: any) {
      message.destroy(uploadKey);
      message.error('提交失败：' + (e?.message || '未知错误'));
    } finally {
      setLoading(false);
    }
  };

  if (fetching) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div className="max-w-3xl mx-auto p-6">
      <Breadcrumb
        items={[
          { title: '服务目录', href: '/service-catalog' },
          { title: '提交申请' },
        ]}
        className="mb-4"
      />
      <Card>
        <Space className="mb-4">
          <Button icon={<ArrowLeft />} onClick={() => navigate('/service-catalog')}>
            返回
          </Button>
          <Title level={3} style={{ margin: 0 }}>
            申请服务
          </Title>
        </Space>

        {fetchError && (
          <Alert
            type="error"
            showIcon
            className="mb-4"
            message={fetchError}
            action={<Button onClick={() => navigate('/service-catalog')}>返回服务目录</Button>}
          />
        )}

        {catalog && (
          <Alert
            type="info"
            showIcon
            className="mb-4"
            message={
              <div className="flex items-center justify-between gap-3 flex-wrap">
                <Text strong className="!text-base">
                  {catalog.name}
                </Text>
                <Space size={4} wrap>
                  {catalog.deliveryTime != null && catalog.deliveryTime > 0 && (
                    <Tag icon={<Clock />} color="blue">
                      交付时长 {catalog.deliveryTime} 天
                    </Tag>
                  )}
                  {catalog.category && <Tag>{catalog.category}</Tag>}
                </Space>
              </div>
            }
            description={
              catalog.description ? (
                <div className="text-gray-600 leading-relaxed">{catalog.description}</div>
              ) : null
            }
          />
        )}

        <Divider />

        <Form form={form} layout="vertical" onFinish={onFinish}>
          <div className="grid grid-cols-2 gap-4">
            <Form.Item name="requesterName" label="申请人">
              <Input disabled placeholder="当前登录用户" />
            </Form.Item>
            <Form.Item name="requesterEmail" label="联系邮箱">
              <Input disabled placeholder="当前用户邮箱" />
            </Form.Item>
          </div>
          <Form.Item
            name="title"
            label="申请标题"
            rules={[{ required: true, message: '请输入申请标题' }]}
          >
            <Input placeholder="一句话说明申请目的" maxLength={200} />
          </Form.Item>

          <Form.Item
            name="reason"
            label="申请理由"
            rules={[
              {
                required: true,
                validator: (_rule, value) => {
                  const filled = richTextEnabled
                    ? !isRichTextEmpty(typeof value === 'string' ? value : '')
                    : String(value || '').trim().length > 0;
                  return filled ? Promise.resolve() : Promise.reject(new Error('请输入申请理由'));
                },
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
                placeholder="请详细说明申请原因、业务场景、紧急程度"
                minHeight={140}
                onUploadImage={handleEditorImageUpload}
                dataTestId="service-request-reason-input"
              />
            ) : (
              <TextArea rows={4} placeholder="请详细说明申请原因、业务场景、紧急程度" maxLength={2000} />
            )}
          </Form.Item>

          <div className="grid grid-cols-2 gap-4">
            <Form.Item name="quantity" label="数量" initialValue={1}>
              <Input type="number" min={1} max={100} />
            </Form.Item>
            <Form.Item name="expectedAt" label="期望交付时间">
              <DatePicker showTime style={{ width: '100%' }} />
            </Form.Item>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <Form.Item name="costCenter" label="成本中心">
              <Input placeholder="例如 CC-1001" />
            </Form.Item>
            <Form.Item
              name="dataClassification"
              label="数据分级"
              initialValue="internal"
            >
              <Select
                options={[
                  { label: '公开 (public)', value: 'public' },
                  { label: '内部 (internal)', value: 'internal' },
                  { label: '机密 (confidential)', value: 'confidential' },
                  { label: '绝密 (restricted)', value: 'restricted' },
                ]}
              />
            </Form.Item>
          </div>

          <Form.Item name="needsPublicIp" valuePropName="checked">
            <Checkbox>需要公网 IP</Checkbox>
          </Form.Item>

          <Form.Item
            name="sourceIpWhitelist"
            label="来源 IP 白名单（多个以英文逗号分隔）"
            dependencies={['needsPublicIp']}
          >
            <Input placeholder="例如 1.2.3.4, 10.0.0.0/8" />
          </Form.Item>

          <Divider />

          <Form.Item
            name="expireAt"
            label="资源过期时间（到期自动回收）"
            extra="若不填写，则按服务目录默认策略"
          >
            <DatePicker
              showTime
              style={{ width: '100%' }}
              disabledDate={(d) => d && d.isBefore(dayjs().startOf('day'))}
            />
          </Form.Item>

          <Form.Item
            name="complianceAck"
            valuePropName="checked"
            rules={[
              {
                validator: (_, value) =>
                  value
                    ? Promise.resolve()
                    : Promise.reject(new Error('请确认已知悉相关合规与安全要求')),
              },
            ]}
          >
            <Checkbox>
              我已知悉本服务的合规要求与安全策略，并承诺仅将资源用于申请所述的合法业务场景
            </Checkbox>
          </Form.Item>

          <Form.Item>
            <Space>
              <Button
                type="primary"
                htmlType="submit"
                icon={<Send />}
                loading={loading}
                disabled={!catalog || !!fetchError}
              >
                提交申请
              </Button>
              <Button onClick={() => navigate('/service-catalog')}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
