'use client';

/**
 * 工单详情页：富文本图片查看器（方案见 docs/architecture/ticket-create-page-rich-input-optimization.md §4.9）
 *
 * 详情页的描述是只读回显（`.ticket-rich-text`），图片被 `max-width: 100%` 限制在正文宽度内，
 * 截图/长图/拓扑图往往看不清。本组件接管容器内所有 `<img>` 的点击与键盘激活，弹出全屏查看器：
 *
 * - 放大 / 缩小：工具栏按钮、鼠标滚轮、（`+` / `-` 键）
 * - 原始尺寸 1:1、适应窗口（默认，不把小于视口的图放大）、双击在两者间切换
 * - 左 / 右旋转 90°（`r` / `Shift+R`）、水平 / 垂直翻转
 * - 放大超出视口后可拖动平移（指针拖拽）；重置（`0`）恢复初始状态
 * - 多图时上一张 / 下一张（← / → 键），下载原图，Esc / 点击背景 / 关闭按钮退出
 *
 * 仅接管「点击打开」这一增量能力，不改变 `.ticket-rich-text` 的既有渲染与清洗链路。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { PointerEvent as ReactPointerEvent, ReactNode, RefObject } from 'react';
import { createPortal } from 'react-dom';
// 图标统一用 lucide-react：与工单详情页其余图标一致，且避免 @ant-design/icons
// 的 CJS 包在 Jest 下解析到 @ant-design/colors 的 ESM 产物而无法执行。
import {
  ChevronLeft,
  ChevronRight,
  Download,
  Expand,
  FlipHorizontal,
  FlipVertical,
  RotateCcw,
  RotateCw,
  Scaling,
  Undo2,
  X,
  ZoomIn,
  ZoomOut,
} from 'lucide-react';

import {
  IMAGE_ROTATION_STEP,
  clampImageOffset,
  computeFitScale,
  formatScalePercent,
  isQuarterTurn,
  nextImageScale,
  rotateImage,
} from '@/lib/rich-text/image-viewer';
import type { ImageOffset, ImageSize } from '@/lib/rich-text/image-viewer';

export interface RichTextImageViewerProps {
  /** 富文本容器（详情页 `.ticket-rich-text` 的 ref）；为 null 时不接管 */
  containerRef: RefObject<HTMLElement | null>;
  /** 关闭（如 NEXT_PUBLIC_RICH_TEXT=off 的纯文本回退）时不接管点击 */
  enabled?: boolean;
}

interface ViewerImage {
  src: string;
  alt: string;
  naturalWidth: number;
  naturalHeight: number;
}

const ZERO_SIZE: ImageSize = { width: 0, height: 0 };

function readImage(img: HTMLImageElement): ViewerImage {
  return {
    src: img.currentSrc || img.getAttribute('src') || '',
    alt: img.getAttribute('alt') || '',
    naturalWidth: img.naturalWidth || 0,
    naturalHeight: img.naturalHeight || 0,
  };
}

function listImages(container: HTMLElement | null): HTMLImageElement[] {
  if (!container) return [];
  return Array.from(container.querySelectorAll<HTMLImageElement>('img')).filter(
    (img) => Boolean(img.getAttribute('src'))
  );
}

function downloadName(image: ViewerImage): string {
  if (image.alt.trim()) return image.alt.trim();
  const lastSegment = image.src.split('?')[0].split('/').filter(Boolean).pop();
  if (!lastSegment) return 'image';
  try {
    return decodeURIComponent(lastSegment);
  } catch {
    return lastSegment;
  }
}

