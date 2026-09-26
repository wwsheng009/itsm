/**
 * 知识库正文渲染分发：按显式「内容类型」选择渲染器，而不是靠内容启发式猜测。
 *
 * 四条分支：
 * - text      ：保留换行的纯文本，不解析任何标记（避免把用户原文里的 # / | 当语法）；
 * - markdown  ：复用 AI 回答的 MarkdownMessage（react-markdown + remark-gfm + rehype-sanitize），
 *               它自带作用域样式：标题间距 / 列表符号 / 表格边框与横向滚动 / 代码块 / 引用 / 链接；
 * - html      ：白名单净化后按 HTML 语义渲染（历史导入的原始 HTML）；
 * - rich_text ：编辑器产出的富文本 HTML，净化后渲染并挂图片查看器（data-attachment-id 点击放大）。
 *
 * 类型来源统一走 `resolveArticleContentType`：显式字段优先，历史数据按内容兜底，
 * 保证「同一篇文章在详情页 / 版本预览 / 列表预览」的渲染口径一致。
 *
 * 样式口径（勿退回 Tailwind `prose`）：项目只装了 tailwindcss@4、未装 @tailwindcss/typography，
 * `prose` 没有任何规则；叠加 preflight 清零后表现为「标题与段落粘连、表格无框无内边距、
 * 列表丢符号、链接与正文同色」。富文本分支的样式来自 `.ticket-rich-text`（globals.css），
 * Markdown 分支统一由 MarkdownMessage 的作用域样式提供，两者同为 14px / 1.7 行高。
 */

import React, { useRef } from 'react';

import MarkdownMessage from '@/components/ai/MarkdownMessage';
import RichTextImageViewer from '@/components/common/rich-text/RichTextImageViewer';
import { sanitizeRichTextHtml } from '@/lib/rich-text/sanitize';
import type { ArticleContentType } from '@/lib/knowledge/article-content-type';

export interface ArticleContentRendererProps {
  /** 正文原文 */
  content?: string | null;
  /** 生效的内容类型（调用方用 resolveArticleContentType 解析） */
  contentType: ArticleContentType;
  /** 空内容占位 */
  emptyText?: React.ReactNode;
  /** Markdown 分支的附加 class（基础样式由 MarkdownMessage 作用域提供，默认无附加） */
  markdownClassName?: string;
  /** HTML 分支的容器 class；默认 'ticket-rich-text prose max-w-none' */
  htmlClassName?: string;
  /** 纯文本分支的容器 class */
  textClassName?: string;
  /** 是否接管 HTML 图片点击放大，默认开启（仅 html / rich_text 分支生效） */
  enableImageViewer?: boolean;
}

export default function ArticleContentRenderer({
  content,
  contentType,
  emptyText = '本文暂无内容。',
  markdownClassName,
  htmlClassName = 'ticket-rich-text prose max-w-none',
  textClassName = '',
  enableImageViewer = true,
}: ArticleContentRendererProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const raw = content ?? '';

  if (!raw.trim()) {
    return <>{emptyText}</>;
  }

  if (contentType === 'text') {
    return (
      <div className={`whitespace-pre-wrap break-words ${textClassName}`.trim()}>{raw}</div>
    );
  }

  if (contentType === 'markdown') {
    // 复用 MarkdownMessage 而不是自铺样式：解析链与知识库口径一致，
    // 且它的作用域样式取色走 antd token（暗色模式可用），表格自带横向滚动容器。
    return <MarkdownMessage className={markdownClassName} content={raw} />;
  }

  // html / rich_text：两者都是 HTML，区别在来源与编辑链路（rich_text 由富文本编辑器产出，
  // 携带 data-attachment-id 图片引用）。渲染统一走净化 + 图片查看器，
  // `data-attachment-id` 保留，与后端附件引用保护同一锚点。
  return (
    <>
      <div
        ref={containerRef}
        className={htmlClassName}
        dangerouslySetInnerHTML={{ __html: sanitizeRichTextHtml(raw) }}
      />
      <RichTextImageViewer containerRef={containerRef} enabled={enableImageViewer} />
    </>
  );
}
