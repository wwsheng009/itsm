# 共享表豁免季度复核（TenantExemptTables）· 2026-09-30

> 状态：**已复核（2026-09-30）**｜下次复核：**2026-12-31**（季度）
> 口径：`itsm-backend/internal/schema/tenant_guard.go` 的 `TenantExemptTables` 单一真相源；复核方法 = ent schema / 迁移 DDL 扫描 + 运行时代码引用检索 + RLS 策略清单（`database/rls/migrations/002_pilot_policies.sql` 及后续）。
> 关联：canon §4.0 共享表决策、`07-known-gaps.md`、IP-P2-3。

---

## 1. 复核结论摘要

| 结论 | 表 | 处置 |
|---|---|---|
| **租户化落地** | `messages` | 加 `tenant_id`（可空过渡）+ 会话回填 + 索引；写入由请求 ctx 派生；移出豁免清单（迁移 `20260930_messages_tenant_id.sql`；巡检 `scripts/msp/verify-messages-tenant-backfill.sql`） |
| **显式共享（保留）** | `marketplace_items`、`prompt_templates` | 全局模板/市场资源，跨租户共享为产品语义；owner/reason/复核期齐备（2026-09-30 刷新） |
| **无需租户列（保留）** | tags 系列、RBAC 关系（`user_roles` 等）、`ai_llm_calls`、knowledge 会话/版本、`msp_allocations`、initialization 系、纯关联表、`password_reset_tokens`、`tenants`、`schema_migrations` | 依据不变（§3），本轮复核确认无新增读写点引入租户语义 |
| **RLS 平台保留（本轮新增，§6）** | `users`、`roles`、`permission_definitions` | 有 `tenant_id` 但**不纳入租户 RLS 策略**；身份/RBAC 平面语义，带补偿控制与复核期 |

## 2. `messages` 租户化决策（IP-P2-3）

- 事实：`messages`（AI 会话消息）无 `tenant_id`，仅经 `conversation_id` 派生；`conversations.tenant_id` 可空。
- 决策：**租户化**。理由：AI 内容属租户数据，RLS / 审计 / 级联删除需要直接租户列，避免仅依赖 join 派生。
- 落地：迁移加列 + 回填 + `(tenant_id, conversation_id, created_at)` 索引；写入点 `handlers/ai/repository_impl.go`（**收尾后：ctx 租户优先；ctx 缺失由会话派生；跨租户冲突/双方缺失 fail-closed**）。
- 巡检（只读）：① 未回填且会话有租户 ② 消息与会话租户错配 ③ 会话自身无租户的历史范围。
- 收尾条件：①=0 且 ②=0，且 ③ 已明确处置 → 单独迁移收紧 `NOT NULL`（**已于 2026-10-03 执行，见下**）。
- **巡检结果留档（2026-10-03，联调库）**：①=0 / ②=0 / ③=0（`scripts/msp/verify-messages-tenant-backfill.sql`）。
- **收尾落地（2026-10-03，v1.46）**：写入侧收口（ctx 优先 → 会话派生 → fail-closed）→ 迁移 `20261003_messages_tenant_not_null.sql`（幂等：再回填 + 残余空值显式报错阻断 + `SET NOT NULL`）+ ent 字段必填（生成物同步）；联调库 `applied=68 pending=0`、`tenant_id nullable=NO`、空值 0 行。

## 3. 保留判定依据（复核不变）

- **tags 系列**：颜色/标签云跨租户共享为合理设计；
- **`tenants` / `schema_migrations`**：递归依赖 / 全局唯一，无法或不应含 `tenant_id`；
- **RBAC 关系（`user_roles` / `role_permissions*`）**：平台级 RBAC，不应被租户过滤拦截（ADR-0001）；
- **`ai_llm_calls`**：LLM 网关级可观测性，与租户无关；
- **knowledge 会话/版本快照**：`session_id` 天然唯一，协作态无租户语义；
- **`msp_allocations`**：MSP 模式下的租户-服务分配，是租户维度之上概念；
- **initialization 系**：`scope_type` / `scope_id` 自带租户维度；
- **纯关联表**（`*_tags`、`*_incidents`、`*_changes` 等）：随宿主表级联，无独立租户语义；
- **`password_reset_tokens`**：按 token 唯一，无租户归属语义。

