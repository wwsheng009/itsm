// 全局 setup 把 dayjs 换成了 DatePicker 桩；本测试验证真实转换语义。
jest.mock('dayjs', () => jest.requireActual('dayjs'));

import dayjs from 'dayjs';
import { dateValue, isoValue } from '../date-value';

describe('date-value', () => {
  it('API 字符串 → DatePicker 可用的 Dayjs', () => {
    const value = dateValue('2026-11-14T00:00:00Z');
    expect(dayjs.isDayjs(value)).toBe(true);
    expect(typeof value?.isValid).toBe('function');
    expect(value?.isValid()).toBe(true);
  });

  it('空值/非法值 → undefined（不把坏值传给 DatePicker）', () => {
    expect(dateValue(undefined)).toBeUndefined();
    expect(dateValue(null)).toBeUndefined();
    expect(dateValue('')).toBeUndefined();
    expect(dateValue('not-a-date')).toBeUndefined();
  });

  it('Dayjs → ISO；空值/非法值 → undefined', () => {
    expect(isoValue(dayjs('2026-11-14T00:00:00Z'))).toBe('2026-11-14T00:00:00.000Z');
    expect(isoValue(undefined)).toBeUndefined();
    expect(isoValue(null)).toBeUndefined();
  });
});
