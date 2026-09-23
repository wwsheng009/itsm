/**
 * FE-3 回归：TicketAttachmentSection 全部附件动作改走 AttachmentApi。
 *
 * 覆盖：
 *   - 列表：AttachmentApi.list({ bizType: 'ticket', bizId })
 *   - 上传：AttachmentApi.uploader() 注入的函数，透传宿主上下文与进度回调（10MB 上限仍拦截）
 *   - 下载 / 预览：优先用 AttachmentRef 自带 URL，缺失时回退工单域内端点（BE-10）
 *   - 删除：AttachmentApi.removeById(id, host)
 */

import React from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { App } from 'antd';
import type { AttachmentRef } from '@/lib/upload/types';
import { TicketAttachmentSection } from '../TicketAttachmentSection';

const mockList = jest.fn();
const mockUpload = jest.fn();
const mockRemoveById = jest.fn();

jest.mock('@/lib/api/attachment-api', () => ({
  // URL 辅助函数保留真实实现：本用例要钉住的正是工单域 URL 形态（BE-10），
  // 用 mock 复刻等于把断言写在了假实现上。
  ...jest.requireActual('@/lib/api/attachment-api'),
  AttachmentApi: {
    list: (...args: unknown[]) => mockList(...args),
    uploader: () => (...args: unknown[]) => mockUpload(...args),
    removeById: (...args: unknown[]) => mockRemoveById(...args),
  },
}));

jest.mock('@/lib/i18n', () => ({
  useI18n: () => ({ t: (key: string) => key, locale: 'zh-CN' }),
}));

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: () => ({ user: { id: 1, name: 'admin' } }),
}));

// jsdom 下真实 Popconfirm 的 portal + loading 组合会让 React 19 的同步 act 挂起，
// 这里退化为一个直接触发 onConfirm 的按钮：被验证的是组件的删除接线（FE-3 改动点），
// 而非 antd 确认弹层自身的动画/portal 行为。
jest.mock('antd', () => {
  const actual = jest.requireActual('antd') as typeof import('antd');
  const ReactModule = jest.requireActual('react') as typeof React;
  return {
    __esModule: true,
    ...actual,
    Popconfirm: ({
      children,
      onConfirm,
      okText,
    }: {
      children?: React.ReactNode;
      onConfirm?: () => void;
      okText?: React.ReactNode;
    }) =>
      ReactModule.createElement(
        ReactModule.Fragment,
        null,
        children,
        ReactModule.createElement(
          'button',
          {
            type: 'button',
            'data-testid': 'popconfirm-ok',
            onClick: () => onConfirm?.(),
          },
          okText
        )
      ),
  };
});

const HOST = { bizType: 'ticket', bizId: 7 };

const REF_DEFAULTS: AttachmentRef = {
  id: 5,
  bizType: 'ticket',
  bizId: 7,
  usage: 'attachment',
  fileName: '需求文档.docx',
  fileSize: 2048,
  mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  fileUrl: '/api/v1/attachments/5/content',
  uploadedBy: 9,
  createdAt: '2026-02-01T10:00:00Z',
};

// 经 Object.assign 合并 Partial 覆盖：直接对象展开会把必填字段推断为 optional，
// 返回类型 AttachmentRef & Partial<AttachmentRef> 可安全赋给 AttachmentRef。
const makeRef = (overrides: Partial<AttachmentRef> = {}): AttachmentRef =>
  Object.assign({}, REF_DEFAULTS, overrides);

function renderSection(
  props: Partial<React.ComponentProps<typeof TicketAttachmentSection>> = {}
) {
  return render(
    <App>
      <TicketAttachmentSection ticketId={7} {...props} />
    </App>
  );
}

/** lucide 图标 class（如 lucide-eye）所在按钮 */
function buttonByIcon(container: HTMLElement, iconClass: string): HTMLButtonElement {
  const icon = container.querySelector(`svg.${iconClass}`);
  if (!icon) throw new Error(`icon not found: ${iconClass}`);
  const button = icon.closest('button');
  if (!button) throw new Error(`button not found for icon: ${iconClass}`);
  return button as HTMLButtonElement;
}

