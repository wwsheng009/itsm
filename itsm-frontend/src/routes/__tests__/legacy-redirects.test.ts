import { describe, expect, it } from '@jest/globals';
import { legacyRedirectTarget } from '../legacy-redirects';

describe('legacy redirects', () => {
  it('把邮件报障下的主数据旧入口归位到 /master-data', () => {
    expect(legacyRedirectTarget('/email-intake/customers')).toBe('/master-data/customers');
    expect(legacyRedirectTarget('/email-intake/contracts')).toBe('/master-data/contracts');
    expect(legacyRedirectTarget('/email-intake/sources')).toBe('/master-data/sources');
  });

  it('邮件报障会话入口保持不变', () => {
    expect(legacyRedirectTarget('/email-intake/conversations')).toBe('/email-intake');
    expect(legacyRedirectTarget('/email-intake')).toBeNull();
  });
});
