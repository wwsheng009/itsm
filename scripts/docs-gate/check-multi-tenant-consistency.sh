#!/usr/bin/env bash
#
# scripts/docs-gate/check-multi-tenant-consistency.sh
#
# Gate C.6 — 多租户文档一致性（编号注册表 + 头部四件套）。
#
# 规则（见 docs/multi-tenant/plan/msp-docs-consistency-audit.md §5 T6 与 canon 附录 C）：
#   R1 每份 multi-tenant 文档头部 15 行内必须含：日期 + "状态/定位"声明；
#   R2 canon 必须保留附录 C 与 K1–K5 登记，且不得出现旧 `| G1 |`–`| G5 |` 表行；
#   R3 工作台文档必须使用 WB 系编号，且不得出现旧 R7–R11 / G1–G6 / R1–R6 / A1–A6 条目；
#   R4 07-known-gaps 必须声明 G1–G10 为唯一 G 空间；
#   R5 ADR-004 行动项引用必须带 `ADR-004:` 前缀（一致性审计报告本身豁免）；
#   R6 login / user-flows 必须声明局部前缀（LOGIN- / UF-）；
#   R7 全文不得出现 legacy 角色 token（msp_observer / msp_full，D10 唯一词表）与旧切换事件名（tenant.scope_switch）；
#   R8 target-architecture 不得保留已作废登录契约（availableTenants[] / 409 SCOPE_SELECTION_REQUIRED）；
#   R9 决策注册表必须为 D1–D11，且读侧文档不得出现 D1–D10；
#   R10 实施方案必须声明 "11 个工作流" 且不得出现 "10 个工作流"；
#   R11 实施方案必须包含 07:G8 承接映射（缓存租户维度）；
#   R12 实施方案必须包含 P1 冻结契约（§4.0：组织关联表 / invitations DDL）。
#
# 用法：
#   ./scripts/docs-gate/check-multi-tenant-consistency.sh
#
set -uo pipefail

ROOT_DIR="${DOCS_GATE_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
cd "${ROOT_DIR}"

STRICT="${1:-}"
VIOLATIONS=0
DIR="docs/multi-tenant"

echo "########################################"
echo "# Gate C.6 — 多租户文档一致性"
echo "########################################"

fail() {
  echo "  - $1"
  VIOLATIONS=$((VIOLATIONS + 1))
}

# R1 头部四件套（日期 + 状态/定位）
while IFS= read -r -d '' f; do
  HEAD15="$(head -n 15 "${f}")"
  echo "${HEAD15}" | grep -Eq '20[0-9]{2}-[0-9]{2}-[0-9]{2}' || fail "${f} :: 头部缺少日期"
  echo "${HEAD15}" | grep -Eq '状态|定位' || fail "${f} :: 头部缺少状态/定位声明"
done < <(git ls-files -z "${DIR}/*.md" "${DIR}/plan/*.md")

CANON="${DIR}/plan/msp-concept-model-and-architecture-canon.md"
WB="${DIR}/plan/msp-cross-customer-workbench-and-filter-plan.md"
GAPS="${DIR}/07-known-gaps.md"
LOGIN="${DIR}/plan/msp-login-and-switching-refinement-plan.md"
FLOWS="${DIR}/plan/msp-user-interaction-flows.md"
TARGET="${DIR}/plan/msp-target-architecture.md"
IMPL="${DIR}/plan/msp-implementation-plan.md"
AUDIT="${DIR}/plan/msp-docs-consistency-audit.md"

# R2 canon 注册表
grep -q '附录 C' "${CANON}" || fail "canon 缺少附录 C 注册表"
grep -q 'K1–K5' "${CANON}" || fail "canon 缺少 K1–K5 登记"
if grep -Eq '^\| G[1-5] \|' "${CANON}"; then
  fail "canon 仍存在旧 G1–G5 表行（应为 K1–K5）"
fi

# R3 工作台编号
grep -q 'WB1' "${WB}" || fail "workbench 缺少 WB1–WB6 规则"
if grep -Eq '^\| R(7|8|9|10|11) \|' "${WB}"; then
  fail "workbench 仍存在旧 R7–R11 表行（应为 REV-*）"
