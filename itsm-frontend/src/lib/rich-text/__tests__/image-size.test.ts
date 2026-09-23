import {
  IMAGE_ALIGN_ATTR,
  MIN_IMAGE_WIDTH,
  clampImageWidth,
  extractPreservedImageAttrs,
  parseImageAlign,
  parseImageWidth,
  parsePercent,
  percentToImageWidthPx,
  resizeWidthFromDrag,
  resolveImageWidthPx,
} from '../image-size';

describe('rich-text/image-size', () => {
  describe('parseImageWidth', () => {
    it('解析像素数值与 px 后缀', () => {
      expect(parseImageWidth('320')).toBe(320);
      expect(parseImageWidth(' 320 ')).toBe(320);
      expect(parseImageWidth('320px')).toBe(320);
      expect(parseImageWidth('320.4')).toBe(320);
    });

    it('解析百分比（仅接受 0~100）', () => {
      expect(parseImageWidth('50%')).toBe('50%');
      expect(parseImageWidth('33.5%')).toBe('33.5%');
      expect(parseImageWidth('150%')).toBeNull();
      expect(parseImageWidth('0%')).toBeNull();
    });

    it('拒绝空值与非数字', () => {
      expect(parseImageWidth(null)).toBeNull();
      expect(parseImageWidth(undefined)).toBeNull();
      expect(parseImageWidth('')).toBeNull();
      expect(parseImageWidth('abc')).toBeNull();
      expect(parseImageWidth('-10')).toBeNull();
      expect(parseImageWidth('0')).toBeNull();
    });
  });

  describe('parsePercent', () => {
    it('解析合法百分比', () => {
      expect(parsePercent('75%')).toBe(75);
      expect(parsePercent(' 75.5% ')).toBe(75.5);
    });

    it('拒绝越界与非法格式', () => {
      expect(parsePercent('101%')).toBeNull();
      expect(parsePercent('0%')).toBeNull();
      expect(parsePercent('75')).toBeNull();
      expect(parsePercent('%')).toBeNull();
    });
  });

  describe('parseImageAlign', () => {
    it('仅接受 left/center/right（大小写不敏感）', () => {
      expect(parseImageAlign('left')).toBe('left');
      expect(parseImageAlign('CENTER')).toBe('center');
      expect(parseImageAlign(' right ')).toBe('right');
    });

    it('其余取值视为未设置', () => {
      expect(parseImageAlign('evil')).toBeNull();
      expect(parseImageAlign('')).toBeNull();
      expect(parseImageAlign(null)).toBeNull();
    });
    it('属性名常量与存储协议一致', () => {
      expect(IMAGE_ALIGN_ATTR).toBe('data-align');
    });
  });

  describe('clampImageWidth', () => {
    it('下限为 MIN_IMAGE_WIDTH，上限为容器宽度', () => {
      expect(clampImageWidth(10, 600)).toBe(MIN_IMAGE_WIDTH);
      expect(clampImageWidth(900, 600)).toBe(600);
      expect(clampImageWidth(300, 600)).toBe(300);
      expect(clampImageWidth(300.6, 600)).toBe(301);
    });

    it('容器宽度未知（0）时不设上限', () => {
      expect(clampImageWidth(1200, 0)).toBe(1200);
    });
  });

  describe('resolveImageWidthPx', () => {
    it('像素值直接夹取', () => {
      expect(resolveImageWidthPx(400, 600)).toBe(400);
      expect(resolveImageWidthPx(900, 600)).toBe(600);
    });

    it('百分比按容器宽度换算', () => {
      expect(resolveImageWidthPx('50%', 800)).toBe(400);
      expect(resolveImageWidthPx('100%', 800)).toBe(800);
    });

    it('未设置或非法值返回 null', () => {
      expect(resolveImageWidthPx(null, 800)).toBeNull();
      expect(resolveImageWidthPx(undefined, 800)).toBeNull();
      expect(resolveImageWidthPx('abc', 800)).toBeNull();
    });
  });

  describe('percentToImageWidthPx', () => {
    it('按比例换算并夹取上限', () => {
      expect(percentToImageWidthPx(0.25, 800)).toBe(200);
      expect(percentToImageWidthPx(1, 800)).toBe(800);
      expect(percentToImageWidthPx(0, 800)).toBe(MIN_IMAGE_WIDTH);
    });
  });

  describe('resizeWidthFromDrag', () => {
    it('右下/右上手柄向右拖变大，左上/左下向右拖变小', () => {
      const base = { startWidth: 300, containerWidth: 800 };
      expect(resizeWidthFromDrag({ ...base, deltaX: 100, handle: 'se' })).toBe(400);
      expect(resizeWidthFromDrag({ ...base, deltaX: 100, handle: 'ne' })).toBe(400);
      expect(resizeWidthFromDrag({ ...base, deltaX: 100, handle: 'nw' })).toBe(200);
      expect(resizeWidthFromDrag({ ...base, deltaX: 100, handle: 'sw' })).toBe(200);
    });

    it('拖拽结果受最小宽度与容器宽度约束', () => {
      expect(resizeWidthFromDrag({ startWidth: 60, deltaX: -100, handle: 'se', containerWidth: 800 })).toBe(
        MIN_IMAGE_WIDTH
      );
      expect(resizeWidthFromDrag({ startWidth: 700, deltaX: 400, handle: 'se', containerWidth: 800 })).toBe(800);
    });
  });

  describe('extractPreservedImageAttrs', () => {
    it('保留 width/height/data-align/title', () => {
      const tag =
        '<img src="blob:x" alt="a.png" width="320" height="180" data-align="center" title="截图" />';
      const preserved = extractPreservedImageAttrs(tag);
      expect(preserved).toContain('width="320"');
      expect(preserved).toContain('height="180"');
      expect(preserved).toContain('data-align="center"');
      expect(preserved).toContain('title="截图"');
    });

    it('无表现属性时返回空串，不保留 src/alt/data-attachment-id', () => {
      const preserved = extractPreservedImageAttrs(
        '<img src="/api/v1/attachments/1/content" alt="图" data-attachment-id="1" />'
      );
      expect(preserved).toBe('');
    });

    it('忽略空值属性', () => {
      expect(extractPreservedImageAttrs('<img src="x" width="" title=" " />')).toBe('');
    });
  });
});
