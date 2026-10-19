/**
 * I2-02 合规：文本下载工具测试（会话导出落盘链路）。
 */
import { downloadTextFile } from '../download-text-file';

describe('downloadTextFile（I2-02）', () => {
  const originalCreate = URL.createObjectURL;
  const originalRevoke = URL.revokeObjectURL;

  afterEach(() => {
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: originalCreate });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: originalRevoke });
  });

  it('支持 Blob URL 时创建 <a download> 并点击，随后 revoke', () => {
    const createObjectURL = jest.fn(() => 'blob:download-mock');
    const revokeObjectURL = jest.fn();
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL });

    const clickSpy = jest
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined);

    const ok = downloadTextFile('ai-conversation-5.md', '# 会话');
    expect(ok).toBe(true);
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(clickSpy).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:download-mock');

    clickSpy.mockRestore();
  });

  it('缺少 Blob URL API（受限环境）时返回 false 且不抛错', () => {
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: undefined });

    expect(() => downloadTextFile('x.md', 'content')).not.toThrow();
    expect(downloadTextFile('x.md', 'content')).toBe(false);
  });
});