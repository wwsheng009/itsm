/**
 * I2-02 合规：文本文件下载（会话导出等）。
 *
 * 边界：jsdom / 受限环境（隐私模式、无 Blob URL 支持）下静默跳过下载——
 * 数据获取已在调用方完成，下载失败不阻断会话（返回 false 供调用方提示）。
 */
export function downloadTextFile(
  filename: string,
  content: string,
  mime = 'text/markdown;charset=utf-8'
): boolean {
  if (typeof window === 'undefined' || typeof document === 'undefined') return false;
  if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return false;
  try {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    anchor.rel = 'noopener';
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(url);
    return true;
  } catch {
    return false;
  }
}