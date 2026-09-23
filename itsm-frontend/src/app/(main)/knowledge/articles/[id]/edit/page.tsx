'use client';

/**
 * 知识库文章编辑页面
 * 修复：列表/详情页“编辑”按钮指向 /knowledge/articles/[id]/edit，但路由缺失导致 404。
 *
 * FE-5：正文按格式双读——
 * - 富文本 HTML（新链路）：`RichTextEditor` 编辑，粘贴 / 拖拽图片即时上传到
 *   `POST /api/v1/knowledge/articles/:id/attachments`（域内别名，静态权限 `knowledge:write`），
 *   保存时把被删除的图片调用附件解绑（幂等，失败不阻断保存，§5.2）；
 * - 历史 Markdown：保持原 `Input.TextArea`，不把整篇 Markdown 塞进富文本编辑器丢语义。
 */

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter, useParams } from 'next/navigation';
import dynamic from 'next/dynamic';
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
import { isHtmlContent } from '@/lib/rich-text/content-format';
import { extractAttachmentImageIds, isRichTextEmpty } from '@/lib/rich-text/sanitize';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';

const { Title } = Typography;
const { TextArea } = Input;

// 富文本编辑器按需加载（ssr: false），仅在正文确为 HTML 时才渲染该分支。
const RichTextEditor = dynamic(() => import('@/components/common/rich-text/RichTextEditor'), {
  ssr: false,
  loading: () => <Skeleton.Input active block style={{ height: 320 }} />,
});

/** 正文形态：null = 尚未加载完成 */
type ContentMode = 'html' | 'markdown';

export default function EditKnowledgeArticlePage() {
  const router = useRouter();
  const { id } = useParams() as { id: string };
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [categories, setCategories] = useState<{ id: number; name: string }[]>([]);
  const [contentMode, setContentMode] = useState<ContentMode | null>(null);
  const [editorUploading, setEditorUploading] = useState(false);
  // 打开编辑时的图片集合：保存后据此解绑被删除的附件
  const initialImageIdsRef = useRef<number[]>([]);

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

  useEffect(() => {
    if (!id) return;
    setFetching(true);
    KnowledgeBaseApi.getArticle(id)
      .then(article => {
        const matched = categories.find(
          c => c.name === article.categoryName || String(c.id) === String(article.categoryId),
        );
        const mode: ContentMode = isHtmlContent(article.content) ? 'html' : 'markdown';
        form.setFieldsValue({
          title: article.title,
          content: article.content,
          categoryId: matched?.id ?? 1,
          tags: article.tags || [],
        });
        setContentMode(mode);
        initialImageIdsRef.current = mode === 'html' ? extractAttachmentImageIds(article.content) : [];
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

  const onFinish = async (values: any) => {
    setLoading(true);
    const html: string = values.content || '';
    try {
      await KnowledgeBaseApi.updateArticle(id, {
        title: values.title,
        content: html,
        category:
          categories.find(c => c.id === values.categoryId)?.name || String(values.categoryId),
        tags: values.tags || [],
      });

      // 编辑器内被删除的图片：调用附件解绑（幂等，失败不阻断保存结果）（§5.2）
      if (contentMode === 'html') {
        const articleId = Number(id);
        const nextImageIds = extractAttachmentImageIds(html);
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

      message.success('文章更新成功');
      router.push(`/knowledge/articles/${id}`);
    } catch (e: any) {
      message.error('更新失败：' + (e?.message || '未知错误'));
    } finally {
      setLoading(false);
    }
  };

  if (notFound) {
    return (
      <div className="max-w-4xl mx-auto p-6">
        <Card>
          <Title level={4}>文章不存在或已被删除</Title>
          <Button onClick={() => router.push('/knowledge')}>返回知识库</Button>
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
          <Button icon={<ArrowLeft />} onClick={() => router.push(`/knowledge/articles/${id}`)}>
            返回
          </Button>
          <Title level={3} style={{ margin: 0 }}>
            编辑知识库文章
          </Title>
        </Space>
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
              name="content"
              label={contentMode === 'html' ? '内容（支持排版与图片）' : '内容（支持 Markdown）'}
              extra={
                contentMode === 'markdown'
                  ? '本文为历史 Markdown 内容，沿用原编辑方式；如需富文本与图片，可新建文章后迁移。'
                  : undefined
              }
              rules={[
                {
                  validator: (_rule, value: string) =>
                    isRichTextEmpty(value)
                      ? Promise.reject(new Error('请输入内容'))
                      : Promise.resolve(),
                },
              ]}
            >
              {contentMode === 'html' ? (
                <RichTextEditor
                  minHeight={320}
                  placeholder="请输入正文，可直接粘贴或拖拽图片（保存后自动上传到本文章附件）"
                  onUploadImage={handleUploadImage}
                  onUploadingChange={setEditorUploading}
                />
              ) : (
                <TextArea rows={15} placeholder="# 问题描述&#10;&#10;请输入内容..." />
              )}
            </Form.Item>

            <Form.Item>
              <Space>
                <Button
                  type="primary"
                  htmlType="submit"
                  icon={<Save />}
                  loading={loading}
                  disabled={editorUploading}
                >
                  保存修改
                </Button>
                <Button onClick={() => router.push(`/knowledge/articles/${id}`)}>取消</Button>
              </Space>
            </Form.Item>
          </Form>
        )}
      </Card>
    </div>
  );
}
