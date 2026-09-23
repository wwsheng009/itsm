/**
 * RichTextEditorResizableImage —— 可缩放图片节点扩展。
 *
 * 在 `@tiptap/extension-image` 基础上补齐：
 *  - `width` / `height` / `data-align` 三个属性（可通过 HTML 属性落库并回显）；
 *  - NodeView：选中时显示四角拖拽手柄，指针拖动实时预览、松手才提交一次事务
 *    （避免拖动过程向 undo 历史塞入上百步）；
 *  - 手柄支持键盘 ← / →（Shift 加速）调整宽度，满足键盘可访问性。
 *
 * 存储宽度写入 `width` 属性而非内联 style：DOMPurify 会剥离 style，而
 * 详情页/编辑器均以 CSS `height: auto` 保持宽高比（见 image-size.ts 顶部说明）。
 */

import { type NodeViewRendererProps } from '@tiptap/core';
import Image from '@tiptap/extension-image';
import type { Node as ProseMirrorNode } from '@tiptap/pm/model';

import {
  IMAGE_ALIGN_ATTR,
  MIN_IMAGE_WIDTH,
  clampImageWidth,
  parseImageAlign,
  parseImageWidth,
  resizeWidthFromDrag,
  resolveImageWidthPx,
  type ImageWidth,
  type ResizeHandle,
} from '@/lib/rich-text/image-size';

/** 四角手柄：se = 右下（向右拖变大），nw = 左上（向右拖变小） */
const RESIZE_HANDLES: ResizeHandle[] = ['nw', 'ne', 'sw', 'se'];

const HANDLE_CLASS_NAME = 'rte-image-node__handle';

function widthToCss(width: ImageWidth | null | undefined): string {
  if (width === null || width === undefined) return '';
  return typeof width === 'number' ? `${width}px` : width;
}

