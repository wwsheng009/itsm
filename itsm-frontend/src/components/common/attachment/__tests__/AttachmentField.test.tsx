/**
 * FE-6 契约用例：公共附件字段壳层 AttachmentField。
 *
 * 覆盖：
 *   - 具名纯函数契约：`validateAttachmentFile` / `formatAttachmentSize` / `uploadAttachmentItems`
 *   - 渲染契约：默认导出、暂存模式（未注入 uploader）vs 即时模式（注入 uploader）、
 *     `maxCount` 上限、`disabled`、i18n key
 *   - 校验拦截：默认 10MB 上限与白名单外扩展名都不入列、不调用 uploader
 *   - 失败交互：失败态 + 失败原因 → 点击重试再次调用 uploader → 转 done；单项失败不阻断其它项
 *   - 即时模式删除：已上传（有 attachmentId）条目走 `onDeleteUploaded`，删除失败时保留条目
 *
 * 测试约定：
 *   - `useI18n` 被替换为「回显 key」的假实现，因此组件渲染出的文案恒等于 i18n key；
 *     纯函数侧的 `translateOrFallback` 在 t 回显 key 时会回落到内置中文兜底文案，
 *     用例据此同时钉住「key 契约」与「无 i18n 上下文时的中文兜底」。
 *   - 文件选择通过 native `input[type=file]` 的 change 事件驱动（与 antd Upload 的
 *     `beforeUpload` 链路一致，见 TicketAttachmentSection 用例的既有做法）。
 */

import React from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { App } from 'antd';
import AttachmentField, {
  formatAttachmentSize,
  uploadAttachmentItems,
  validateAttachmentFile,
  type AttachmentFieldItem,
  type AttachmentUploadResult,
} from '../AttachmentField';
import { DEFAULT_ATTACHMENT_MAX_SIZE_MB } from '@/lib/upload/types';

jest.mock('@/lib/i18n/useI18n', () => ({
  useI18n: () => ({ t: (key: string) => key, language: 'zh-CN' }),
}));

// ---------------------------------------------------------------- helpers

type AttachmentUploaderMock = jest.Mock<
  Promise<AttachmentUploadResult>,
  [File, ((percent: number) => void)?]
>;

const makeUploader = (): AttachmentUploaderMock =>
  jest.fn<Promise<AttachmentUploadResult>, [File, ((percent: number) => void)?]>();

const MB = 1024 * 1024;

const makeFile = (name: string, bytes: number, type?: string): File =>
  new File(bytes > 0 ? [new ArrayBuffer(bytes)] : [], name, type ? { type } : undefined);

const renderField = (props: Partial<React.ComponentProps<typeof AttachmentField>> = {}) =>
  render(
    <App>
      <AttachmentField {...props} />
    </App>
  );

const fileInput = (container: HTMLElement): HTMLInputElement => {
  const input = container.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error('AttachmentField 未渲染原生文件输入');
  return input;
};

const selectFiles = (container: HTMLElement, files: File[]) => {
  fireEvent.change(fileInput(container), { target: { files } });
};

const UPLOADED_ITEM: AttachmentFieldItem = {
  uid: 'att-done',
  name: '既有附件.pdf',
  size: 2048,
  type: 'application/pdf',
  attachmentId: 42,
  url: '/api/v1/attachments/42/content',
  status: 'done',
  progress: 100,
};

const itemOf = (items: AttachmentFieldItem[], uid: string): AttachmentFieldItem => {
  const found = items.find((item) => item.uid === uid);
  if (!found) throw new Error(`item not emitted: ${uid}`);
  return found;
};

// ------------------------------------------- 具名纯函数契约（无渲染，快）