describe('TicketAttachmentSection (FE-3)', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('用工单宿主上下文拉取列表并渲染 AttachmentRef 字段', async () => {
    mockList.mockResolvedValue({ attachments: [makeRef()], total: 1 });

    renderSection();

    await screen.findByText('需求文档.docx');
    expect(mockList).toHaveBeenCalledWith(HOST);
    expect(screen.getByText('附件列表 (1)')).toBeInTheDocument();
    // 旧域内响应的 uploader 昵称经映射保留；缺失时回落 uploadedBy
    expect(screen.getByText('用户 #9')).toBeInTheDocument();
  });

  it('uploader 昵称存在时优先展示昵称（不退化到用户 ID）', async () => {
    mockList.mockResolvedValue({
      attachments: [makeRef({ uploader: { id: 9, name: '张三' } })],
      total: 1,
    });

    renderSection();

    await screen.findByText('需求文档.docx');
    expect(screen.getByText('张三')).toBeInTheDocument();
  });

  it('删除走 removeById(id, host)，成功后重新拉取并回调', async () => {
    const onDeleted = jest.fn();
    mockList.mockResolvedValue({ attachments: [makeRef()], total: 1 });
    mockRemoveById.mockResolvedValue(undefined);

    const { container } = renderSection({ onAttachmentDeleted: onDeleted });
    await screen.findByText('需求文档.docx');

    fireEvent.click(buttonByIcon(container, 'lucide-trash-2'));
    // okText 仍来自 t('common.confirm')，确认按钮由 Popconfirm stub 渲染
    const confirmButton = screen.getByTestId('popconfirm-ok');
    expect(confirmButton).toHaveTextContent('common.confirm');
    fireEvent.click(confirmButton);

    await waitFor(() => expect(mockRemoveById).toHaveBeenCalledWith(5, HOST));
    await waitFor(() => expect(onDeleted).toHaveBeenCalledWith(5));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('非图片预览在缺少 previewUrl 时回退工单域内预览地址', async () => {
    const openSpy = jest.spyOn(window, 'open').mockImplementation(() => null);
    mockList.mockResolvedValue({ attachments: [makeRef({ previewUrl: undefined })], total: 1 });

    const { container } = renderSection();
    await screen.findByText('需求文档.docx');

    fireEvent.click(buttonByIcon(container, 'lucide-eye'));

    expect(openSpy).toHaveBeenCalledWith('/api/v1/tickets/7/attachments/5/preview', '_blank');
  });

  it('下载恒走域内下载端点（旧 fileUrl 的 preview 形态不得用于下载），并带上文件名', async () => {
    const clickSpy = jest.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    const appendSpy = jest.spyOn(document.body, 'appendChild');
    mockList.mockResolvedValue({
      attachments: [
        makeRef({ fileUrl: '/api/v1/tickets/7/attachments/5/preview', previewUrl: undefined }),
      ],
      total: 1,
    });

    const { container } = renderSection();
    await screen.findByText('需求文档.docx');

    fireEvent.click(buttonByIcon(container, 'lucide-download'));

    const anchor = appendSpy.mock.calls
      .map(call => call[0] as HTMLElement)
      .find(node => node.tagName === 'A') as HTMLAnchorElement | undefined;
    expect(anchor).toBeTruthy();
    expect(anchor?.getAttribute('href')).toBe('/api/v1/tickets/7/attachments/5');
    expect(anchor?.getAttribute('download')).toBe('需求文档.docx');
    expect(clickSpy).toHaveBeenCalled();
  });

  it('上传透传宿主上下文与进度回调，成功后回调 onAttachmentUploaded', async () => {
    const onUploaded = jest.fn();
    let resolveUpload: ((ref: AttachmentRef) => void) | undefined;
    mockUpload.mockImplementation(
      () =>
        new Promise<AttachmentRef>(resolve => {
          resolveUpload = resolve;
        })
    );
    mockList.mockResolvedValue({ attachments: [], total: 0 });

    const { container } = renderSection({ onAttachmentUploaded: onUploaded });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['hello'], 'report.pdf', { type: 'application/pdf' });
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => expect(mockUpload).toHaveBeenCalled());
    expect(mockUpload.mock.calls[0][0]).toBe(file);
    expect(mockUpload.mock.calls[0][1]).toEqual(HOST);

    // 进度回调语义：百分比透传并驱动进度条
    const reportProgress = mockUpload.mock.calls[0][2] as (percent: number) => void;
    await act(async () => {
      reportProgress(50);
    });
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '50');

    await act(async () => {
      resolveUpload?.(makeRef({ id: 11 }));
    });
    await waitFor(() => expect(onUploaded).toHaveBeenCalledWith(expect.objectContaining({ id: 11 })));
  });

  it('超过 10MB 的文件被拦截，不会发起上传', async () => {
    mockList.mockResolvedValue({ attachments: [], total: 0 });

    const { container } = renderSection();
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    const bigFile = new File([new ArrayBuffer(11 * 1024 * 1024)], 'big.bin');
    fireEvent.change(input, { target: { files: [bigFile] } });

    await screen.findByText('文件大小不能超过10MB');
    expect(mockUpload).not.toHaveBeenCalled();
  });
});
