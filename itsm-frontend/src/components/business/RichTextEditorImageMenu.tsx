'use client';

/**
 * RichTextEditorImageMenu —— 图片浮动编辑工具条。
 *
 * 当富文本编辑器内选中图片（NodeSelection）时，在图片上方弹出：
 *  - 尺寸预设（25% / 50% / 75% / 100%，相对编辑区内容宽度）；
 *  - 自定义像素宽度（回车 / 失焦提交，范围夹取到 [MIN_IMAGE_WIDTH, 内容宽度]）；
 *  - 对齐（左 / 居中 / 右，写 data-align）；
 *  - 还原原始尺寸、删除图片。
 *
 * 所有写操作都通过 `editor.chain().focus().updateAttributes('image', …)` 提交，
 * 保证进入 undo 历史、且与 `RichTextEditorResizableImage` 的属性协议一致。
 */

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Divider, InputNumber, Tooltip, Typography } from 'antd';
import {
  AlignCenterOutlined,
  AlignLeftOutlined,
  AlignRightOutlined,
  DeleteOutlined,
  UndoOutlined,
} from '@ant-design/icons';
import { BubbleMenu, useEditorState, type Editor } from '@tiptap/react';

import {
  IMAGE_ALIGN_ATTR,
  IMAGE_SIZE_PRESETS,
  MIN_IMAGE_WIDTH,
  clampImageWidth,
  percentToImageWidthPx,
  resolveImageWidthPx,
  type ImageAlign,
  type ImageWidth,
} from '@/lib/rich-text/image-size';

const { Text } = Typography;

export interface RichTextEditorImageMenuProps {
  editor: Editor;
  disabled?: boolean;
}

export const RichTextEditorImageMenu: React.FC<RichTextEditorImageMenuProps> = ({
  editor,
  disabled = false,
}) => {
  const snapshot = useEditorState({
    editor,
    selector: ({ editor: current }) => {
      const attrs = current.getAttributes('image') as Record<string, unknown>;
      return {
        width: (attrs.width ?? null) as ImageWidth | null,
        align: (attrs[IMAGE_ALIGN_ATTR] ?? null) as ImageAlign | null,
        contentWidth: Math.round(current.view?.dom?.clientWidth || 0),
      };
    },
  });

  const contentWidth = Math.max(MIN_IMAGE_WIDTH, snapshot?.contentWidth ?? 0);
  const widthPx = useMemo(
    () => resolveImageWidthPx(snapshot?.width ?? null, contentWidth),
    [snapshot?.width, contentWidth]
  );

  const [draft, setDraft] = useState<number | null>(widthPx);
  useEffect(() => {
    setDraft(widthPx);
  }, [widthPx]);

  const applyAttributes = useCallback(
    (attrs: Record<string, unknown>) => {
      editor.chain().focus().updateAttributes('image', attrs).run();
    },
    [editor]
  );

  const applyWidth = useCallback(
    (width: ImageWidth | null) => {
      applyAttributes({ width });
    },
    [applyAttributes]
  );

  const commitDraft = useCallback(() => {
    if (draft === null || !Number.isFinite(draft)) return;
    applyWidth(clampImageWidth(draft, contentWidth));
  }, [applyWidth, contentWidth, draft]);

  const toggleAlign = useCallback(
    (align: ImageAlign) => {
      const next = snapshot?.align === align ? null : align;
      applyAttributes({ [IMAGE_ALIGN_ATTR]: next });
    },
    [applyAttributes, snapshot?.align]
  );

  const removeImage = useCallback(() => {
    if (window.confirm('确定删除这张图片吗？')) {
      editor.chain().focus().deleteSelection().run();
    }
  }, [editor]);

  const preventFocusLoss = (event: React.MouseEvent) => event.preventDefault();

  const sizeButton = (key: string, label: string, ratio: number) => (
    <Button
      key={key}
      size="small"
      type="text"
      aria-label={`图片宽度 ${label}`}
      onMouseDown={preventFocusLoss}
      onClick={() => applyWidth(percentToImageWidthPx(ratio, contentWidth))}
    >
      {label}
    </Button>
  );

  const alignButton = (align: ImageAlign, icon: React.ReactNode, title: string) => (
    <Tooltip title={title} key={align}>
      <Button
        size="small"
        type={snapshot?.align === align ? 'primary' : 'text'}
        icon={icon}
        aria-label={title}
        aria-pressed={snapshot?.align === align}
        onMouseDown={preventFocusLoss}
        onClick={() => toggleAlign(align)}
      />
    </Tooltip>
  );

  return (
    <BubbleMenu
      editor={editor}
      pluginKey="rteImageMenu"
      updateDelay={60}
      shouldShow={({ editor: current }) =>
        !disabled && current.isEditable && current.isActive('image')
      }
      tippyOptions={{ placement: 'top', maxWidth: 'none', duration: 100, offset: [0, 10] }}
      className="rich-text-editor__image-menu"
    >
      <div
        className="rich-text-editor__image-menu-inner"
        role="toolbar"
        aria-label="图片编辑工具栏"
        data-testid="rich-text-image-menu"
      >
        {IMAGE_SIZE_PRESETS.map((preset) => sizeButton(preset.key, preset.label, preset.ratio))}

        <Divider orientation="vertical" />

        <InputNumber
          size="small"
          min={MIN_IMAGE_WIDTH}
          max={contentWidth}
          step={10}
          style={{ width: 76 }}
          value={draft}
          aria-label="自定义图片宽度（像素）"
          onChange={(value) => setDraft(typeof value === 'number' ? value : null)}
          onPressEnter={commitDraft}
          onBlur={commitDraft}
        />
        <Text type="secondary" style={{ fontSize: 12 }}>
          px
        </Text>

        <Divider type="vertical" />

        {alignButton('left', <AlignLeftOutlined />, '左对齐')}
        {alignButton('center', <AlignCenterOutlined />, '居中')}
        {alignButton('right', <AlignRightOutlined />, '右对齐')}

        <Divider type="vertical" />

        <Tooltip title="还原为原始尺寸">
          <Button
            size="small"
            type="text"
            icon={<UndoOutlined />}
            aria-label="还原为原始尺寸"
            onMouseDown={preventFocusLoss}
            onClick={() => applyWidth(null)}
          />
        </Tooltip>
        <Tooltip title="删除图片">
          <Button
            size="small"
            type="text"
            danger
            icon={<DeleteOutlined />}
            aria-label="删除图片"
            onMouseDown={preventFocusLoss}
            onClick={removeImage}
          />
        </Tooltip>
      </div>
    </BubbleMenu>
  );
};

export default RichTextEditorImageMenu;
