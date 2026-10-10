# 存量租户权限目录处置（D1）决策材料

> **状态**：v0.2（2026-10-10）——**修订：D1=A 执行**（开发环境直接收回）。触发条件 §5-3/§5-4 成立：平台治理家族扩集为 `{system, tenant, mcp}`（治理域收口轮，设计文档 §8）——扩容即意味着 B 的"保留"面从 2 行/租户扩大到 7+ 行/租户，且用户已授权开发环境直接收回。执行记录与复验见 §8。v0.1（2026-10-08）——实测证据回填完成；原建议 D1=B（保留 + 不可分配）定案，A 作为预置预案（触发条件见 §5，不排期）
>
> **上位（权威顺序）**：[平台面权限屏蔽与角色架构设计](./platform-permission-shielding-design.md)（§5.2 P0 / §6.1 D1 / R1）→ [canon](./msp-concept-model-and-architecture-canon.md)（D4=B：平台根 = 类型判定叠加 `tenants.code=default` 兜底）

---

## 1. 结论速览（TL;DR）

1. **实测存量规模小而规则**：10 个非平台根租户各 2 个平台家族码（`system:read`/`system:write`）、各 5 个内置角色共 6 条授权；全库**零通配符**；**非平台根租户零用户实际持有**（仅平台根 t1 有 3 个用户持有，属正当）。
2. **攻击链已三重阻断**：L1（平台面身份守卫）使持码不可穿过平台面（实弹 403）；L2 写面护栏拒绝非根分配 `resource=system`（400）；展示层 `assignable=false` 隐藏（补丁 8）。
3. **残余性质 = 数据卫生 / 审计可读性**，不是风险敞口。
4. **建议：D1=B 定案**（保留 + 不可分配；现状即已实现）。A（裁剪迁移）成本显著更高（须连带改开通克隆链 + 守卫 + 迁移），收益仅为数据洁癖；预置预案保留，触发即执行。

---

## 2. 实测证据（2026-10-08，dev `itsm_prod`，只读 SQL）

采集方式：只读 SQL 直连（`lib/pq`；探针脚本运行后即删，无任何写入）。SQL 见 §7。

| 维度 | 实测 |
|------|------|
| 租户 | 11 个：t1 `default`（`msp_provider`，平台根）；非根 10 个（`msp_customer`/`saas_customer`/`msp_provider`） |
| 目录总量 | 每租户 203–206 行 |
| `resource='system'` 行 | **每租户恰好 2 行**（`system:read`、`system:write`）；11/11 租户全有（含平台根 t1——t1 属正当） |
| 通配符权限（`code LIKE '%*%'`） | **0 行**（全库，含角色授权侧） |
| 角色授权（`role_permissions ⋈ permissions.resource='system'`） | 每租户 5 角色/6 条：`sysadmin`×2（read+write）；`audit_admin`/`it_director`/`ops_director`/`security_admin`×1（read）。非根合计 60 条 |
| 用户持有面（`user_roles`） | 仅 t1：`sysadmin`/`it_director`/`ops_director` 各 1 用户；**非根租户零持有**（`users.role` 主角色全库亦无这 5 码） |
| 平台根识别 | 全库无 `internal`/`standard` 类型租户；t1 按 D4=B 的 `code=default` 兜底判定为平台根 |

> 结论：R1「克隆目录仍含平台码」的存量面=**常数级噪声**（非根 10 租户 × 2 行目录 + 6 条授权），且**无现实持有人**。

---

## 3. 现状防线核对（为什么 B 的残余是卫生问题）

| 层 | 机制（已落地） | 对存量 `system:*` 的效果 |
|----|----------------|--------------------------|
| L1 身份 | `RequirePlatformAdmin` / 平台面路由守卫（补丁 4/6，`router/platform_face_guard_test.go`） | 非平台根身份即使持码也无法穿过平台面（实弹 403 矩阵） |
| L2 能力 | 未分配码不生效；分配护栏（补丁 1/2/4） | 非平台根不可再分配 `resource='system'`（400 `ErrPlatformPermissionForbidden`） |
| 展示 | `assignable=false` 隐藏 + 锁定码警示（补丁 8） | 界面不展示、不进选择矩阵；编辑含锁定码角色时警示 |
| **数据** | —— | **行仍在（本决策对象）**；且新租户经 `ProvisionTenant` 克隆模板（`code=default`）**持续获得**该 2 行 + 授权（`createMissingPermissions` / `cloneRolePermissions`） |

**关键供给事实**：`pkg/seeder/tenant_provisioner.go` 的 `ProvisionTenant` 以产品模板租户（`code=default`）为源全量克隆权限目录与角色授权——**A 若只做一次性数据迁移而不改开通链路，新租户会持续重新引入**（漂移再生）。

---

## 4. 选项对比

