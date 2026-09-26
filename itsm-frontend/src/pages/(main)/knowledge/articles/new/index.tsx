import { useLocation, useNavigate } from 'react-router';

/**
 * 知识库新建文章页面
 *
 * B7 修复：原本 /knowledge/articles/new 路由 404。
 * FE-5：正文改用公共富文本编辑器（TipTap，落库 HTML），粘贴 / 拖拽图片经 `AttachmentApi`
 *       走知识库域内别名路由（`POST /api/v1/knowledge/articles/:id/attachments`，静态权限
 *       `knowledge:write`；若改走通用 A1 会被兜底码 `attachment:write` 拦下 → 普通用户 403）。
 *
 * 新建时文章 ID 尚不存在，且后端归属校验（BE-7）在 `article_id=0` 时会剥离一切内嵌图片引用，
 * 因此沿用与工单创建页同构的「两阶段」流程：
 *   1. 粘贴 / 拖拽先插入 `blob:` 占位图，并以 `data-attachment-id="staged-xxx"` 标记待上传；
 *   2. 创建成功后逐张上传到该文章，替换为域内预览地址，再回写一次正文。
 * 图片单张失败只移除对应占位图并提示，不影响文章创建（正文仍在）。
 *
 * 预填：来自 AI 会话页「补充为知识文章」的路由 state（契约见 lib/knowledge/ai-article-prefill），
 *       内容为回答的 Markdown 原文；此时正文固定走 Markdown 模式，不塞进富文本编辑器（会丢语义）。
 */

