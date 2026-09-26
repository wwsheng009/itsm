import { useNavigate, useParams } from 'react-router';

/**
 * 知识库文章编辑页面
 * 修复：列表/详情页“编辑”按钮指向 /knowledge/articles/[id]/edit，但路由缺失导致 404。
 *
 * 正文按「内容类型」编辑与保存（text / markdown / html / rich_text）：
 * - rich_text：`RichTextEditor` 编辑，粘贴 / 拖拽图片即时上传到
 *   `POST /api/v1/knowledge/articles/:id/attachments`（域内别名，静态权限 `knowledge:write`），
 *   保存时把被删除的图片调用附件解绑（幂等，失败不阻断保存，§5.2）；
 * - markdown / text / html：保持纯文本输入域，不把整篇 Markdown / HTML 塞进富文本编辑器丢语义。
 * 类型随文章落库，详情页据此分发渲染；历史文章缺失类型时按内容形态兜底判定。
 *
 * 保存动作两种（版本 = 发布历史，见后端 publish 语义）：
 * - 「保存并重新发布」：写回内容后立即发布，内容相对上一发布版本有变化才产生新版本；
 * - 「保存为草稿」：只写回内容并回到草稿（已发布文章会因此下架），需在详情页重新发布。
 */

import React, { useCallback, useEffect, useMemo, useRef, useState, lazy, Suspense } from 'react';
import {
  Card,
  Form,
  Input,
  Alert,
  Select,
  Tag,
  Segmented,
  Button,
  Space,
  message,
  Typography,
  Breadcrumb,
  Skeleton,
} from 'antd';
import { ArrowLeft, Save, Send } from 'lucide-react';
import { KnowledgeBaseApi } from '@/lib/api/knowledge-base-api';
import { AttachmentApi, knowledgeAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import {
  extractAttachmentImageIds,
  isRichTextEmpty,
  isRichTextEnabled,
} from '@/lib/rich-text/sanitize';
import {
  ARTICLE_CONTENT_TYPE_HINTS,
  ARTICLE_CONTENT_TYPE_LABELS,
  ARTICLE_CONTENT_TYPE_OPTIONS,
  editorKindForContentType,
  resolveArticleContentType,
  type ArticleContentType,
} from '@/lib/knowledge/article-content-type';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { Title } = Typography;
const { TextArea } = Input;

// 富文本编辑器按需加载，仅在正文确为 HTML 时才渲染该分支。
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<Skeleton.Input active block style={{ height: 320 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

/** 各类型的正文输入提示（富文本走编辑器自带 placeholder）。 */
const CONTENT_PLACEHOLDERS: Record<ArticleContentType, string> = {
  rich_text: '',
  markdown: '# 问题描述\n\n请输入内容...',
  text: '请输入纯文本内容（换行会原样保留）',
  html: '<p>请输入 HTML 内容</p>',
};

export default function EditKnowledgeArticlePage() {
  const navigate = useNavigate();
  const { id } = useParams() as { id: string };
  const [form] = Form.useForm();
  // 保存动作：'save' = 仅保存（回落草稿），'publish' = 保存并重新发布
  const [savingMode, setSavingMode] = useState<'save' | 'publish' | null>(null);
  const [fetching, setFetching] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [categories, setCategories] = useState<{ id: string; name: string }[]>([]);
  // 生效正文类型：null = 尚未加载完成
  const [contentType, setContentType] = useState<ArticleContentType | null>(null);
  const [editorUploading, setEditorUploading] = useState(false);
  // 打开编辑时文章是否处于发布态：发布态文章一旦保存即回到草稿，需要明确告知用户。
  const [wasPublished, setWasPublished] = useState(false);
  // 打开编辑时的图片集合：保存后据此解绑被删除的附件
  const initialImageIdsRef = useRef<number[]>([]);
  const editorKind = contentType ? editorKindForContentType(contentType) : null;
  const richTextEnabled = isRichTextEnabled();
  // 类型可选项：VITE_RICH_TEXT=off 时不暴露富文本入口。
  const contentTypeOptions = useMemo(
    () =>
      ARTICLE_CONTENT_TYPE_OPTIONS.filter(
        option => option.value !== 'rich_text' || richTextEnabled
      ),
    [richTextEnabled]
  );

  useEffect(() => {
    KnowledgeBaseApi.getCategories()
      .then((data: any) => {
        const list = Array.isArray(data) ? data : data?.categories || [];
        setCategories(
          list.map((c: any) =>
            typeof c === 'string' ? { id: c, name: c } : { id: String(c.id), name: String(c.name) }
          )
        );
      })
      .catch(() => {
        // fallback 默认分类（分类名即标识，与后端字符串分类口径一致）
        setCategories([
          { id: '故障处理', name: '故障处理' },
          { id: '操作指南', name: '操作指南' },
          { id: '常见问题', name: '常见问题' },
        ]);
      });
  }, []);

  useEffect(() => {
    if (!id) return;
    setFetching(true);
    KnowledgeBaseApi.getArticle(id)
      .then(article => {
        // 文章响应用 category 表示所属分类（categoryName/categoryId 是旧字段兼容）；
        // 匹配不上时必须原样保留文章当前分类，不能兜底写死默认分类——否则用户只改
        // 正文，保存却把分类改写成 "1"，还会让下一次发布凭空多出一个版本。
        const ownCategory = (article.categoryName ?? article.category ?? '').trim();
        const matched = categories.find(
          c => c.name === ownCategory || String(c.id) === String(article.categoryId),
        );
        // 显式类型优先；历史文章缺失类型时按内容形态兜底（与详情页同一口径）。
        const resolvedType = resolveArticleContentType({
          contentType: article.contentType,
          content: article.content,
        });
        form.setFieldsValue({
          title: article.title,
          content: article.content,
          categoryId: matched?.id ?? (ownCategory || categories[0]?.id),
          tags: article.tags || [],
        });
        setContentType(resolvedType);
        setWasPublished(article.status === 'published');
        initialImageIdsRef.current =
          editorKindForContentType(resolvedType) === 'rich'
            ? extractAttachmentImageIds(article.content)
            : [];
      })
      .catch(() => {
        setNotFound(true);
      })
      .finally(() => setFetching(false));
    // categories 加载完成后重新匹配一次默认分类
  }, [id, categories, form]);

  /** 编辑态文章 ID 已存在：图片即时上传，直接返回可渲染的域内预览地址 */
  const handleUploadImage = useCallback(
    async (file: File): Promise<UploadedImage> => {
      const articleId = Number(id);
      if (!Number.isFinite(articleId) || articleId <= 0) {
        throw new Error('文章 ID 非法，无法上传图片');
      }
      const uploaded = await AttachmentApi.upload(file, {
        bizType: 'knowledge_article',
        bizId: articleId,
        usage: 'inline_image',
      });
      return {
        id: uploaded.id,
        url: uploaded.previewUrl || knowledgeAttachmentPreviewUrl(articleId, uploaded.id),
        name: uploaded.fileName || file.name,
      };
    },
    [id]
  );

  /** 写回文章内容，并解绑富文本中已删除的图片；失败向上抛出由调用方提示。 */
  const persistArticle = async (values: any) => {
    const content: string = values.content || '';
    await KnowledgeBaseApi.updateArticle(id, {
      title: values.title,
      content,
      contentType: contentType ?? undefined,
      category:
        categories.find(c => c.id === values.categoryId)?.name || String(values.categoryId),
      tags: values.tags || [],
    });

    // 编辑器内被删除的图片：调用附件解绑（幂等，失败不阻断保存结果）（§5.2）
    if (editorKind === 'rich') {
      const articleId = Number(id);
      const nextImageIds = extractAttachmentImageIds(content);
      const removedImageIds = initialImageIdsRef.current.filter(
        imageId => !nextImageIds.includes(imageId)
      );
      if (Number.isFinite(articleId) && articleId > 0 && removedImageIds.length > 0) {
        await Promise.allSettled(
          removedImageIds.map(imageId =>
            AttachmentApi.removeById(imageId, {
              bizType: 'knowledge_article',
              bizId: articleId,
              usage: 'inline_image',
            })
          )
        );
      }
      initialImageIdsRef.current = nextImageIds;
    }
  };

  /** 仅保存：写回内容并回到草稿态（已发布文章会因此下架）。 */
  const onFinish = async (values: any) => {
    setSavingMode('save');
    try {
      await persistArticle(values);
      if (wasPublished) {
        // 保存=编辑：已发布文章会因此下架回到草稿，必须显式提示，否则用户会以为改动已生效。
        message.warning('已保存为草稿并下架，需重新发布后才会对外生效并生成新版本');
      } else {
        message.success('文章更新成功');
      }
      navigate(`/knowledge/articles/${id}`);
    } catch (e: any) {
      message.error('更新失败：' + (e?.message || '未知错误'));
    } finally {
      setSavingMode(null);
    }
  };

  /** 保存并重新发布：一步完成「写回草稿 + 发布」；发布是产生新版本的唯一入口。 */
  const onSaveAndPublish = async () => {
    let values: any;
    try {
      values = await form.validateFields();
    } catch {
      // 校验未通过：AntD 已在表单内联提示，这里不再弹全局错误。
      return;
    }
    setSavingMode('publish');
    try {
      await persistArticle(values);
    } catch (e: any) {
      message.error('更新失败：' + (e?.message || '未知错误'));
      setSavingMode(null);
      return;
    }
    try {
      await KnowledgeBaseApi.publishArticle(id);
      message.success('已保存并重新发布');
    } catch (e: any) {
      // 内容已落库为草稿，发布失败不丢数据：回详情页可再次发布。
      message.error('已保存为草稿，但重新发布失败：' + (e?.message || '未知错误'));
    } finally {
      setSavingMode(null);
      navigate(`/knowledge/articles/${id}`);
    }
  };

  /** 切换正文类型：跨编辑形态时提示，内容原样保留、是否匹配由用户确认。 */
  const handleContentTypeChange = (value: string | number) => {
    const next = value as ArticleContentType;
    if (editorKind && editorKindForContentType(next) !== editorKind) {
      message.info(
        next === 'rich_text'
          ? '已切换到富文本：正文将按富文本编辑，原 Markdown 标记会作为普通文本处理'
          : `已切换到${ARTICLE_CONTENT_TYPE_LABELS[next]}：正文将按该类型渲染，请确认内容与类型匹配`
      );
    }
    setContentType(next);
  };

  if (notFound) {
    return (
      <div className="max-w-4xl mx-auto p-6">
        <Card>
          <Title level={4}>文章不存在或已被删除</Title>
          <Button onClick={() => navigate('/knowledge')}>返回知识库</Button>
        </Card>
      </div>
    );
  }

  return (
    <div className="max-w-4xl mx-auto p-6">
      <Breadcrumb
        items={[
          { title: '知识库', href: '/knowledge' },
          { title: '编辑文章' },
        ]}
        className="mb-4"
      />
      <Card>
        <Space className="mb-4">
          <Button icon={<ArrowLeft />} onClick={() => navigate(`/knowledge/articles/${id}`)}>
            返回
          </Button>
          <Title level={3} style={{ margin: 0 }}>
            编辑知识库文章
          </Title>
        </Space>
        {!fetching && wasPublished && (
          <Alert
            type="warning"
            showIcon
            className="mb-4"
            title="该文章当前已发布"
            description="「保存为草稿」会先下架文章，线上检索立即不可见，需重新发布后才对外生效并生成新版本；「保存并重新发布」则改动立即生效，内容与上一发布版本一致时不会产生新版本。"
          />
        )}
        {fetching ? (
          <Skeleton active paragraph={{ rows: 10 }} />
        ) : (
          <Form form={form} layout="vertical" onFinish={onFinish} initialValues={{ tags: [] }}>
            <Form.Item
              name="title"
              label="标题"
              rules={[{ required: true, message: '请输入标题' }]}
            >
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
              label="正文类型"
              extra={contentType ? `${ARTICLE_CONTENT_TYPE_HINTS[contentType]}；切换类型不会转换正文内容。` : undefined}
            >
              <Segmented
                value={contentType ?? undefined}
                onChange={handleContentTypeChange}
                options={contentTypeOptions}
              />
            </Form.Item>

            <Form.Item
              name="content"
              label={
                editorKind === 'rich'
                  ? '内容（支持排版与图片）'
                  : `内容（${contentType ? ARTICLE_CONTENT_TYPE_LABELS[contentType] : ''}）`
              }
              extra={contentType ? ARTICLE_CONTENT_TYPE_HINTS[contentType] : undefined}
              rules={[
                {
                  validator: (_rule, value: string) => {
                    const empty =
                      editorKind === 'rich'
                        ? isRichTextEmpty(value)
                        : String(value ?? '').trim().length === 0;
                    return empty ? Promise.reject(new Error('请输入内容')) : Promise.resolve();
                  },
                },
              ]}
            >
              {editorKind === 'rich' ? (
                <RichTextEditor
                  minHeight={320}
                  placeholder="请输入正文，可直接粘贴或拖拽图片（保存后自动上传到本文章附件）"
                  onUploadImage={handleUploadImage}
                  onUploadingChange={setEditorUploading}
                />
              ) : (
                <TextArea rows={15} placeholder={CONTENT_PLACEHOLDERS[contentType ?? 'markdown']} />
              )}
            </Form.Item>

            <Form.Item>
              <Space>
                {wasPublished && (
                  <Button
                    type="primary"
                    icon={<Send />}
                    loading={savingMode === 'publish'}
                    disabled={editorUploading || savingMode !== null}
                    onClick={onSaveAndPublish}
                  >
                    保存并重新发布
                  </Button>
                )}
                <Button
                  type={wasPublished ? 'default' : 'primary'}
                  htmlType="submit"
                  icon={<Save />}
                  loading={savingMode === 'save'}
                  disabled={editorUploading || savingMode !== null}
                >
                  {wasPublished ? '保存为草稿' : '保存修改'}
                </Button>
                <Button onClick={() => navigate(`/knowledge/articles/${id}`)}>取消</Button>
              </Space>
            </Form.Item>
          </Form>
        )}
      </Card>
    </div>
  );
}
