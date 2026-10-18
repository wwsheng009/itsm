// PE-28（D-63）：预算口径归一与展示。
// 权威设计：docs/project-management/plan/p3-write-integrity-design.md。

/**
 * 币种归一：trim + 大写（空值 → 空串 = 租户默认）。
 * 后端对非空币种强校验 ^[A-Z]{2,8}$（2-8 位字母），此处只做与后端一致的归一。
 */
export function normalizeBudgetCurrency(value?: string | null): string {
  return typeof value === 'string' ? value.trim().toUpperCase() : '';
}

/**
 * 预算展示：金额未设置（0/负/空）→ null；币种为空 → 仅金额。
 * 例：formatBudget(120000, 'cny') → '120,000 CNY'。
 */
export function formatBudget(amount?: number | null, currency?: string | null): string | null {
  if (!amount || amount <= 0) return null;
  const cur = normalizeBudgetCurrency(currency);
  return cur ? `${amount.toLocaleString()} ${cur}` : amount.toLocaleString();
}
