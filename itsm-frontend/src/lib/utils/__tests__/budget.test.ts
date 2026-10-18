import { formatBudget, normalizeBudgetCurrency } from '../budget';

describe('budget 口径（PE-28 / D-63）', () => {
  it('币种归一：trim + 大写；空值 → 空串', () => {
    expect(normalizeBudgetCurrency(' cny ')).toBe('CNY');
    expect(normalizeBudgetCurrency('usd')).toBe('USD');
    expect(normalizeBudgetCurrency('')).toBe('');
    expect(normalizeBudgetCurrency(undefined)).toBe('');
    expect(normalizeBudgetCurrency(null)).toBe('');
  });

  it('展示：金额未设置 → null；币种空 → 仅金额', () => {
    expect(formatBudget(undefined, 'CNY')).toBeNull();
    expect(formatBudget(null, 'CNY')).toBeNull();
    expect(formatBudget(0, 'CNY')).toBeNull();
    expect(formatBudget(-1, 'CNY')).toBeNull();
    expect(formatBudget(120000, '')).toBe('120,000');
    expect(formatBudget(120000, undefined)).toBe('120,000');
  });

  it('展示：金额 + 币种（小写归一为大写）', () => {
    expect(formatBudget(120000, 'CNY')).toBe('120,000 CNY');
    expect(formatBudget(120000, ' cny ')).toBe('120,000 CNY');
  });
});
