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

## 2. `messages` 租户化决策（IP-P2-3）

- 事实：`messages`（AI 会话消息）无 `tenant_id`，仅经 `conversation_id` 派生；`conversations.tenant_id` 可空。
- 决策：**租户化**。理由：AI 内容属租户数据，RLS / 审计 / 级联删除需要直接租户列，避免仅依赖 join 派生。
- 落地：迁移加列 + 回填 + `(tenant_id, conversation_id, created_at)` 索引；写入点 `handlers/ai/repository_impl.go`（ctx 租户优先；无 ctx 保持 NULL 兼容历史）。
- 巡检（只读）：① 未回填且会话有租户 ② 消息与会话租户错配 ③ 会话自身无租户的历史范围。
- 收尾条件：①=0 且 ②=0，且 ③ 已明确处置 → 单独迁移收紧 `NOT NULL`（不在本批）。
- **巡检结果留档（2026-10-03，联调库）**：①=0 / ②=0 / ③=0（`scripts/msp/verify-messages-tenant-backfill.sql`）。收紧 `NOT NULL` 的前置仍未满足：写入侧 `handlers/ai/repository_impl.go` 无 ctx 时保持写 NULL 兼容历史，需先收口（fail-closed/派生）再单独迁移。

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