fi
if grep -Eq '^\| G[1-6] \|' "${WB}"; then
  fail "workbench 仍存在旧 G1–G6 表行（应为 WB1–WB6）"
fi
if grep -Eq '^\| R[1-6] \|' "${WB}"; then
  fail "workbench 仍存在旧 R1–R6 表行（应为 WB-R1–WB-R6）"
fi
if grep -Eq '^- A[1-6] ' "${WB}"; then
  fail "workbench 仍存在旧 A1–A6 验收项（应为 WB-A1–WB-A6）"
fi

# R4 07 缺口编号空间
grep -q 'G1–G10' "${GAPS}" || fail "07-known-gaps 缺少 G1–G10 唯一 G 空间声明"

# R5 ADR-004 行动项前缀
HITS="$(grep -rn --include='*.md' '行动项' "${DIR}" \
  | grep -v 'msp-docs-consistency-audit.md' \
  | grep -E 'A[0-9]' \
  | grep -v 'ADR-004:A' || true)"
if [ -n "${HITS}" ]; then
  echo "${HITS}" | sed 's/^/    /'
  fail "存在未加 ADR-004: 前缀的行动项引用（见上）"
fi

# R6 局部编号前缀声明
grep -q 'LOGIN-' "${LOGIN}" || fail "login 文档缺少 LOGIN- 局部前缀声明"
grep -q 'UF-01' "${FLOWS}" || fail "user-flows 缺少 UF-01–UF-10 编号声明"

# R7 legacy 角色 token（D10 唯一词表）
LEGACY="$(grep -rn --include='*.md' -E 'msp_observer|msp_full|tenant\.scope_switch' "${DIR}" || true)"
if [ -n "${LEGACY}" ]; then
  echo "${LEGACY}" | sed 's/^/    /'
  fail "存在 legacy 锚点（msp_observer / msp_full / tenant.scope_switch）；唯一口径见 canon D10 与实施方案 §3.0-E"
fi

# R8 已作废登录契约（仅检查 target-architecture 正文）
if grep -Eq 'availableTenants\[|SCOPE_SELECTION_REQUIRED' "${TARGET}"; then
  fail "target-architecture 仍含已作废登录契约（availableTenants[] / 409 SCOPE_SELECTION_REQUIRED）"
fi

# R9 决策注册表
if grep -l 'D1–D10' "${CANON}" "${DIR}/README.md" "${DIR}/INDEX.md" "${AUDIT}" >/dev/null 2>&1; then
  fail "存在过期注册表 D1–D10（应为 D1–D11）"
fi
grep -q 'D1–D11' "${CANON}" || fail "canon 注册表缺少 D1–D11"

# R10 P0 工作流计数
if grep -q '10 个工作流' "${IMPL}"; then
  fail "实施方案仍写 '10 个工作流'（应为 11 个工作流）"
fi
grep -q '11 个工作流' "${IMPL}" || fail "实施方案缺少 '11 个工作流' 声明"

# R11 G8 承接锚点
grep -q '07:G8' "${IMPL}" || fail "实施方案缺少 07:G8（缓存租户维度）承接映射"

# R12 P1 契约冻结锚点
grep -q 'P1 冻结契约' "${IMPL}" || fail "实施方案缺少 P1 冻结契约（§4.0）"
grep -q 'user_tenant_membership_orgs' "${IMPL}" || fail "实施方案缺少 user_tenant_membership_orgs 定义（§4.0-A）"
grep -q 'CREATE TABLE invitations' "${IMPL}" || fail "实施方案缺少 invitations DDL（§4.0-C）"

echo ""
echo "########################################"
echo "# Gate C.6 Summary: ${VIOLATIONS} violation(s)"
echo "########################################"

if [ "${VIOLATIONS}" -gt 0 ]; then
  echo "::error::multi-tenant docs consistency violations detected. See docs/multi-tenant/plan/msp-docs-consistency-audit.md."
  exit 1
fi
exit 0