import React, { useCallback, useEffect, useMemo, useRef, useState, lazy, Suspense } from 'react';
import {
  Card,
  Form,
  Input,
  Select,
  Tag,
  Button,
  Space,
  message,
  Typography,
  Breadcrumb,
  Skeleton,
} from 'antd';
import { ArrowLeft, Save } from 'lucide-react';
import { KnowledgeBaseApi } from '@/lib/api/knowledge-base-api';
import { AttachmentApi, knowledgeAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import { readArticlePrefillState } from '@/lib/knowledge/ai-article-prefill';
import { isRichTextEmpty, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  hasStagedImages,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { Title } = Typography;
const { TextArea } = Input;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，
// 也就不会请求编辑器 chunk（方案 §NF-2 / AC-9）。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<Skeleton.Input active block style={{ height: 320 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

export default function NewKnowledgeArticlePage() {
  const location = useLocation();
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [categories, setCategories] = useState<{ id: number; name: string }[]>([]);
  // 暂存图片：staged id → 本地 File；提交拿不到文章 ID 前不落任何远端数据
  const stagedImagesRef = useRef<Map<string, File>>(new Map());
  const richTextEnabled = isRichTextEnabled();
  // AI 会话页带入的预填（Markdown 原文）；无预填 / 结构不符时为 null。
  const prefill = useMemo(() => readArticlePrefillState(location.state), [location.state]);
  // 有预填时固定走 Markdown 正文：整篇 Markdown 进 TipTap 会被解析成纯段落，标题 / 表格语义尽失
  // （与 edit 页「HTML / Markdown 双读」策略一致）。
  const useRichEditor = richTextEnabled && !prefill;

  // 预填只应用一次：用户改动表单后，父级重渲染不得把内容冲回初值。
  const prefillAppliedRef = useRef(false);
  useEffect(() => {
    if (!prefill || prefillAppliedRef.current) return;
    prefillAppliedRef.current = true;
    form.setFieldsValue({ title: prefill.title, content: prefill.content });
  }, [form, prefill]);

  /** 正文判空：富文本与 Markdown/纯文本口径不同（后者按去空白后的长度）。 */
  const isContentEmpty = (value?: string) =>
    useRichEditor ? isRichTextEmpty(value) : String(value ?? '').trim().length === 0;

  useEffect(() => {
    KnowledgeBaseApi.getCategories()
      .then((data: any) => {
        const list = Array.isArray(data) ? data : data?.categories || [];
        setCategories(list.map((c: any) => ({ id: c.id, name: c.name })));
      })
      .catch(() => {
        // fallback 默认分类
        setCategories([
          { id: 1, name: '故障处理' },
          { id: 2, name: '操作指南' },
          { id: 3, name: '常见问题' },
        ]);
      });
  }, []);

  // 提交过程中关闭页面会丢失上传中的图片，给出浏览器确认提示
  useEffect(() => {
    if (!loading) return;
    const handler = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [loading]);

  /** 第一阶段：只登记 File 并返回 blob 占位（不发起网络请求） */
  const handleUploadImage = useCallback(async (file: File): Promise<UploadedImage> => {
    const stagedId = `${STAGED_ID_PREFIX}${Date.now().toString(36)}-${Math.random()
      .toString(36)
      .slice(2, 8)}`;
    stagedImagesRef.current.set(stagedId, file);
    return { url: URL.createObjectURL(file), id: stagedId, name: file.name };
  }, []);

  /** 第二阶段：把正文里的暂存图逐张上传并替换为正式地址（单项失败不阻断其余图片） */
  const uploadStagedImages = useCallback(async (articleId: number, html: string) => {
    const stagedIds = extractStagedImageIds(html);
    if (stagedIds.length === 0) return { html, uploaded: 0, failed: 0 };

    const replacements: Record<string, StagedImageReplacement> = {};
    let failed = 0;
    for (const stagedId of stagedIds) {
      const file = stagedImagesRef.current.get(stagedId);
      if (!file) {
        failed += 1;
        continue;
      }
      try {
        const uploaded = await AttachmentApi.upload(file, {
          bizType: 'knowledge_article',
          bizId: articleId,
          usage: 'inline_image',
        });
        replacements[stagedId] = {
          id: uploaded.id,
          // 内嵌图片走 preview（inline）；fileUrl 是带 Content-Disposition: attachment 的
          // 下载地址，直接塞进 <img> 不会显示。
          url: uploaded.previewUrl || knowledgeAttachmentPreviewUrl(articleId, uploaded.id),
          name: uploaded.fileName || file.name,
        };
        stagedImagesRef.current.delete(stagedId);
      } catch {
        failed += 1;
      }
    }
    return { html: replaceStagedImages(html, replacements), uploaded: Object.keys(replacements).length, failed };
  }, []);

  const onFinish = async (values: any) => {
    setLoading(true);
    const rawHtml: string = values.content || '';
    // 创建请求不能携带 `blob:` 占位图：对服务端无意义，且归属校验会剥离全部内嵌引用。
    const createContent = useRichEditor ? stripStagedImages(rawHtml) : rawHtml;
    try {
      const created = await KnowledgeBaseApi.createArticle({
        title: values.title,
        content: createContent || '<p></p>',
        category:
          categories.find(c => c.id === values.categoryId)?.name || String(values.categoryId),
        tags: values.tags || [],
      });

      if (useRichEditor && hasStagedImages(rawHtml)) {
        const articleId = Number(created.id);
        if (Number.isFinite(articleId) && articleId > 0) {
          const { html: finalHtml, uploaded, failed } = await uploadStagedImages(articleId, rawHtml);
          if (uploaded > 0 && finalHtml !== createContent) {
            try {
              await KnowledgeBaseApi.updateArticle(created.id, { content: finalHtml });
            } catch {
              message.warning('图片已上传，但正文回写失败，请在编辑页重新保存一次');
            }
          }
          if (failed > 0) {
            message.warning(`有 ${failed} 张图片上传失败，已从正文中移除，请重新插入`);
          }
        }
      }

      message.success('文章创建成功');
      navigate(`/knowledge/articles/${created.id}`);
    } catch (e: any) {
      message.error('创建失败：' + (e?.message || '未知错误'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="max-w-4xl mx-auto p-6">
      <Breadcrumb
        items={[{ title: '知识库', href: '/knowledge' }, { title: '新建文章' }]}
        className="mb-4"
      />
      <Card>
        <Space className="mb-4">
          <Button icon={<ArrowLeft />} onClick={() => navigate('/knowledge')}>
            返回
          </Button>
          <Title level={3} style={{ margin: 0 }}>
            新建知识库文章
          </Title>
        </Space>
        <Form
          form={form}
          layout="vertical"
          onFinish={onFinish}
          initialValues={{ categoryId: 1, tags: [] }}
        >
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input placeholder="例如：VPN 拨号失败排查指南" maxLength={200} />
          </Form.Item>

          <Form.Item
            name="categoryId"
            label="分类"
            rules={[{ required: true, message: '请选择分类' }]}
          >
            <Select
              placeholder="选择分类"
              options={categories.map(c => ({ label: c.name, value: c.id }))}
            />
          </Form.Item>

          <Form.Item name="tags" label="标签">
            <Select
              mode="tags"
              placeholder="输入标签后回车"
              tagRender={({ label, closable, onClose }) => (
                <Tag
                  closable={closable}
                  onClose={onClose}
                  className="bg-blue-100 text-blue-800 border-blue-300 mr-1 mb-1"
                >
                  {label}
                </Tag>
              )}
            />
          </Form.Item>

          <Form.Item
            name="content"
            label={useRichEditor ? '内容（支持排版与图片）' : '内容（支持 Markdown）'}
            extra={
              prefill
                ? '正文来自 AI 助手回答（Markdown 原文），保存后由详情页按 Markdown 渲染；如需富文本排版与图片，可另建文章。'
                : undefined
            }
            rules={[
              {
                validator: (_rule, value: string) =>
                  isContentEmpty(value)
                    ? Promise.reject(new Error('请输入内容'))
                    : Promise.resolve(),
              },
            ]}
          >
            {useRichEditor ? (
              <RichTextEditor
                minHeight={320}
                placeholder="请输入正文，可直接粘贴或拖拽图片（保存后自动上传到本文章附件）"
                onUploadImage={handleUploadImage}
              />
            ) : (
              <TextArea rows={15} placeholder="# 问题描述&#10;&#10;请输入内容..." />
            )}
          </Form.Item>

          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" icon={<Save />} loading={loading}>
                保存草稿
              </Button>
              <Button onClick={() => navigate('/knowledge')}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
