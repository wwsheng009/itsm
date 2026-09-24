/**
 * 富文本只读渲染（HTML / 历史纯文本双读）。
 *
 * 单字段范式（服务请求 `reason`、知识库 `content`）新数据落 HTML，历史数据仍是
 * 纯文本 / Markdown，因此渲染必须做形态判定：
 * - HTML → `sanitizeRichTextHtml` 白名单净化后渲染，并挂图片查看器（点击放大）；
 * - 其他 → 按纯文本输出，保留换行。
 *
 * 紧凑场景（列表卡片 / 通知 / 搜索）请改用 `htmlToPlainText` 截断，
 * 不要把本组件塞进单行排版：块级标签会破坏卡片版式。
 */

import React, { useRef } from 'react';

import { isHtmlContent } from '@/lib/rich-text/content-format';
import { sanitizeRichTextHtml } from '@/lib/rich-text/sanitize';

import RichTextImageViewer from './RichTextImageViewer';

export interface RichTextContentProps {
  /** 正文（HTML 或历史纯文本） */
  content?: string | null;
  /** 空内容占位，默认 '-' */
  emptyText?: React.ReactNode;
  /** 富文本根节点 className，默认沿用详情页排版样式 */
  className?: string;
  /** 是否接管图片点击放大，默认开启（仅 HTML 分支生效） */
  enableImageViewer?: boolean;
}

export default function RichTextContent({
  content,
  emptyText = '-',
  className = 'ticket-rich-text prose max-w-none',
  enableImageViewer = true,
}: RichTextContentProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  if (!content) {
    return <>{emptyText}</>;
  }

  if (!isHtmlContent(content)) {
    // 历史纯文本 / Markdown：保持原样，仅保留换行语义。
    return <span className="whitespace-pre-wrap break-words">{content}</span>;
  }

  return (
    <>
      {/*
        净化与写入共用同一白名单（sanitizeRichTextHtml）；`data-attachment-id` 保留，
        与后端附件引用保护同一锚点。
      */}
      <div
        ref={containerRef}
        className={className}
        dangerouslySetInnerHTML={{ __html: sanitizeRichTextHtml(content) }}
      />
      <RichTextImageViewer containerRef={containerRef} enabled={enableImageViewer} />
    </>
  );
}