describe('AttachmentField 具名纯函数契约', () => {
  it('formatAttachmentSize 覆盖 0 B / B / KB / MB 单位与两位小数', () => {
    expect(formatAttachmentSize(0)).toBe('0 B');
    expect(formatAttachmentSize(512)).toBe('512 B');
    expect(formatAttachmentSize(1024)).toBe('1 KB');
    expect(formatAttachmentSize(1536)).toBe('1.5 KB');
    expect(formatAttachmentSize(MB)).toBe('1 MB');
    expect(formatAttachmentSize(2.5 * MB)).toBe('2.5 MB');
  });

  it('validateAttachmentFile 接受白名单扩展名（大小写归一）并返回 ok', () => {
    expect(validateAttachmentFile(makeFile('Report.PDF', 1024))).toEqual({ ok: true });
    expect(validateAttachmentFile(makeFile('photo.webp', 512, 'image/webp'))).toEqual({ ok: true });
    expect(validateAttachmentFile(makeFile('archive.zip', 4096))).toEqual({ ok: true });
  });

  it('validateAttachmentFile 超过默认 10MB 上限返回 code=tooLarge 与可读原因', () => {
    const file = makeFile('big.pdf', DEFAULT_ATTACHMENT_MAX_SIZE_MB * MB + 1);
    const verdict = validateAttachmentFile(file);
    if (verdict.ok) throw new Error('超限文件不应通过校验');

    expect(verdict.code).toBe('tooLarge');
    expect(verdict.reason).toContain('big.pdf');
    expect(verdict.reason).toContain(`${DEFAULT_ATTACHMENT_MAX_SIZE_MB}MB`);

    // 组件 maxSizeMB prop 下沉到同一纯函数：自定义上限同样生效
    const custom = validateAttachmentFile(makeFile('tiny.pdf', 2 * MB), { maxSizeMB: 1 });
    if (custom.ok) throw new Error('超出自定义上限不应通过校验');
    expect(custom.code).toBe('tooLarge');
    expect(custom.reason).toContain('1MB');
  });

  it('validateAttachmentFile 空文件与白名单外扩展名分别返回 emptyFile / typeNotAllowed', () => {
    const empty = validateAttachmentFile(makeFile('empty.txt', 0));
    if (empty.ok) throw new Error('0 字节文件不应通过校验');
    expect(empty.code).toBe('emptyFile');
    expect(empty.reason).toContain('empty.txt');

    const exe = validateAttachmentFile(makeFile('virus.exe', 16));
    if (exe.ok) throw new Error('.exe 不应通过校验');
    expect(exe.code).toBe('typeNotAllowed');
    expect(exe.reason).toContain('virus.exe');

    // 无扩展名同样按白名单拒绝
    const noExt = validateAttachmentFile(makeFile('README', 16));
    if (noExt.ok) throw new Error('无扩展名文件不应通过校验');
    expect(noExt.code).toBe('typeNotAllowed');
  });

  it('validateAttachmentFile 注入 t 时 reason 走 i18n key 而不是中文兜底', () => {
    const t = jest.fn<string, [string, (Record<string, string | number> | undefined)?]>(
      (key) => `i18n:${key}`
    );
    const verdict = validateAttachmentFile(makeFile('big.pdf', 11 * MB), { t });
    if (verdict.ok) throw new Error('超限文件不应通过校验');

    expect(verdict.code).toBe('tooLarge');
    expect(verdict.reason).toBe('i18n:attachment.field.reason.tooLarge');
    expect(t).toHaveBeenCalledWith(
      'attachment.field.reason.tooLarge',
      expect.objectContaining({ name: 'big.pdf', maxSizeMB: DEFAULT_ATTACHMENT_MAX_SIZE_MB })
    );
  });

  it('uploadAttachmentItems 逐项上传：单项失败不阻断后续项，done / 无文件项跳过', async () => {
    const uploader = makeUploader().mockImplementation(async (file: File) => {
      if (file.name === 'bad.pdf') throw new Error('boom');
      return { id: 42, url: '/api/v1/attachments/42/content' };
    });
    const items: AttachmentFieldItem[] = [
      {
        uid: 'a',
        name: 'ok.pdf',
        size: 8,
        type: 'application/pdf',
        file: makeFile('ok.pdf', 8),
        status: 'pending',
        progress: 0,
      },
      {
        uid: 'b',
        name: 'bad.pdf',
        size: 8,
        type: 'application/pdf',
        file: makeFile('bad.pdf', 8),
        status: 'pending',
        progress: 0,
      },
      { uid: 'c', name: 'done.pdf', size: 8, type: 'application/pdf', status: 'done', attachmentId: 7 },
      { uid: 'd', name: 'no-file.pdf', size: 8, type: 'application/pdf', status: 'pending' },
    ];
    const updates: Array<[string, Partial<AttachmentFieldItem>]> = [];

    const result = await uploadAttachmentItems(items, uploader, (uid, patch) => {
      updates.push([uid, patch]);
    });

    // 只对「pending 且有 file」的两项发起上传，顺序执行
    expect(uploader).toHaveBeenCalledTimes(2);
    expect(uploader.mock.calls.map((call) => call[0].name)).toEqual(['ok.pdf', 'bad.pdf']);

    expect(itemOf(result, 'a')).toMatchObject({
      status: 'done',
      attachmentId: 42,
      url: '/api/v1/attachments/42/content',
      progress: 100,
    });
    expect(itemOf(result, 'b').status).toBe('error');
    expect(itemOf(result, 'b').error).toBe('boom');
    expect(itemOf(result, 'c').status).toBe('done');
    expect(itemOf(result, 'd').status).toBe('pending');

    // 返回副本：入参数组不被就地改写
    expect(itemOf(items, 'a').status).toBe('pending');
    expect(updates.map(([uid, patch]) => `${uid}:${patch.status}`)).toEqual([
      'a:uploading',
      'a:done',
      'b:uploading',
      'b:error',
    ]);
  });

  it('uploadAttachmentItems 把 uploader 的进度回调透传为 progress 补丁', async () => {
    const uploader = makeUploader().mockImplementation(
      async (_file: File, onProgress?: (percent: number) => void) => {
        onProgress?.(30);
        onProgress?.(100);
        return { id: 1 };
      }
    );
    const patches: Array<Partial<AttachmentFieldItem>> = [];

    const result = await uploadAttachmentItems(
      [
        {
          uid: 'u1',
          name: 'a.pdf',
          size: 8,
          type: 'application/pdf',
          file: makeFile('a.pdf', 8),
          status: 'pending',
        },
      ],
      uploader,
      (_uid, patch) => {
        patches.push(patch);
      }
    );

    expect(typeof uploader.mock.calls[0][1]).toBe('function');
    expect(patches).toEqual(
      expect.arrayContaining([expect.objectContaining({ progress: 30 })])
    );
    expect(patches).toEqual(
      expect.arrayContaining([expect.objectContaining({ status: 'done', progress: 100 })])
    );
    expect(itemOf(result, 'u1').progress).toBe(100);
  });
});