| 维度 | A. 裁剪（逐租户删除平台家族码行） | B. 保留但不可分配（现状） |
|------|-----------------------------------|---------------------------|
| 数据动作 | 删 `role_permissions`（引用平台家族码）→ 删 `permissions` 行；**排除平台根 t1** | 无 |
| 开通链路 | **必须连带改**：`createMissingPermissions`/`cloneRolePermissions`（或 `ProvisionTenant`）对非根目标跳过 `platformGovernedPermissionResources`；`applyCustomerScopeMSPRoleBaseline` 核对；`validateTenantClone`/readiness 期望同步（行数 206→204 仍 >0，readiness 不破） | 保留（新租户继续获得，常数级） |
| 守卫 | 需新增「非根租户目录零平台家族码」断言（防再生） | 已有 L1/L2/展示三重 |
| 审计影响 | 实时表不再展示"非根角色持有 system:*"；历史可读性改由审计日志/变更单承担 | 审计可读性保持原样（含噪声） |
| 风险 | 迁移不可逆（`permissions` 无软删）；误删平台根可致平台面能力丢失（**高危**）；跨环境（多部署）需各自执行 | 无功能风险；长期保留常数噪声 |
| 回滚 | 备份表恢复或重跑克隆；跨环境一致性难 | 不适用 |
| 成本 | 迁移脚本 + 供给面改动 + 守卫 + 跨环境执行与复验 | 零 |
| 收益 | 目录/授权洁癖；导出场景不含平台码 | 防线已闭环，风险为零 |

---

## 5. 触发条件（何时从 B 切换到 A）

满足任一即启动 A（预置方案见 §4 列）：

1. 合规/审计明确要求「租户权限目录不得含平台代号」（含导出、报表、开放 API 等任何数据出口）；
2. 新增任何「目录/角色/授权导出」面（当前无）；
3. `platformGovernedPermissionResources` 扩容导致存量噪声扩大；
4. 例行数据卫生窗口（如版本升级迁移批次）且成本已摊销。

---

## 6. 建议与决策登记

- **建议：D1=B 定案**。理由：① 实测零持有面 → 残余纯卫生；② A 的成本横跨「迁移 + 开通链路 + 守卫 + 跨环境复验」，收益不成比例；③ 三重防线已闭环（实弹证据见设计文档 §7）。
- **A 预案保留**（§4），触发即执行；执行时须同步登记本文件的「决策变更」。
- 决策登记（回填设计文档）：§6.1 D1 → **已决（B，2026-10-08）**；§5.2 P0 → **关闭**；R1 → 更新为「已决（B）；触发条件见本文件 §5」。
- **决策变更（2026-10-10）**：D1 → **修订为 A 并执行**（见 §8）；设计文档 §6.1 D1 行、§6.2 R1 行已同步；§5.2 P0 重新打开并随治理域收口轮关闭。

---

## 7. 附录：证据采集 SQL（只读）

```sql
-- [1] 租户清单
SELECT id, code, name, type, status FROM tenants ORDER BY id;

-- [2] 每租户目录总量 / 平台家族行数
SELECT tenant_id, COUNT(*), COUNT(*) FILTER (WHERE resource='system')
FROM permissions GROUP BY tenant_id ORDER BY tenant_id;

-- [3] 通配符类权限（全库）
SELECT tenant_id, code, resource FROM permissions WHERE code LIKE '%*%' ORDER BY tenant_id, code;

-- [4] 角色已授予的平台家族权限
SELECT r.tenant_id, r.code, COUNT(*)
FROM role_permissions rp
JOIN roles r ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE p.resource = 'system'
GROUP BY r.tenant_id, r.code ORDER BY r.tenant_id, r.code;

-- [5] 用户持有面（m2m）
SELECT u.tenant_id, r.code, COUNT(*)
FROM user_roles ur
JOIN users u ON u.id = ur.user_id
JOIN roles r ON r.id = ur.role_id
WHERE r.code IN ('sysadmin','audit_admin','it_director','ops_director','security_admin')
GROUP BY u.tenant_id, r.code ORDER BY u.tenant_id, r.code;
```

> 复验口径（无论 B/A）：非平台根租户目录 `resource='system'` 行数、`role_permissions` 平台家族授权数、`user_roles` 持有面三项均可由上述 SQL 直接输出；A 执行后期望为 `0 / 0 / 0`，B 维持现状 `2 / 6 / 0`（每租户）。

---

## 8. 修订执行记录（2026-10-10，D1=A）

**范围扩展**：原 §2 只统计 `resource='system'`；本轮平台治理家族扩集为 `{system, tenant, mcp}`，收回范围同步扩展（含 `tenant:read/write`、`mcp:read/write/admin`）。

**执行动作**（开发环境 `itsm_prod`，平台根 t1 `code=default` 排除）：

1. 删除非平台根租户 `role_permissions` 中引用平台家族码的授权行；
2. 删除非平台根租户 `permissions` 中平台家族码行（`resource ∈ {system, tenant, mcp}`）；
3. 开通克隆链（`ProvisionTenant` → `createMissingPermissions` / `cloneRolePermissions` / `cloneMenus`）对非平台根目标跳过平台家族（防再生）；
4. 菜单行 `/admin/menus`、`/admin/vector-store` 改挂平台码（`system:write` / `system:read`）；
5. 新增防再生守卫：非平台根克隆产物零平台家族行/授权（`pkg/seeder` 用例）。

**复验（SQL 口径同 §7）**：执行后实测 **目录行 0 / 授权 0**（非平台根全租户合计，范围含 `{system, tenant, mcp}` 三家族）；平台根（`code=default`）保留 7 行；菜单码改挂 24 行（`/admin/menus=system:write`、`/admin/vector-store=system:read`）。实弹与浏览器证据见设计文档 §8.4。