export default function RichTextImageViewer({ containerRef, enabled = true }: RichTextImageViewerProps) {
  const [images, setImages] = useState<ViewerImage[]>([]);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [fitMode, setFitMode] = useState(true);
  const [manualScale, setManualScale] = useState(1);
  const [rotation, setRotation] = useState(0);
  const [flipX, setFlipX] = useState(false);
  const [flipY, setFlipY] = useState(false);
  const [offset, setOffset] = useState<ImageOffset>({ x: 0, y: 0 });
  const [natural, setNatural] = useState<ImageSize>(ZERO_SIZE);
  const [stage, setStage] = useState<ImageSize>(ZERO_SIZE);
  const [dragging, setDragging] = useState(false);

  const dialogRef = useRef<HTMLDivElement | null>(null);
  const stageRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const dragRef = useRef<{ pointerId: number; startX: number; startY: number; origin: ImageOffset } | null>(
    null
  );

  const open = activeIndex >= 0 && images.length > 0;
  const current = open ? images[Math.min(activeIndex, images.length - 1)] : null;

  const swapped = isQuarterTurn(rotation);
  const fitScale = useMemo(
    () =>
      computeFitScale(
        swapped ? natural.height : natural.width,
        swapped ? natural.width : natural.height,
        stage.width,
        stage.height
      ),
    [natural, stage, swapped]
  );
  const scale = fitMode ? fitScale : manualScale;
  const contentSize = useMemo<ImageSize>(() => ({
    width: (swapped ? natural.height : natural.width) * scale,
    height: (swapped ? natural.width : natural.height) * scale,
  }), [natural, scale, swapped]);
  const pannable = contentSize.width > stage.width + 1 || contentSize.height > stage.height + 1;

  /** 打开查看器：以被点击的图片为起点，图片列表取自容器内所有可见图片 */
  const openAt = useCallback(
    (img: HTMLImageElement) => {
      const elements = listImages(containerRef.current);
      if (elements.length === 0) return;

      const index = Math.max(0, elements.indexOf(img));
      const target = elements[index];
      triggerRef.current = img;
      setImages(elements.map(readImage));
      setActiveIndex(index);
      setFitMode(true);
      setManualScale(1);
      setRotation(0);
      setFlipX(false);
      setFlipY(false);
      setOffset({ x: 0, y: 0 });
      setNatural({
        width: target.naturalWidth || 0,
        height: target.naturalHeight || 0,
      });
    },
    [containerRef]
  );

  const close = useCallback(() => {
    setActiveIndex(-1);
    setImages([]);
    const trigger = triggerRef.current;
    triggerRef.current = null;
    // 关闭后把焦点还给触发图片，键盘用户不丢上下文
    if (trigger && trigger.isConnected) trigger.focus();
  }, []);

  /** 切换图片（多图时） */
  const showImage = useCallback(
    (nextIndex: number) => {
      if (images.length === 0) return;
      const index = ((nextIndex % images.length) + images.length) % images.length;
      const target = images[index];
      setActiveIndex(index);
      setNatural({ width: target.naturalWidth || 0, height: target.naturalHeight || 0 });
      setOffset({ x: 0, y: 0 });
      setFitMode(true);
      setManualScale(1);
    },
    [images]
  );

  const zoom = useCallback(
    (direction: 'in' | 'out') => {
      const base = fitMode ? fitScale : manualScale;
      setManualScale(nextImageScale(base, direction));
      setFitMode(false);
    },
    [fitMode, fitScale, manualScale]
  );

  const zoomTo = useCallback((value: number) => {
    setManualScale(value);
    setFitMode(false);
  }, []);

  const reset = useCallback(() => {
    setFitMode(true);
    setManualScale(1);
    setRotation(0);
    setFlipX(false);
    setFlipY(false);
    setOffset({ x: 0, y: 0 });
  }, []);

  const rotate = useCallback((delta: number) => {
    setRotation((prev) => rotateImage(prev, delta));
    setOffset({ x: 0, y: 0 });
  }, []);

  /** 焦点圈定在弹层内（Tab / Shift+Tab 循环） */
  const trapFocus = useCallback((event: KeyboardEvent) => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const focusable = Array.from(
      dialog.querySelectorAll<HTMLElement>(
        'button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])'
      )
    );
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement as HTMLElement | null;
    if (event.shiftKey && (active === first || !dialog.contains(active))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }, []);

  // 容器内图片：点击 / 回车 / 空格打开查看器；顺带补齐键盘可达性提示。
  // 描述 HTML 走 dangerouslySetInnerHTML，内容变化会重建节点，故用 MutationObserver 增量装饰。
  useEffect(() => {
    const container = containerRef.current;
    if (!container || !enabled) return;

    const decorate = () => {
      container.querySelectorAll<HTMLImageElement>('img').forEach((img) => {
        if (!img.getAttribute('src')) return;
        img.tabIndex = 0;
        img.setAttribute('role', 'button');
        if (!img.getAttribute('aria-label')) {
          const alt = img.getAttribute('alt') || '';
          img.setAttribute('aria-label', `查看图片${alt ? `：${alt}` : ''}`);
        }
        if (!img.getAttribute('title')) {
          img.setAttribute('title', '点击查看大图（可缩放 / 旋转 / 翻转）');
        }
      });
    };
    decorate();

    const onClick = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      const img = target?.closest?.('img') as HTMLImageElement | null;
      if (!img || !container.contains(img)) return;
      event.preventDefault();
      openAt(img);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Enter' && event.key !== ' ') return;
      const target = event.target as HTMLElement | null;
      if (!target || target.tagName !== 'IMG') return;
      event.preventDefault();
      openAt(target as unknown as HTMLImageElement);
    };

    const observer = new MutationObserver(decorate);
    container.addEventListener('click', onClick);
    container.addEventListener('keydown', onKeyDown);
    observer.observe(container, { childList: true, subtree: true });

    return () => {
      container.removeEventListener('click', onClick);
      container.removeEventListener('keydown', onKeyDown);
      observer.disconnect();
    };
  }, [containerRef, enabled, openAt]);

  // 打开期间：锁定页面滚动、测量画布、聚焦关闭按钮
  useEffect(() => {
    if (!open) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const measure = () => {
      const rect = stageRef.current?.getBoundingClientRect();
      if (rect) setStage({ width: Math.round(rect.width), height: Math.round(rect.height) });
    };
    measure();
    window.addEventListener('resize', measure);
    const timer = window.setTimeout(() => {
      dialogRef.current?.querySelector<HTMLElement>('[data-viewer-autofocus]')?.focus();
    }, 0);

    return () => {
      window.clearTimeout(timer);
      window.removeEventListener('resize', measure);
      document.body.style.overflow = previousOverflow;
    };
  }, [open]);

  // 缩放 / 旋转 / 换图后夹取平移量，避免图片被拖出视野
  useEffect(() => {
    setOffset((prev) => {
      const next = clampImageOffset(prev, contentSize, stage);
      return next.x === prev.x && next.y === prev.y ? prev : next;
    });
  }, [contentSize, stage]);

  // 键盘：Esc 关闭、←→ 换图、+/- 缩放、0 重置、1 原始尺寸、f 适应、r/R 旋转、Tab 圈定焦点
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      switch (event.key) {
        case 'Escape':
          event.preventDefault();
          close();
          break;
        case 'ArrowLeft':
          if (images.length > 1) {
            event.preventDefault();
            showImage(activeIndex - 1);
          }
          break;
        case 'ArrowRight':
          if (images.length > 1) {
            event.preventDefault();
            showImage(activeIndex + 1);
          }
          break;
        case '+':
        case '=':
          event.preventDefault();
          zoom('in');
          break;
        case '-':
        case '_':
          event.preventDefault();
          zoom('out');
          break;
        case '0':
          event.preventDefault();
          reset();
          break;
        case '1':
          event.preventDefault();
          zoomTo(1);
          break;
        case 'f':
        case 'F':
          event.preventDefault();
          setFitMode(true);
          break;
        case 'r':
          event.preventDefault();
          rotate(IMAGE_ROTATION_STEP);
          break;
        case 'R':
          event.preventDefault();
          rotate(-IMAGE_ROTATION_STEP);
          break;
        case 'Tab':
          trapFocus(event);
          break;
        default:
          break;
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, close, images.length, activeIndex, showImage, zoom, zoomTo, reset, rotate, trapFocus]);

  // 滚轮缩放（需 non-passive，React 的 onWheel 在部分浏览器为 passive，无法 preventDefault）
  useEffect(() => {
    if (!open) return;
    const node = stageRef.current;
    if (!node) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      zoom(event.deltaY < 0 ? 'in' : 'out');
    };
    node.addEventListener('wheel', onWheel, { passive: false });
    return () => node.removeEventListener('wheel', onWheel);
  }, [open, zoom]);

  const onPointerDown = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>) => {
      if (event.button !== 0 || !pannable) return;
      dragRef.current = {
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        origin: offset,
      };
      setDragging(true);
      event.currentTarget.setPointerCapture?.(event.pointerId);
    },
    [offset, pannable]
  );

  const onPointerMove = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>) => {
      const drag = dragRef.current;
      if (!drag || drag.pointerId !== event.pointerId) return;
      setOffset(
        clampImageOffset(
          {
            x: drag.origin.x + (event.clientX - drag.startX),
            y: drag.origin.y + (event.clientY - drag.startY),
          },
          contentSize,
          stage
        )
      );
    },
    [contentSize, stage]
  );

  const endPointerDrag = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    setDragging(false);
    if (event.currentTarget.hasPointerCapture?.(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  }, []);

  /** 双击：适应窗口 ↔ 100% 之间切换（小图看细节、大图看全貌的最短路径） */
  const handleDoubleClick = useCallback(() => {
    if (fitMode) zoomTo(1);
    else setFitMode(true);
  }, [fitMode, zoomTo]);

  if (!open || !current) return null;

  const downloadTarget = downloadName(current);
  const cursor = pannable ? (dragging ? 'grabbing' : 'grab') : 'default';

  return createPortal(
    <div
      className="rte-image-viewer"
      role="dialog"
      aria-modal="true"
      aria-label={current.alt ? `图片预览：${current.alt}` : '图片预览'}
      ref={dialogRef}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) close();
      }}
    >
      <div className="rte-image-viewer__toolbar" role="toolbar" aria-label="图片操作">
        {images.length > 1 ? (
          <>
            <ViewerButton label="上一张" shortcut="←" onClick={() => showImage(activeIndex - 1)}>
              <ChevronLeft size={18} />
            </ViewerButton>
            <span className="rte-image-viewer__counter" aria-live="polite">
              {activeIndex + 1} / {images.length}
            </span>
            <ViewerButton label="下一张" shortcut="→" onClick={() => showImage(activeIndex + 1)}>
              <ChevronRight size={18} />
            </ViewerButton>
            <span className="rte-image-viewer__divider" aria-hidden="true" />
          </>
        ) : null}
        <ViewerButton label="缩小" shortcut="-" onClick={() => zoom('out')}>
          <ZoomOut size={18} />
        </ViewerButton>
        <button
          type="button"
          className="rte-image-viewer__scale"
          title="缩放到 100%"
          aria-label={`当前缩放 ${formatScalePercent(scale)}，点击恢复 100%`}
          aria-live="polite"
          onClick={() => zoomTo(1)}
        >
          {formatScalePercent(scale)}
        </button>
        <ViewerButton label="放大" shortcut="+" onClick={() => zoom('in')}>
          <ZoomIn size={18} />
        </ViewerButton>
        <ViewerButton label="原始尺寸" shortcut="1" active={!fitMode && Math.abs(scale - 1) < 0.001} onClick={() => zoomTo(1)}>
          <Scaling size={18} />
        </ViewerButton>
        <ViewerButton label="适应窗口" shortcut="F" active={fitMode} onClick={() => setFitMode(true)}>
          <Expand size={18} />
        </ViewerButton>
        <span className="rte-image-viewer__divider" aria-hidden="true" />
        <ViewerButton label="向左旋转" shortcut="Shift+R" onClick={() => rotate(-IMAGE_ROTATION_STEP)}>
          <RotateCcw size={18} />
        </ViewerButton>
        <ViewerButton label="向右旋转" shortcut="R" onClick={() => rotate(IMAGE_ROTATION_STEP)}>
          <RotateCw size={18} />
        </ViewerButton>
        <ViewerButton label="水平翻转" active={flipX} onClick={() => setFlipX((prev) => !prev)}>
          <FlipHorizontal size={18} />
        </ViewerButton>
        <ViewerButton
          label="垂直翻转"
          active={flipY}
          onClick={() => setFlipY((prev) => !prev)}
        >
          <FlipVertical size={18} />
        </ViewerButton>
        <span className="rte-image-viewer__divider" aria-hidden="true" />
        <ViewerButton label="重置" shortcut="0" onClick={reset}>
          <Undo2 size={18} />
        </ViewerButton>
        <a
          className="rte-image-viewer__btn"
          href={current.src}
          download={downloadTarget}
          target="_blank"
          rel="noreferrer"
          title="下载原图"
          aria-label="下载原图"
        >
          <Download size={18} />
        </a>
        <span className="rte-image-viewer__divider" aria-hidden="true" />
        <ViewerButton label="关闭" shortcut="Esc" autofocus onClick={close}>
          <X size={18} />
        </ViewerButton>
      </div>

      <div
        className="rte-image-viewer__stage"
        ref={stageRef}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endPointerDrag}
        onPointerCancel={endPointerDrag}
        onDoubleClick={handleDoubleClick}
      >
        <img
          key={`${activeIndex}-${current.src}`}
          className="rte-image-viewer__image"
          src={current.src}
          alt={current.alt}
          draggable={false}
          onLoad={(event) => {
            const el = event.currentTarget;
            if (el.naturalWidth && el.naturalHeight) {
              setNatural({ width: el.naturalWidth, height: el.naturalHeight });
            }
          }}
          style={{
            width: natural.width > 0 ? `${natural.width}px` : undefined,
            height: natural.height > 0 ? `${natural.height}px` : undefined,
            maxWidth: natural.width > 0 ? 'none' : '90vw',
            maxHeight: natural.height > 0 ? 'none' : '85vh',
            transform: `translate3d(${offset.x}px, ${offset.y}px, 0) scale(${scale}) rotate(${rotation}deg) scaleX(${
              flipX ? -1 : 1
            }) scaleY(${flipY ? -1 : 1})`,
            transition: dragging ? 'none' : 'transform 150ms ease-out',
            cursor,
          }}
        />
      </div>

      <p className="rte-image-viewer__hint">
        滚轮 / 双击缩放 · 放大后拖动平移 · {images.length > 1 ? '← → 切换图片 · ' : ''}R 旋转 · Esc 关闭
      </p>
    </div>,
    document.body
  );
}

interface ViewerButtonProps {
  label: string;
  shortcut?: string;
  onClick: () => void;
  active?: boolean;
  autofocus?: boolean;
  children: ReactNode;
}

/** 工具栏按钮：统一 title / aria 文案，减少重复 */
function ViewerButton({ label, shortcut, onClick, active, autofocus, children }: ViewerButtonProps) {
  return (
    <button
      type="button"
      className={`rte-image-viewer__btn${active ? ' rte-image-viewer__btn--active' : ''}`}
      title={shortcut ? `${label}（${shortcut}）` : label}
      aria-label={label}
      aria-pressed={active}
      data-viewer-autofocus={autofocus ? 'true' : undefined}
      onClick={onClick}
    >
      {children}
    </button>
  );
}
