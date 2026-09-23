/**
 * 详情页图片查看器（§4.9）纯函数单测。
 *
 * 这些函数决定「适应窗口不放大小图」「缩放上下限」「旋转/翻转后的平移夹取」等
 * 用户可见行为，边界值（0 / NaN / 未加载图片）必须稳定，故单独覆盖。
 */
import {
  IMAGE_ROTATION_STEP,
  IMAGE_SCALE_STEP,
  MAX_IMAGE_SCALE,
  MIN_IMAGE_SCALE,
  clampImageOffset,
  clampImageScale,
  computeFitScale,
  formatScalePercent,
  isQuarterTurn,
  nextImageScale,
  normalizeRotation,
  rotateImage,
} from '../image-viewer';

describe('clampImageScale', () => {
  it('夹取到 [10%, 800%]', () => {
    expect(clampImageScale(0.001)).toBe(MIN_IMAGE_SCALE);
    expect(clampImageScale(100)).toBe(MAX_IMAGE_SCALE);
    expect(clampImageScale(1.5)).toBe(1.5);
  });

  it('非法输入回落到 100%', () => {
    expect(clampImageScale(Number.NaN)).toBe(1);
    expect(clampImageScale(Number.POSITIVE_INFINITY)).toBe(1);
  });
});

describe('nextImageScale', () => {
  it('按 1.25 倍步进放大/缩小', () => {
    expect(nextImageScale(1, 'in')).toBeCloseTo(IMAGE_SCALE_STEP, 4);
    expect(nextImageScale(1, 'out')).toBeCloseTo(1 / IMAGE_SCALE_STEP, 4);
  });

  it('不会越过上下限', () => {
    expect(nextImageScale(MAX_IMAGE_SCALE, 'in')).toBe(MAX_IMAGE_SCALE);
    expect(nextImageScale(MIN_IMAGE_SCALE, 'out')).toBe(MIN_IMAGE_SCALE);
  });

  it('非法入参按 100% 起算，非法步进回落默认步长', () => {
    expect(nextImageScale(Number.NaN, 'in')).toBeCloseTo(IMAGE_SCALE_STEP, 4);
    expect(nextImageScale(1, 'in', 0.5)).toBeCloseTo(IMAGE_SCALE_STEP, 4);
  });
});

describe('normalizeRotation / rotateImage / isQuarterTurn', () => {
  it('角度归一化到 [0, 360)', () => {
    expect(normalizeRotation(-90)).toBe(270);
    expect(normalizeRotation(450)).toBe(90);
    expect(normalizeRotation(Number.NaN)).toBe(0);
  });

  it('正数顺时针、负数逆时针', () => {
    expect(rotateImage(0, IMAGE_ROTATION_STEP)).toBe(90);
    expect(rotateImage(0, -IMAGE_ROTATION_STEP)).toBe(270);
    expect(rotateImage(270, IMAGE_ROTATION_STEP)).toBe(0);
  });

  it('仅 90 / 270 度时宽高互换', () => {
    expect(isQuarterTurn(0)).toBe(false);
    expect(isQuarterTurn(90)).toBe(true);
    expect(isQuarterTurn(180)).toBe(false);
    expect(isQuarterTurn(270)).toBe(true);
    expect(isQuarterTurn(360)).toBe(false);
  });
});

describe('computeFitScale', () => {
  it('大图缩到画布内', () => {
    expect(computeFitScale(2000, 1000, 800, 600)).toBeCloseTo(0.4, 4);
  });

  it('小图默认不放大（适应窗口但保持原始尺寸）', () => {
    expect(computeFitScale(400, 300, 800, 600)).toBe(1);
  });

  it('allowUpscale 时可放大但仍受上限约束', () => {
    expect(computeFitScale(400, 300, 800, 600, { allowUpscale: true })).toBeCloseTo(2, 4);
    expect(computeFitScale(1, 1, 100000, 100000, { allowUpscale: true })).toBe(MAX_IMAGE_SCALE);
  });

  it('尺寸缺失（图片未加载 / jsdom）时回落到 100%', () => {
    expect(computeFitScale(0, 0, 800, 600)).toBe(1);
    expect(computeFitScale(2000, 1000, 0, 0)).toBe(1);
    expect(computeFitScale(Number.NaN, 1000, 800, 600)).toBe(1);
  });
});

describe('formatScalePercent', () => {
  it('四舍五入到整数百分比', () => {
    expect(formatScalePercent(1)).toBe('100%');
    expect(formatScalePercent(0.4)).toBe('40%');
    expect(formatScalePercent(1.253)).toBe('125%');
  });

  it('非法输入显示 100%', () => {
    expect(formatScalePercent(Number.NaN)).toBe('100%');
  });
});

describe('clampImageOffset', () => {
  const box = { width: 800, height: 600 };

  it('内容小于画布时不允许平移（居中显示）', () => {
    expect(clampImageOffset({ x: 100, y: 100 }, { width: 400, height: 300 }, box)).toEqual({ x: 0, y: 0 });
  });

  it('超出画布时夹取到边缘贴合', () => {
    expect(clampImageOffset({ x: 9999, y: -9999 }, { width: 1200, height: 1000 }, box)).toEqual({
      x: 200,
      y: -200,
    });
  });

  it('非法偏移量回落到 0', () => {
    expect(clampImageOffset({ x: Number.NaN, y: Number.NaN }, { width: 1200, height: 1000 }, box)).toEqual({
      x: 0,
      y: 0,
    });
  });
});
