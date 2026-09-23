import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  getStagedImageId,
  hasStagedImages,
  replaceStagedImages,
  stripStagedImages,
} from '../staged-images';

describe('rich-text/staged-images', () => {
  const stagedTag =
    '<img src="blob:http://localhost/a" alt="a.png" data-attachment-id="staged-1" width="320" data-align="center" />';

  it('识别并提取暂存图片 id', () => {
    expect(getStagedImageId(stagedTag)).toBe('staged-1');
    expect(getStagedImageId('<img src="x" data-attachment-id="9" />')).toBeNull();
    expect(extractStagedImageIds(`<p>x</p>${stagedTag}<img src="y" data-attachment-id="staged-2">`)).toEqual([
      'staged-1',
      'staged-2',
    ]);
    expect(hasStagedImages(stagedTag)).toBe(true);
    expect(hasStagedImages('<img src="y" data-attachment-id="9">')).toBe(false);
  });

  it('替换暂存图片时保留缩放尺寸与对齐属性', () => {
    const out = replaceStagedImages(`<p>图：</p>${stagedTag}`, {
      'staged-1': { id: 42, url: '/api/v1/attachments/42/content', name: 'a.png' },
    });

    expect(out).toContain('src="/api/v1/attachments/42/content"');
    expect(out).toContain('data-attachment-id="42"');
    expect(out).toContain('width="320"');
    expect(out).toContain('data-align="center"');
    expect(out).not.toContain('blob:');
    expect(out).not.toContain(STAGED_ID_PREFIX);
  });

  it('上传失败的暂存图片整标签移除，不写 blob 地址', () => {
    const out = replaceStagedImages(stagedTag, {});
    expect(out).toBe('');
  });

  it('非暂存图片原样保留（含尺寸属性）', () => {
    const normal = '<img src="/api/v1/attachments/7/content" width="200" data-align="right" />';
    expect(replaceStagedImages(normal, {})).toBe(normal);
    expect(stripStagedImages(normal)).toBe(normal);
  });

  it('stripStagedImages 仅移除暂存图片', () => {
    const out = stripStagedImages(`<p>x</p>${stagedTag}<img src="/api/v1/attachments/7/content" width="200">`);
    expect(out).not.toContain('blob:');
    expect(out).toContain('<img src="/api/v1/attachments/7/content" width="200">');
  });
});