export const RichTextEditorResizableImage = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),

      width: {
        default: null,
        parseHTML: (element: HTMLElement) => parseImageWidth(element.getAttribute('width')),
        renderHTML: (attributes: Record<string, unknown>) => {
          const width = attributes.width as ImageWidth | null | undefined;
          return width === null || width === undefined ? {} : { width: String(width) };
        },
      },

      // 仅做透传：允许粘贴来的 HTML 保留 height，UI 只改宽度并用 CSS 维持比例
      height: {
        default: null,
        parseHTML: (element: HTMLElement) => parseImageWidth(element.getAttribute('height')),
        renderHTML: (attributes: Record<string, unknown>) => {
          const height = attributes.height as ImageWidth | null | undefined;
          return height === null || height === undefined ? {} : { height: String(height) };
        },
      },

      [IMAGE_ALIGN_ATTR]: {
        default: null,
        parseHTML: (element: HTMLElement) => parseImageAlign(element.getAttribute(IMAGE_ALIGN_ATTR)),
        renderHTML: (attributes: Record<string, unknown>) => {
          const align = attributes[IMAGE_ALIGN_ATTR] as string | null | undefined;
          return align ? { [IMAGE_ALIGN_ATTR]: align } : {};
        },
      },

      // 附件回链：创建页用它标记「暂存待上传」图片（data-attachment-id="staged-xxx"），
      // 详情/编辑页用它回链真实附件。必须声明为节点属性，否则 insertContent(html)
      // 与 getHTML() 的往返会把该属性丢掉，导致暂存图片无法被替换（保存后图片消失）。
      'data-attachment-id': {
        default: null,
        parseHTML: (element: HTMLElement) => element.getAttribute('data-attachment-id'),
        renderHTML: (attributes: Record<string, unknown>) => {
          const attachmentId = attributes['data-attachment-id'] as string | null | undefined;
          return attachmentId ? { 'data-attachment-id': String(attachmentId) } : {};
        },
      },
    };
  },

  addNodeView() {
    return ({ node, editor, getPos }: NodeViewRendererProps) => {
      const wrapper = document.createElement('div');
      wrapper.className = 'rte-image-node';

      const img = document.createElement('img');
      img.className = 'rte-image-node__img';
      img.draggable = false;
      wrapper.appendChild(img);

      let currentNode: ProseMirrorNode = node;
      let detachDragListeners: (() => void) | null = null;

      const contentWidth = () =>
        Math.max(MIN_IMAGE_WIDTH, Math.round(editor.view.dom.clientWidth || 0));

      const resolvePos = (): number | null => {
        try {
          const pos = getPos();
          return typeof pos === 'number' ? pos : null;
        } catch {
          return null;
        }
      };

      const applyNode = (target: ProseMirrorNode) => {
        currentNode = target;
        const attrs = target.attrs as Record<string, unknown>;

        const src = attrs.src ? String(attrs.src) : '';
        if (src) img.setAttribute('src', src);
        else img.removeAttribute('src');

        const alt = attrs.alt ? String(attrs.alt) : '';
        if (alt) img.setAttribute('alt', alt);
        else img.setAttribute('alt', '');

        const title = attrs.title ? String(attrs.title) : '';
        if (title) img.setAttribute('title', title);
        else img.removeAttribute('title');

        const width = (attrs.width ?? null) as ImageWidth | null;
        if (widthToCss(width)) {
          img.setAttribute('width', String(width));
          img.setAttribute('aria-valuenow', String(width));
        } else {
          img.removeAttribute('width');
          img.removeAttribute('aria-valuenow');
        }
        img.style.width = '';

        const align = attrs[IMAGE_ALIGN_ATTR] ? String(attrs[IMAGE_ALIGN_ATTR]) : '';
        if (align) wrapper.setAttribute(IMAGE_ALIGN_ATTR, align);
        else wrapper.removeAttribute(IMAGE_ALIGN_ATTR);

        const attachmentId = attrs['data-attachment-id'] ? String(attrs['data-attachment-id']) : '';
        if (attachmentId) img.setAttribute('data-attachment-id', attachmentId);
        else img.removeAttribute('data-attachment-id');
      };

      applyNode(node);

      const currentWidthPx = (): number => {
        const resolved = resolveImageWidthPx(
          (currentNode.attrs.width ?? null) as ImageWidth | null,
          contentWidth()
        );
        if (resolved !== null) return resolved;
        const rect = img.getBoundingClientRect?.();
        return Math.max(MIN_IMAGE_WIDTH, Math.round(rect?.width || MIN_IMAGE_WIDTH));
      };

      const commitWidth = (width: number) => {
        const pos = resolvePos();
        if (pos === null) return;
        const { state } = editor.view;
        const target = state.doc.nodeAt(pos);
        if (!target || target.type !== currentNode.type) return;

        const next = clampImageWidth(width, contentWidth());
        if (target.attrs.width === next) return;
        editor.view.dispatch(
          state.tr.setNodeMarkup(pos, undefined, { ...target.attrs, width: next })
        );
      };

      const startResize = (event: PointerEvent, handle: ResizeHandle) => {
        if (event.button !== 0 || !editor.isEditable) return;
        event.preventDefault();
        event.stopPropagation();

        // 保证图片处于 NodeSelection：浮动工具条与键盘删除都依赖该选中态
        const pos = resolvePos();
        if (pos !== null) editor.commands.setNodeSelection(pos);

        const startX = event.clientX;
        const startWidth = currentWidthPx();
        const maxWidth = contentWidth();
        let latest = startWidth;

        const onMove = (moveEvent: PointerEvent) => {
          latest = resizeWidthFromDrag({
            startWidth,
            deltaX: moveEvent.clientX - startX,
            handle,
            containerWidth: maxWidth,
          });
          // 拖动过程只改 DOM，不派发事务：松手时提交一次，undo 只回退一步
          img.style.width = `${latest}px`;
        };

        const finish = () => {
          detachDragListeners?.();
          detachDragListeners = null;
          img.style.width = '';
          commitWidth(latest);
        };

        window.addEventListener('pointermove', onMove);
        window.addEventListener('pointerup', finish);
        window.addEventListener('pointercancel', finish);
        detachDragListeners = () => {
          window.removeEventListener('pointermove', onMove);
          window.removeEventListener('pointerup', finish);
          window.removeEventListener('pointercancel', finish);
        };
      };

      const handleElements = RESIZE_HANDLES.map((handle) => {
        const element = document.createElement('span');
        element.className = `${HANDLE_CLASS_NAME} ${HANDLE_CLASS_NAME}--${handle}`;
        element.dataset.handle = handle;
        element.tabIndex = 0;
        element.setAttribute('role', 'slider');
        element.setAttribute('aria-label', `拖拽或使用方向键调整图片宽度（${handle}）`);
        element.setAttribute('aria-orientation', 'horizontal');
        element.setAttribute('aria-valuemin', String(MIN_IMAGE_WIDTH));

        element.addEventListener('pointerdown', (event) => startResize(event, handle));

        element.addEventListener('keydown', (event) => {
          if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
          event.preventDefault();
          const step = event.shiftKey ? 50 : 10;
          const delta = event.key === 'ArrowRight' ? step : -step;
          const pos = resolvePos();
          if (pos !== null) editor.commands.setNodeSelection(pos);
          commitWidth(currentWidthPx() + delta);
        });

        wrapper.appendChild(element);
        return element;
      });

      const isHandleTarget = (target: EventTarget | null) =>
        target instanceof HTMLElement && target.classList.contains(HANDLE_CLASS_NAME);

      // 手柄拖动期间禁止原生 HTML5 拖拽（否则会变成“拖动图片节点”）
      wrapper.addEventListener('dragstart', (event) => {
        if (isHandleTarget(event.target) || detachDragListeners) event.preventDefault();
      });

      return {
        dom: wrapper,

        update: (updatedNode: ProseMirrorNode) => {
          if (updatedNode.type !== currentNode.type) return false;
          applyNode(updatedNode);
          handleElements.forEach((element) => {
            element.setAttribute('aria-valuemax', String(contentWidth()));
            element.setAttribute(
              'aria-valuenow',
              String(resolveImageWidthPx(currentNode.attrs.width ?? null, contentWidth()) ?? '')
            );
          });
          return true;
        },

        selectNode: () => {
          wrapper.classList.add('rte-image-node--selected');
          handleElements.forEach((element) =>
            element.setAttribute('aria-valuemax', String(contentWidth()))
          );
        },

        deselectNode: () => {
          wrapper.classList.remove('rte-image-node--selected');
        },

        stopEvent: (event: Event) => isHandleTarget(event.target),

        destroy: () => {
          detachDragListeners?.();
          detachDragListeners = null;
        },
      };
    };
  },
});

export default RichTextEditorResizableImage;
