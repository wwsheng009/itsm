import { readSidebarPreference } from '../sidebar-preference';

describe('readSidebarPreference', () => {
  it('识别收起的显式参数', () => {
    expect(readSidebarPreference('?sidebar=collapsed')).toBe(true);
    expect(readSidebarPreference('?sidebar=1')).toBe(true);
    expect(readSidebarPreference('?sidebar=hidden')).toBe(true);
  });

  it('识别展开的显式参数', () => {
    expect(readSidebarPreference('?sidebar=expanded')).toBe(false);
    expect(readSidebarPreference('?sidebar=0')).toBe(false);
    expect(readSidebarPreference('?sidebar=open')).toBe(false);
  });

  it('缺省、空值或未知值不改变现有行为', () => {
    expect(readSidebarPreference('')).toBeNull();
    expect(readSidebarPreference('?foo=bar')).toBeNull();
    expect(readSidebarPreference('?sidebar=')).toBeNull();
    expect(readSidebarPreference('?sidebar=auto')).toBeNull();
  });

  it('忽略大小写与首尾空白，并保留其他查询参数', () => {
    expect(readSidebarPreference('?view=board&sidebar=%20COLLAPSED%20')).toBe(true);
  });
});