## 4. 复核流程（固化）

1. **触发**：每季度（next 2026-12-31）或新增共享表 / RLS 策略变更时；
2. **证据**：DDL 扫描（`ent/migrate/schema.go`、`migrations/`）、代码引用检索、策略清单；
3. **输出**：更新 `TenantExemptTables`（owner/reason/ReviewedAt）+ 本页记录 + 必要时迁移；
4. **门禁**：`ApplyGuard` 过期告警（>180 天）与 CI 用例（`TestTenantExemptTables_*`）。

## 5. 变更记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-09-30 | 首轮季度复核：`messages` 租户化；`marketplace_items` / `prompt_templates` 保留显式共享并刷新复核期 |
| v1.1 | 2026-10-04 | 新增 §6「RLS 平台保留项」：`users` / `roles` / `permission_definitions` 定案不纳入租户 RLS 策略（RLS 批次 8 收口；补偿控制与 2026-12-31 复核期见 §6） |

---

## 6. RLS 平台保留项（含 `tenant_id`，不纳入租户策略）· 2026-10-04 定案

> 范围说明：§1–§4 面向无 `tenant_id` 的 `TenantExemptTables` 豁免；本节处理**有 `tenant_id` 但语义上不应套用「`tenant_id = get_current_tenant_id()`」单租户策略**的表（RLS 批次 8 收口结论，单一治理留档）。
> 背景：RLS 策略扩展 001–010 已纳管 **150/153** 张 `tenant_id` 表；以下 3 张为终局豁免（150 纳管 + 3 豁免 = 153 全覆盖决策）。

| 表 | 语义 | 不纳管理由（2026-10-04 复核） | 补偿控制 | 复核期 / 后续 |
|---|---|---|---|---|
| `users` | 身份根表（`tenant_id` = home 租户） | ① 预认证路径（登录/注册/找回/刷新）天然跨租户查询用户名/邮箱；② 切换租户后按 `user_id` 读取自身身份（RBAC/MSP/GetMe）与 `tenant_id` 过滤**不等价**，单租户策略会阻断合法身份读取；③ 租户守卫（tenant guard）与 CLI 引导依赖平台范围查询 | 关系表 `user_tenant_memberships`、`user_tenant_membership_orgs` 已 RLS；应用层读写均带租户过滤或自有身份（`userID` 来自签名 token）；登录/切换/建号路径保持审计留痕 | 2026-12-31 前评估 `user-scope GUC`（`app.current_user_id`）策略可行性；若落地需覆盖身份读取路径的 GUC 注入 |
| `roles` | 平台 RBAC 词表 | ADR-0001 / `TenantExemptTables`「RBAC 关系为平台级」同源语义；角色解析跨 home/target 作用域（membership → `role_id` → `role_permissions`），单租户策略语义不成立 | `role_permissions`（RLS 批次 5）、memberships（RLS 批次 1）均已纳管；权限判定 fail-closed（无 membership → 空集） | 2026-12-31 |
| `permission_definitions` | 平台权限字典 | `tenant_id` 全 0/NULL（平台词表）；当前**无运行时代码读取**（权限清单真源为 `permissions` + `role_permissions`，均已 RLS） | 平台级只读字典；若未来启用读取须显式 `SystemContext` 并回写本页 | 2026-12-31（若仍无读者，评估直接归档/下线） |

**长期路线**：若身份面需要 DB 级隔离，方案是引入第二 GUC（`app.current_user_id`）与 `users` 专属策略 `tenant_id = get_current_tenant_id() OR id = get_current_user_id()`，并逐一改造身份读取路径；在完成前维持本节豁免与补偿控制。
