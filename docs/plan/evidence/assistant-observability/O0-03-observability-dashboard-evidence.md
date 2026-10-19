# O0-03 观测看板 MVP · 交付证据

- 日期：2026-10-18
- 任务：O0-03（观测性与分析 O 系列，方案 `docs/plan/ai-assistant-dock-plan-2026-10-18.md` §13.3/§13.7）
- 状态：`integration_verified`（浏览器级全栈 E2E 归后续联调）
- 变更文件：
  - 后端：`service/ai_assistant_telemetry.go`（+role 维度与过滤）、`service/ai_assistant_telemetry_test.go`（+1 例）
  - 后端：`handlers/ai/telemetry.go`（role 落库 + pageKey/role 过滤）、`handlers/ai/telemetry_handler_test.go`（+1 例）
  - 后端：`handlers/ai/service.go`（委托签名）、`migrations/20261018_ai_telemetry_events_filters.sql`（role 列 + 索引）
  - 后端：`migrations/20261018_add_ai_observability_menu.sql`、`pkg/seeder/seeder.go`（菜单）
  - 前端：`lib/api/ai-api.ts`（summary 客户端）、`pages/(main)/ai/observability/index.tsx`（看板页面）
  - 前端：`routes/index.tsx`、`routes/route-paths.ts`、`lib/i18n/translations.ts`（zh/en）
  - 前端：`pages/(main)/ai/observability/__tests__/index.test.tsx`（5 例）

## 1. 交付内容

1. **看板页面 `/ai/observability`**（方案 §13.3 四层）：
   - 采用：事件总量 / 去重会话 / 助手打开 / 建议点击；
   - 效果：任务发起 / 任务完成 / 完成率；
   - 质量：错误事件 / 错误率 / 确认通过 / 确认拒绝；
   - 成本与时延：平均 / P95 / 最大；
   - **关键转化漏斗**（事件计数：打开 → 发送 → 回答完成 → 任务完成，含较上一步转化率）；
   - 分解表：按事件名 / 按页面；空窗口提示；503/404 → 「服务未启用」降级不报错。
2. **过滤**：`days`（7/14/30/90）、`pageKey`（下拉来自当前窗口页面分布）、`role`（服务端 RBAC 上下文写入，非前端上报）。
3. **后端**：summary 端点新增 `pageKey`/`role` 过滤（空值不参与条件）；`role` 列 + `(tenant_id, role, occurred_at)` 索引；菜单迁移 + seeder。
4. **菜单**：`/ai/observability` 挂 AI 助手子菜单（ai:read，SortOrder 117）。

## 2. 验证证据（可复跑）

```powershell
# 后端
cd itsm-backend
go test ./service/ -run 'AssistantTelemetry' -count=1        # ok（7 例，含过滤透传）
go test ./handlers/ai/ -run 'Telemetry' -count=1             # ok（8 例，含过滤 handler）
go test ./migration/ -run 'FilesystemMigrations' -count=1    # ok（两迁移被自发现）
go test ./internal/bootstrap/ -run 'PostSchemaMigrations' -count=1
go build ./pkg/seeder/
go run ./cmd/migration-lint -migration-sql ./migrations/20261018_ai_telemetry_events_filters.sql   # exit 0
go run ./cmd/migration-lint -migration-sql ./migrations/20261018_add_ai_observability_menu.sql    # exit 0

# 前端
cd itsm-frontend
npx jest --testPathPattern "observability" --coverage=false --runInBand   # 5 passed
npx jest src/routes/__tests__ --coverage=false --runInBand                # 30 passed（路由门禁无回归）
npx tsc --noEmit                                                          # 0 错误
npx eslint "src/pages/(main)/ai/observability/index.tsx" "src/pages/(main)/ai/observability/__tests__/index.test.tsx"
```

## 3. 真实 PostgreSQL 实测（embedded-postgres 18，一次性实例）

**过滤迁移与聚合口径**（`.tmp/pgverify/verify-o003-filters.js`）：

```json
{ "roleColumn": {"column_name": "role", "data_type": "character varying", "is_nullable": "NO"},
  "roleIndexes": ["ai_telemetry_events_tenant_role_occurred_idx"],
  "checks": { "tenant1Unfiltered": 3, "tenant1ByPage": 2, "tenant1ByPageAndRole": 1,
              "tenant1ByRoleCustomer": 1, "tenant2Unfiltered": 1 } }
```

**菜单迁移冒烟**（`.tmp/pgverify/verify-o003-menu-migration.js`，按真实 ent 表结构重建 menus）：

```json
{ "rowCount": 1, "idempotent": true,
  "row": {"name": "AI 观测看板", "path": "/ai/observability", "permission_code": "ai:read",
          "sort_order": "117", "parent_ok": true} }
```

## 4. 冒烟发现（重要）

- **menus 表没有 `(tenant_id, path)` 唯一约束**（ent schema `Menu` 未声明索引）；
  既有 `migrations/20260628_add_connector_menu.sql` 的 `ON CONFLICT (tenant_id, path) DO NOTHING`
  在真实表结构下会报 `no unique or exclusion constraint matching the ON CONFLICT specification`。
- 本任务的菜单迁移已改用 **NOT EXISTS 守卫**（与 `20260824_email_intake_poc.sql` 的既有正确范式一致），
  并用真实 PG 连续执行两次验证幂等；未修改既有已发布迁移（建议后续单独治理：补唯一索引或修正该迁移）。

## 5. 条件项与遗留

- **全栈 E2E**（登录态 → 埋点落库 → 看板可见）未在本机验证，归后续联调；
- 菜单对既有租户需执行迁移后可见；新租户由 seeder 直接装配；
- `role` 由服务端上下文写入，前端伪造无效；但历史数据（迁移前写入）role 为空串，过滤时按空值语义处理；
- 漏斗为事件计数口径（非用户去重），页面标题已显式标注；如需用户级漏斗归 O2 分析增强。