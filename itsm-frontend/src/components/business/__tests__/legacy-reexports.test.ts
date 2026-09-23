/**
 * FE-2 目录下沉的兼容性守卫。
 *
 * 断言 `components/business/*`（旧路径）的 re-export 与
 * `components/common/rich-text|attachment/*`（新路径）指向**同一实现**，
 * 保证一个发布周期内的旧 import 不失效；类型探针在编译期校验新旧类型同源。
 *
 * 组件行为用例已随实现迁移至 `components/common/rich-text/__tests__/`，
 * 本文件只做兼容层断言，请勿在此重复覆盖行为。
 */
import LegacyAttachmentField, {
  formatAttachmentSize as legacyFormatAttachmentSize,
  validateAttachmentFile as legacyValidateAttachmentFile,
} from '@/components/business/AttachmentField';
import LegacyRichTextEditor from '@/components/business/RichTextEditor';
import LegacyRichTextEditorImageMenu from '@/components/business/RichTextEditorImageMenu';
import LegacyRichTextEditorResizableImage from '@/components/business/RichTextEditorResizableImage';
import LegacyRichTextImageViewer from '@/components/business/RichTextImageViewer';

import NewAttachmentField, {
  formatAttachmentSize as newFormatAttachmentSize,
  validateAttachmentFile as newValidateAttachmentFile,
} from '@/components/common/attachment/AttachmentField';
import NewRichTextEditor from '@/components/common/rich-text/RichTextEditor';
import NewRichTextEditorImageMenu from '@/components/common/rich-text/RichTextEditorImageMenu';
import NewRichTextEditorResizableImage from '@/components/common/rich-text/RichTextEditorResizableImage';
import NewRichTextImageViewer from '@/components/common/rich-text/RichTextImageViewer';

import type { AttachmentFieldProps as LegacyAttachmentFieldProps } from '@/components/business/AttachmentField';
import type { UploadedImage as LegacyUploadedImage } from '@/components/business/RichTextEditor';
import type { RichTextEditorImageMenuProps as LegacyImageMenuProps } from '@/components/business/RichTextEditorImageMenu';
import type { RichTextImageViewerProps as LegacyViewerProps } from '@/components/business/RichTextImageViewer';
import type { AttachmentFieldProps } from '@/components/common/attachment/AttachmentField';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';
import type { RichTextEditorImageMenuProps } from '@/components/common/rich-text/RichTextEditorImageMenu';
import type { RichTextImageViewerProps } from '@/components/common/rich-text/RichTextImageViewer';

// `@ant-design/icons` 的 CJS 入口会拉起 `@ant-design/colors` 的 ESM 产物，
// jest 的 transformIgnorePatterns 不转译 node_modules，故与 RichTextEditor 用例保持同样的代理 mock。
jest.mock('@ant-design/icons', () => {
  const ReactLib = require('react');
  return new Proxy(
    {},
    {
      get: (_target: object, prop: string) =>
        function MockIcon() {
          return ReactLib.createElement('span', { 'data-icon': prop });
        },
    }
  );
});

/**
 * 编译期同源探针：入参两个方向的映射函数均需成立，等价于「新旧类型可互相赋值」。
 * 若旧路径漏导出类型、或新旧定义漂移，`npx tsc --noEmit -p tsconfig.json` 会直接失败。
 */
const assertSameType = <A, B>(_forward: (value: A) => B, _backward: (value: B) => A): void => undefined;

assertSameType<LegacyUploadedImage, UploadedImage>(
  (value) => value,
  (value) => value,
);
assertSameType<LegacyAttachmentFieldProps, AttachmentFieldProps>(
  (value) => value,
  (value) => value,
);
assertSameType<LegacyImageMenuProps, RichTextEditorImageMenuProps>(
  (value) => value,
  (value) => value,
);
assertSameType<LegacyViewerProps, RichTextImageViewerProps>(
  (value) => value,
  (value) => value,
);

describe('FE-2 旧路径 re-export 兼容层', () => {
  it('RichText 组件族：旧路径与新路径指向同一实现', () => {
    expect(LegacyRichTextEditor).toBe(NewRichTextEditor);
    expect(LegacyRichTextEditorImageMenu).toBe(NewRichTextEditorImageMenu);
    expect(LegacyRichTextEditorResizableImage).toBe(NewRichTextEditorResizableImage);
    expect(LegacyRichTextImageViewer).toBe(NewRichTextImageViewer);
  });

  it('AttachmentField：默认导出与具名导出均与新路径同源', () => {
    expect(LegacyAttachmentField).toBe(NewAttachmentField);
    expect(legacyValidateAttachmentFile).toBe(newValidateAttachmentFile);
    expect(legacyFormatAttachmentSize).toBe(newFormatAttachmentSize);
  });

  it('经旧路径消费的校验函数与直接引用新路径结果一致', () => {
    const options = { maxSizeMB: 1 };
    const okFile = new File([new Uint8Array(1024)], 'ok.pdf', { type: 'application/pdf' });
    const oversizeFile = new File([new Uint8Array(2 * 1024 * 1024)], 'big.pdf', { type: 'application/pdf' });

    expect(legacyValidateAttachmentFile(okFile, options)).toEqual({ ok: true });
    expect(legacyValidateAttachmentFile(oversizeFile, options)).toEqual(newValidateAttachmentFile(oversizeFile, options));
    expect(legacyValidateAttachmentFile(oversizeFile, options).ok).toBe(false);
    expect(legacyFormatAttachmentSize(2048)).toBe(newFormatAttachmentSize(2048));
  });
});
