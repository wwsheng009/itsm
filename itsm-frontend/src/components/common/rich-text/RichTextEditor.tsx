
/**
 * RichTextEditor —— 富文本编辑器（TipTap v2）
 *
 * 设计文档 §4.5：富输入优化
 *  - 工具栏：加粗 / 斜体 / 下划线 / 删除线 / 标题 / 有序列表 / 无序列表 / 引用 / 代码块 / 链接 / 图片 / 撤销 / 重做
 *  - 粘贴、拖拽图片自动上传，上传成功后插入 <img src data-attachment-id>
 *  - 图片可编辑：选中后四角可拖拽缩放、浮动工具条可设尺寸/对齐/还原/删除
 *    （width/height/data-align 属性落库，详见 RichTextEditorResizableImage）
 *  - 输出统一经过 sanitizeRichTextHtml 白名单净化，避免 XSS
 *
 * 上传函数通过 onUploadImage 注入；未注入时**不回落到任何内置端点**（fail-fast）：
 * 图片按钮禁用、粘贴/拖拽图片被拦截并告警，避免请求未注册接口产生静默 404。
 * 组件本身不直接依赖具体业务 API，便于在工单创建页、回复框等场景复用。
 */

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { App, Button, Divider, Space, Spin, Tooltip, Typography } from 'antd';
import {
  BoldOutlined,
  ItalicOutlined,
  UnderlineOutlined,
  StrikethroughOutlined,
  OrderedListOutlined,
  UnorderedListOutlined,
  CodeOutlined,
  RedoOutlined,
  UndoOutlined,
  LinkOutlined,
  PictureOutlined,
  FontSizeOutlined,
} from '@ant-design/icons';
import { useEditor, EditorContent, type Editor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import Underline from '@tiptap/extension-underline';
import Link from '@tiptap/extension-link';
import Placeholder from '@tiptap/extension-placeholder';

import { sanitizeRichTextHtml, isRichTextEmpty } from '@/lib/rich-text/sanitize';
import RichTextEditorResizableImage from './RichTextEditorResizableImage';
import RichTextEditorImageMenu from './RichTextEditorImageMenu';

const { Text } = Typography;

/** 上传成功后的返回值 */
export interface UploadedImage {
  /** 可直接用于 <img src> 的地址（通常是站内附件代理地址） */
  url: string;
  /** 附件 ID，写入 data-attachment-id，便于后端回填/校验 */
  id?: number | string;
  /** 原始文件名，写入 alt */
  name?: string;
}

export interface RichTextEditorProps {
  /** 受控值：富文本 HTML 字符串 */
  value?: string;
  /** 变更回调，参数已通过白名单净化 */
  onChange?: (html: string) => void;
  /** 失焦回调 */
  onBlur?: () => void;
  placeholder?: string;
  disabled?: boolean;
  /** 编辑区最小高度，默认 220 */
  minHeight?: number;
  /** 上传中状态变化（父级可用于提交前拦截） */
  onUploadingChange?: (uploading: boolean) => void;
  /**
   * 自定义上传实现。**未传时图片能力整体禁用**（fail-fast，方案 D6/AC-8）：
   * 图片按钮禁用、粘贴/拖拽图片被拦截并告警，不发起任何上传请求。
   */
  onUploadImage?: (file: File) => Promise<UploadedImage>;
  /** 允许粘贴/拖拽上传的最大单文件体积（MB），默认 10 */
  maxImageSizeMB?: number;
  /** 单次最多粘贴/拖拽图片数量，默认 9 */
  maxImageCount?: number;
  /** 无工具栏（只读展示等） */
  hideToolbar?: boolean;
  /** 编辑区 data-testid（透传到 contenteditable，便于 E2E 直接 fill/type） */
  dataTestId?: string;
}

const buildImageHtml = (img: UploadedImage): string => {
  const alt = (img.name || '图片').replace(/"/g, '&quot;');
  const idAttr = img.id !== undefined && img.id !== null ? ` data-attachment-id="${String(img.id)}"` : '';
  return `<img src="${img.url}" alt="${alt}"${idAttr} />`;
};

export const RichTextEditor: React.FC<RichTextEditorProps> = ({
  value = '',
  onChange,
  onBlur,
  placeholder,
  disabled = false,
  minHeight = 220,
  onUploadingChange,
  onUploadImage,
  maxImageSizeMB = 10,
  maxImageCount = 9,
  hideToolbar = false,
  dataTestId,
}) => {
  const { message } = App.useApp();
  const [pending, setPending] = useState(0);
  const lastEmitted = useRef<string>(value || '');
  // fail-fast：未注入上传实现时不回落到任何内置端点（方案 D6）。
  const canUploadImages = typeof onUploadImage === 'function';
  const uploadImpl = canUploadImages ? onUploadImage : undefined;
  const effectivePlaceholder =
    placeholder ?? (canUploadImages ? '请输入内容，支持粘贴或拖拽图片' : '请输入内容');
  const warnNoUploadCapacity = useCallback(() => {
    message.warning('当前场景未配置图片上传能力，已忽略图片（不会发起上传请求）');
  }, [message]);

  const uploadFiles = useCallback(
    async (files: File[], editor: Editor) => {
      if (!uploadImpl) {
        warnNoUploadCapacity();
        return;
      }
      const images = files.filter((f) => f.type.startsWith('image/'));
      if (images.length === 0) return;

      const accepted = images.slice(0, maxImageCount);
      if (images.length > maxImageCount) {
        message.warning(`一次最多上传 ${maxImageCount} 张图片，已忽略多余部分`);
      }

      const oversized = accepted.filter((f) => f.size > maxImageSizeMB * 1024 * 1024);
      if (oversized.length > 0) {
        message.error(`单张图片不能超过 ${maxImageSizeMB}MB：${oversized.map((f) => f.name).join('、')}`);
      }

      const valid = accepted.filter((f) => f.size <= maxImageSizeMB * 1024 * 1024);
      if (valid.length === 0) return;

      setPending((n) => n + valid.length);
      onUploadingChange?.(true);

      let okCount = 0;
      for (const file of valid) {
        try {
          const uploaded = await uploadImpl(file);
          editor.chain().focus().insertContent(buildImageHtml(uploaded)).run();
          okCount += 1;
        } catch (err) {
          const msg = err instanceof Error ? err.message : '未知错误';
          message.error(`图片「${file.name}」上传失败：${msg}`);
        } finally {
          setPending((n) => {
            const next = Math.max(0, n - 1);
            if (next === 0) onUploadingChange?.(false);
            return next;
          });
        }
      }

      if (okCount > 0) {
        message.success(`已上传 ${okCount} 张图片`);
      }
    },
    [maxImageCount, maxImageSizeMB, message, onUploadingChange, uploadImpl, warnNoUploadCapacity]
  );

  const editor = useEditor({
    immediatelyRender: false,
    editable: !disabled,
    extensions: [
      StarterKit.configure({
        heading: { levels: [1, 2, 3] },
      }),
      Underline,
      Link.configure({
        openOnClick: false,
        autolink: true,
        protocols: ['http', 'https', 'mailto'],
        HTMLAttributes: { rel: 'noopener noreferrer nofollow', target: '_blank' },
      }),
      RichTextEditorResizableImage.configure({ inline: false, allowBase64: false }),
      Placeholder.configure({ placeholder: effectivePlaceholder }),
    ],
    content: value || '',
    editorProps: {
      attributes: {
        class: 'rte-content',
        style: `min-height:${minHeight}px;outline:none;`,
        ...(dataTestId ? { 'data-testid': dataTestId } : {}),
      },
      handlePaste(view, event) {
        const items = Array.from(event.clipboardData?.items || []);
        const files = items
          .filter((it) => it.kind === 'file' && it.type.startsWith('image/'))
          .map((it) => it.getAsFile())
          .filter((f): f is File => Boolean(f));
        if (files.length === 0) return false;
        event.preventDefault();
        const instance = view?.state ? (undefined as unknown as Editor) : (undefined as unknown as Editor);
        void instance;
        // 未注入上传能力时同样吞掉图片粘贴，交由外层容器告警处理，避免浏览器默认行为插入 base64。
        return canUploadImages;
      },
      handleDrop(view, event, _slice, moved) {
        if (moved) return false;
        const dt = (event as DragEvent).dataTransfer;
        const files = Array.from(dt?.files || []).filter((f) => f.type.startsWith('image/'));
        if (files.length === 0) return false;
        event.preventDefault();
        return canUploadImages;
      },
    },
    onUpdate({ editor: ed }) {
      const clean = sanitizeRichTextHtml(ed.getHTML());
      lastEmitted.current = clean;
      onChange?.(clean);
    },
    onBlur() {
      onBlur?.();
    },
  });

  // 外部 value 同步（避免与内部编辑状态互相打架）
  useEffect(() => {
    if (!editor) return;
    const incoming = value || '';
    if (incoming === lastEmitted.current) return;
    if (incoming === editor.getHTML()) return;
    lastEmitted.current = incoming;
    // TipTap 2 的 setContent 第二参为 boolean（是否 emit update）：
    // 传 false 避免外部 value 同步触发 onUpdate，导致脏标记/回写风暴。
    editor.commands.setContent(incoming, false);
  }, [editor, value]);

  // 可编辑态同步
  useEffect(() => {
    if (!editor) return;
    editor.setEditable(!disabled);
  }, [editor, disabled]);

  const handlePasteRef = useRef<(files: File[]) => void>(() => {});
  handlePasteRef.current = (files: File[]) => {
    if (editor) void uploadFiles(files, editor);
  };

  const handleDropRef = useRef<(files: File[]) => void>(() => {});
  handleDropRef.current = (files: File[]) => {
    if (editor) void uploadFiles(files, editor);
  };

  const uploading = pending > 0;

  const setLink = useCallback(() => {
    if (!editor) return;
    const prev = editor.getAttributes('link').href as string | undefined;
    const input = window.prompt('请输入链接地址', prev || 'https://');
    if (input === null) return;
    if (input === '') {
      editor.chain().focus().extendMarkRange('link').unsetLink().run();
      return;
    }
    editor.chain().focus().extendMarkRange('link').setLink({ href: input }).run();
  }, [editor]);

  const insertImageByPick = useCallback(() => {
    if (!editor) return;
    if (!uploadImpl) {
      warnNoUploadCapacity();
      return;
    }
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/*';
    input.multiple = true;
    input.onchange = () => {
      const files = Array.from(input.files || []);
      if (files.length > 0) void uploadFiles(files, editor);
    };
    input.click();
  }, [editor, uploadFiles, uploadImpl, warnNoUploadCapacity]);

  const btn = (
    key: string,
    icon: React.ReactNode,
    title: string,
    active: boolean,
    action: () => void,
    disabledBtn = false
  ) => (
    <Tooltip title={title} key={key}>
      <Button
        size="small"
        type={active ? 'primary' : 'text'}
        icon={icon}
        disabled={disabled || disabledBtn}
        aria-label={title}
        onMouseDown={(e) => e.preventDefault()}
        onClick={action}
      />
    </Tooltip>
  );

  return (
    <div
      className="rich-text-editor"
      data-testid="rich-text-editor"
    >
      {!hideToolbar && (
        <div className="rich-text-editor__toolbar">
          {btn('bold', <BoldOutlined />, '加粗', !!editor?.isActive('bold'), () => editor?.chain().focus().toggleBold().run())}
          {btn('italic', <ItalicOutlined />, '斜体', !!editor?.isActive('italic'), () => editor?.chain().focus().toggleItalic().run())}
          {btn('underline', <UnderlineOutlined />, '下划线', !!editor?.isActive('underline'), () => editor?.chain().focus().toggleUnderline().run())}
          {btn('strike', <StrikethroughOutlined />, '删除线', !!editor?.isActive('strike'), () => editor?.chain().focus().toggleStrike().run())}
          <Divider type="vertical" />
          {btn(
            'h2',
            <FontSizeOutlined />,
            '标题',
            !!editor?.isActive('heading', { level: 2 }),
            () => editor?.chain().focus().toggleHeading({ level: 2 }).run()
          )}
          {btn('ul', <UnorderedListOutlined />, '无序列表', !!editor?.isActive('bulletList'), () => editor?.chain().focus().toggleBulletList().run())}
          {btn('ol', <OrderedListOutlined />, '有序列表', !!editor?.isActive('orderedList'), () => editor?.chain().focus().toggleOrderedList().run())}
          {btn('quote', <CodeOutlined />, '引用', !!editor?.isActive('blockquote'), () => editor?.chain().focus().toggleBlockquote().run())}
          <Divider type="vertical" />
          {btn('link', <LinkOutlined />, '插入链接', !!editor?.isActive('link'), setLink)}
          {btn(
            'image',
            <PictureOutlined />,
            canUploadImages
              ? '插入图片（支持拖拽调整大小）'
              : '当前场景未配置图片上传能力，插入图片已禁用',
            false,
            insertImageByPick,
            !canUploadImages
          )}
          <Divider type="vertical" />
          {btn('undo', <UndoOutlined />, '撤销', false, () => editor?.chain().focus().undo().run(), !editor?.can().undo())}
          {btn('redo', <RedoOutlined />, '重做', false, () => editor?.chain().focus().redo().run(), !editor?.can().redo())}
          <div style={{ flex: 1 }} />
          {uploading && (
            <Space size={4}>
              <Spin size="small" />
              <Text type="secondary" style={{ fontSize: 12 }}>
                图片上传中（{pending}）
              </Text>
            </Space>
          )}
        </div>
      )}

      {!hideToolbar && editor && !disabled && <RichTextEditorImageMenu editor={editor} />}

      <div
        className={`ticket-rich-editor${disabled ? ' ticket-rich-editor--disabled' : ''}`}
        onPaste={(e) => {
          const files = Array.from(e.clipboardData?.files || []).filter((f) => f.type.startsWith('image/'));
          if (files.length === 0) return;
          e.preventDefault();
          if (!canUploadImages) {
            warnNoUploadCapacity();
            return;
          }
          handlePasteRef.current(files);
        }}
        onDrop={(e) => {
          const files = Array.from(e.dataTransfer?.files || []).filter((f) => f.type.startsWith('image/'));
          if (files.length === 0) return;
          e.preventDefault();
          if (!canUploadImages) {
            warnNoUploadCapacity();
            return;
          }
          handleDropRef.current(files);
        }}
        onDragOver={(e) => e.preventDefault()}
      >
        <EditorContent editor={editor} style={{ padding: '8px 12px' }} />
        {editor && isRichTextEmpty(editor.getHTML()) && (
          <div aria-hidden className="ticket-rich-editor__placeholder">
            {effectivePlaceholder}
          </div>
        )}
      </div>

      <style>{`
        .rich-text-editor .rte-content p {
          margin: 0 0 8px;
        }
        .rich-text-editor .rte-content:focus-visible {
          outline: 2px solid #1677ff;
          outline-offset: -2px;
        }
        .rich-text-editor .rte-content img {
          max-width: 100%;
          height: auto;
        }
        /* ---- 可缩放图片（NodeView）：选中描边 + 四角拖拽手柄 ---- */
        .rich-text-editor .rte-image-node {
          position: relative;
          display: block;
          width: fit-content;
          max-width: 100%;
          line-height: 0;
        }
        .rich-text-editor .rte-image-node[data-align='center'] {
          margin-left: auto;
          margin-right: auto;
        }
        .rich-text-editor .rte-image-node[data-align='right'] {
          margin-left: auto;
        }
        .rich-text-editor .rte-image-node--selected {
          outline: 2px solid var(--rte-image-outline, #1677ff);
          outline-offset: 2px;
        }
        .rich-text-editor .rte-image-node__img {
          display: block;
          max-width: 100%;
          height: auto;
        }
        .rich-text-editor .rte-image-node__handle {
          position: absolute;
          display: none;
          width: 10px;
          height: 10px;
          background: #fff;
          border: 1.5px solid var(--rte-image-outline, #1677ff);
          border-radius: 2px;
          touch-action: none;
        }
        .rich-text-editor .rte-image-node--selected .rte-image-node__handle,
        .rich-text-editor .rte-image-node:focus-within .rte-image-node__handle {
          display: block;
        }
        .rich-text-editor .rte-image-node__handle:focus-visible {
          outline: 2px solid var(--rte-image-outline, #1677ff);
          outline-offset: 2px;
        }
        .rich-text-editor .rte-image-node__handle--nw {
          top: -5px;
          left: -5px;
          cursor: nwse-resize;
        }
        .rich-text-editor .rte-image-node__handle--ne {
          top: -5px;
          right: -5px;
          cursor: nesw-resize;
        }
        .rich-text-editor .rte-image-node__handle--sw {
          bottom: -5px;
          left: -5px;
          cursor: nesw-resize;
        }
        .rich-text-editor .rte-image-node__handle--se {
          bottom: -5px;
          right: -5px;
          cursor: nwse-resize;
        }
        /* ---- 图片浮动工具条（BubbleMenu 渲染到 body，需独立类名定样式） ---- */
        .rich-text-editor__image-menu .rich-text-editor__image-menu-inner {
          display: flex;
          align-items: center;
          gap: 2px;
          padding: 4px 6px;
          border: 1px solid var(--rte-menu-border, rgba(0, 0, 0, 0.12));
          border-radius: 8px;
          background: var(--rte-menu-bg, #fff);
          box-shadow: 0 6px 16px rgba(0, 0, 0, 0.16);
        }
        .dark .rich-text-editor__image-menu .rich-text-editor__image-menu-inner {
          border-color: rgba(255, 255, 255, 0.18);
          background: #1f1f1f;
        }
        .rich-text-editor .rte-content blockquote {
          border-left: 3px solid var(--rte-quote-border, #d9d9d9);
          margin: 8px 0;
          padding-left: 12px;
          color: var(--rte-quote-text, #595959);
        }
        .rich-text-editor .rte-content pre {
          background: var(--rte-pre-bg, #f5f5f5);
          border-radius: 4px;
          padding: 8px 12px;
          overflow: auto;
        }
        .rich-text-editor .rte-content table {
          border-collapse: collapse;
        }
        .rich-text-editor .rte-content th,
        .rich-text-editor .rte-content td {
          border: 1px solid var(--rte-border, #d9d9d9);
          padding: 4px 8px;
        }
      `}</style>
    </div>
  );
};

export default RichTextEditor;