// ------------------------------------------------------- 渲染契约

describe('AttachmentField 渲染契约', () => {
  it('默认导出可渲染：根容器 / 拖拽区 / 默认标题与提示语全部走 i18n key', () => {
    renderField();

    expect(screen.getByTestId('attachment-field')).toBeInTheDocument();
    expect(screen.getByTestId('attachment-field-dragger')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.title')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.dropText')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.hintDefault')).toBeInTheDocument();
    expect(screen.getByLabelText('attachment.field.draggerAria')).toBeInTheDocument();
    // 无文件时不渲染列表
    expect(screen.queryByTestId('attachment-field-list')).not.toBeInTheDocument();
  });

  it('暂存模式（未注入 uploader）：文件只入列 pending，不发生任何上传', async () => {
    const { container } = renderField();

    selectFiles(container, [makeFile('spec.pdf', 2048, 'application/pdf')]);

    expect(await screen.findByText('spec.pdf')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.status.pending')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.pendingSummary')).toBeInTheDocument();
    // formatAttachmentSize 在组件内同样生效 2048 B → 2 KB
    expect(screen.getByText('2 KB')).toBeInTheDocument();
    expect(screen.queryByText('attachment.field.status.uploading')).not.toBeInTheDocument();
    expect(screen.queryByText('attachment.field.status.done')).not.toBeInTheDocument();
    // 暂存模式不提供重试入口（重试只对即时模式的失败项开放）
    expect(screen.queryByLabelText('attachment.field.retryAria')).not.toBeInTheDocument();
  });

  it('即时模式：选择文件立即调用 uploader，进度透传并在成功后回填 attachmentId/url', async () => {
    let resolveUpload: ((value: AttachmentUploadResult) => void) | undefined;
    const uploader = makeUploader().mockImplementation(
      () =>
        new Promise<AttachmentUploadResult>((resolve) => {
          resolveUpload = resolve;
        })
    );
    const onChange = jest.fn<void, [AttachmentFieldItem[]]>();
    const { container } = renderField({ uploader, onChange });
    const file = makeFile('report.pdf', 4096, 'application/pdf');

    selectFiles(container, [file]);

    await waitFor(() => expect(uploader).toHaveBeenCalledTimes(1));
    expect(uploader.mock.calls[0][0]).toBe(file);
    const reportProgress = uploader.mock.calls[0][1];
    expect(typeof reportProgress).toBe('function');

    await act(async () => {
      reportProgress?.(40);
    });
    expect(screen.getByText('attachment.field.status.uploading')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '40');
    const uploadingCall = onChange.mock.calls[onChange.mock.calls.length - 1];
    expect(itemOf(uploadingCall[0], uploadingCall[0][0].uid)).toMatchObject({
      name: 'report.pdf',
      size: 4096,
      type: 'application/pdf',
      status: 'uploading',
      progress: 40,
    });

    await act(async () => {
      resolveUpload?.({ id: 77, url: '/api/v1/attachments/77/content' });
    });

    await waitFor(() =>
      expect(screen.getByText('attachment.field.status.done')).toBeInTheDocument()
    );
    expect(screen.queryByText('attachment.field.status.uploading')).not.toBeInTheDocument();
    const doneCall = onChange.mock.calls[onChange.mock.calls.length - 1];
    expect(itemOf(doneCall[0], doneCall[0][0].uid)).toMatchObject({
      status: 'done',
      attachmentId: 77,
      url: '/api/v1/attachments/77/content',
      progress: 100,
    });
  });

  it('maxCount 上限：达到上限后继续选择被拒绝（提示 + 不入列 + 不上传）', async () => {
    const uploader = makeUploader().mockResolvedValue({ id: 1 });
    const { container } = renderField({ uploader, maxCount: 1 });

    selectFiles(container, [makeFile('a.pdf', 8, 'application/pdf')]);
    await waitFor(() => expect(uploader).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByText('a.pdf')).toBeInTheDocument());

    selectFiles(container, [makeFile('b.pdf', 8, 'application/pdf')]);

    expect(await screen.findByText('attachment.field.maxCountExceeded')).toBeInTheDocument();
    expect(screen.getAllByTestId(/^attachment-field-item-/)).toHaveLength(1);
    expect(screen.queryByText('b.pdf')).not.toBeInTheDocument();
    expect(uploader).toHaveBeenCalledTimes(1);
    // 实现说明：入口（拖拽区/input）并未因达到 maxCount 而禁用或隐藏，
    // 上限只在 handleFiles 内以 warning 拒绝，这里按真实行为固定。
    expect(fileInput(container)).not.toBeDisabled();
  });

  it('disabled 态：文件入口与删除入口都被禁用，不产生上传与删除', async () => {
    const uploader = makeUploader();
    const onDeleteUploaded = jest.fn();
    renderField({
      uploader,
      disabled: true,
      value: [UPLOADED_ITEM],
      onChange: jest.fn(),
      onDeleteUploaded,
    });

    // antd 把 disabled 落到原生 input 与拖拽区（ant-upload-disabled），用户无法再选文件
    expect(fileInput(document.body)).toBeDisabled();
    const dragArea = document.body.querySelector<HTMLElement>('.ant-upload-drag');
    if (!dragArea) throw new Error('未找到拖拽区');
    expect(dragArea).toHaveClass('ant-upload-disabled');

    // 组件自身的 disabled 短路：删除按钮禁用且点击不会回调
    const removeButton = screen.getByLabelText('attachment.field.removeAria');
    expect(removeButton).toBeDisabled();
    fireEvent.click(removeButton);

    expect(onDeleteUploaded).not.toHaveBeenCalled();
    expect(uploader).not.toHaveBeenCalled();
    expect(screen.getByTestId(`attachment-field-item-${UPLOADED_ITEM.uid}`)).toBeInTheDocument();
  });

  it('超限（默认 10MB）与非白名单扩展名被拒绝：不调用 uploader 且不入列', async () => {
    const uploader = makeUploader();
    const { container } = renderField({ uploader });

    selectFiles(container, [
      makeFile('big.pdf', DEFAULT_ATTACHMENT_MAX_SIZE_MB * MB + 1, 'application/pdf'),
    ]);
    expect(
      await screen.findByText(`「big.pdf」超过 ${DEFAULT_ATTACHMENT_MAX_SIZE_MB}MB 上限`)
    ).toBeInTheDocument();

    selectFiles(container, [makeFile('virus.exe', 16)]);
    expect(await screen.findByText('「virus.exe」类型不在允许范围内')).toBeInTheDocument();

    expect(uploader).not.toHaveBeenCalled();
    expect(screen.queryByTestId('attachment-field-list')).not.toBeInTheDocument();
    expect(screen.queryByTestId(/^attachment-field-item-/)).not.toBeInTheDocument();
  });

  it('上传失败展示失败态与原因，点击重试再次调用 uploader 并转回正常态', async () => {
    const uploader = makeUploader()
      .mockRejectedValueOnce(new Error('网络中断'))
      .mockResolvedValueOnce({ id: 99, url: '/api/v1/attachments/99/content' });
    const { container } = renderField({ uploader });

    selectFiles(container, [makeFile('retry.pdf', 16, 'application/pdf')]);

    await waitFor(() =>
      expect(screen.getByText('attachment.field.status.error')).toBeInTheDocument()
    );
    expect(uploader).toHaveBeenCalledTimes(1);
    expect(screen.getByText('网络中断')).toBeInTheDocument();
    expect(await screen.findByText('attachment.field.uploadFailedItem')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.errorSummary')).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText('attachment.field.retryAria'));

    await waitFor(() =>
      expect(screen.getByText('attachment.field.status.done')).toBeInTheDocument()
    );
    expect(uploader).toHaveBeenCalledTimes(2);
    expect(screen.queryByText('attachment.field.status.error')).not.toBeInTheDocument();
    expect(screen.queryByText('网络中断')).not.toBeInTheDocument();
    expect(screen.queryByText('attachment.field.errorSummary')).not.toBeInTheDocument();
  });

  it('单个文件失败不阻断其它文件：失败项保留重试入口，成功项正常转 done', async () => {
    const uploader = makeUploader().mockImplementation(async (file: File) => {
      if (file.name === 'bad.pdf') throw new Error('bad file');
      return { id: 3, url: '/api/v1/attachments/3/content' };
    });
    const { container } = renderField({ uploader });

    selectFiles(container, [
      makeFile('bad.pdf', 8, 'application/pdf'),
      makeFile('good.pdf', 8, 'application/pdf'),
    ]);

    await waitFor(() => expect(uploader).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(screen.getByText('attachment.field.status.done')).toBeInTheDocument()
    );

    expect(uploader.mock.calls.map((call) => call[0].name)).toEqual(['bad.pdf', 'good.pdf']);
    expect(screen.getAllByTestId(/^attachment-field-item-/)).toHaveLength(2);
    expect(screen.getByText('bad.pdf')).toBeInTheDocument();
    expect(screen.getByText('good.pdf')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.status.error')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.status.done')).toBeInTheDocument();
    expect(screen.getByText('attachment.field.errorSummary')).toBeInTheDocument();
    // 仅失败项提供重试入口，两项都可删除
    expect(screen.getAllByLabelText('attachment.field.retryAria')).toHaveLength(1);
    expect(screen.getAllByLabelText('attachment.field.removeAria')).toHaveLength(2);
  });

  it('即时模式删除：已上传条目调用 onDeleteUploaded，成功后 emit 过滤后的列表', async () => {
    const onDeleteUploaded = jest.fn().mockResolvedValue(undefined);
    const onChange = jest.fn<void, [AttachmentFieldItem[]]>();
    renderField({ value: [UPLOADED_ITEM], onChange, onDeleteUploaded });

    fireEvent.click(screen.getByLabelText('attachment.field.removeAria'));

    await waitFor(() => expect(onDeleteUploaded).toHaveBeenCalledWith(UPLOADED_ITEM));
    // 受控模式下由父级回写 value，这里钉住组件 emit 出去的过滤结果
    await waitFor(() => expect(onChange).toHaveBeenCalledWith([]));
  });

  it('删除失败时保留条目并提示失败原因（不静默移除）', async () => {
    const onDeleteUploaded = jest.fn().mockRejectedValue(new Error('解绑失败'));
    const onChange = jest.fn<void, [AttachmentFieldItem[]]>();
    renderField({ value: [UPLOADED_ITEM], onChange, onDeleteUploaded });

    fireEvent.click(screen.getByLabelText('attachment.field.removeAria'));

    expect(await screen.findByText('attachment.field.deleteFailedItem')).toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByTestId(`attachment-field-item-${UPLOADED_ITEM.uid}`)).toBeInTheDocument();
  });
});
