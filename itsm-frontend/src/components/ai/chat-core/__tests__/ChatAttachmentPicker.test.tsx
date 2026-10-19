import React from 'react';
import { App } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import ChatAttachmentPicker from '../ChatAttachmentPicker';
import { aiUploadAttachment } from '@/lib/api/ai-api';

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiUploadAttachment: jest.fn(),
}));

const mockedUpload = aiUploadAttachment as jest.MockedFunction<typeof aiUploadAttachment>;

const renderPicker = (onChange = jest.fn()) =>
  render(
    <App>
      <ChatAttachmentPicker attachments={[]} onChange={onChange} />
    </App>
  );

describe('ChatAttachmentPicker（T5-03b）', () => {
  beforeEach(() => {
    mockedUpload.mockReset();
    mockedUpload.mockResolvedValue({
      id: 11,
      fileName: '截图.png',
      fileSize: 1024,
      mimeType: 'image/png',
      fileUrl: '/api/v1/ai/attachments/11/content',
    });
  });

  it('选择文件后上传并回传附件元数据', async () => {
    const onChange = jest.fn();
    renderPicker(onChange);

    const file = new File(['x'], '截图.png', { type: 'image/png' });
    fireEvent.change(screen.getByTestId('chat-attachment-input'), {
      target: { files: [file] },
    });

    await waitFor(() => expect(mockedUpload).toHaveBeenCalledWith(file));
    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith([
        expect.objectContaining({ id: 11, fileName: '截图.png' }),
      ])
    );
  });

  it('上传失败不产生附件芯片', async () => {
    mockedUpload.mockRejectedValue(new Error('upload failed'));
    const onChange = jest.fn();
    renderPicker(onChange);

    fireEvent.change(screen.getByTestId('chat-attachment-input'), {
      target: { files: [new File(['x'], 'a.png', { type: 'image/png' })] },
    });

    await waitFor(() => expect(mockedUpload).toHaveBeenCalled());
    expect(onChange).not.toHaveBeenCalled();
  });

  it('白名单外文件在前端直接拒绝，不发起上传', async () => {
    const onChange = jest.fn();
    renderPicker(onChange);

    fireEvent.change(screen.getByTestId('chat-attachment-input'), {
      target: { files: [new File(['x'], 'payload.exe', { type: 'application/octet-stream' })] },
    });

    await waitFor(() => expect(screen.getByTestId('chat-attachment-error')).toBeInTheDocument());
    expect(mockedUpload).not.toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalled();
  });
});
