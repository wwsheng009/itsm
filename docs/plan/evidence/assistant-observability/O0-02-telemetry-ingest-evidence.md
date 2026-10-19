# O0-02 后端埋点接入与聚合 · 交付证据

- 日期：2026-10-18
- 任务：O0-02（观测性与分析 O 系列，方案 `docs/plan/ai-assistant-dock-plan-2026-10-18.md` §13.2/§13.7）
- 状态：`integration_verified`（真实 PG 迁移 + RLS 隔离实测；条件项见 §5）
- 变更文件：
  - `itsm-backend/migrations/20261018_ai_telemetry_events.sql`（新增表 + 3 索引 + RLS 策略）
  - `itsm-backend/service/ai_assistant_telemetry.go`（服务/仓储：批量写入 + 聚合）
  - `itsm-backend/service/ai_assistant_telemetry_test.go`（6 例）
  - `itsm-backend/handlers/ai/telemetry.go`（接入/聚合 handler + 白名单 + 载荷护栏）
  - `itsm-backend/handlers/ai/telemetry_handler_test.go`（7 例）
  - `itsm-backend/handlers/ai/service.go`（Service 委托 2 方法）
  - `itsm-backend/router/ai_routes.go`（+2 路由，权限复用 `ai:read`）
  - `itsm-backend/middleware/rbac_precheck_gen.go`（authz-gen 重新生成，+2 条目）

## 1. 交付内容

1. **端点**：
   - `POST /api/v1/ai/telemetry/events`：单批 ≤100 条；事件名服务端白名单（28 项，与前端 SDK 逐项对齐）；
     未知事件静默丢弃并计入 `dropped`；载荷 >8KB 降级为 `{truncated:true}`；
     返回 `{accepted, dropped}`；`ts` 未来 >5 分钟回落服务端时钟。
   - `GET /api/v1/ai/telemetry/summary?days=`：总量/去重会话/`error_shown` 计数、
     按事件名与页面 Top50、`latency_ms` avg/p95/max（days 夹取 1..90，缺省 7）。
2. **存储**：`ai_telemetry_events`（tenant_id/user_id/session_id/name/page_key/route/entrypoint/
   conversation_id/run_id/latency_ms/error_code/payload JSONB/occurred_at）+ 3 个租户前置索引 + RLS 策略
   （与 `alerts` 同范式）。
3. **租户边界**：应用层按 `tenant_id` 过滤；数据库层 RLS `USING/WITH CHECK` 兜底。
4. **权限/审计**：路由挂 `ai:read`；RBAC 预检生成物由 `go run ./cmd/authz-gen` 同步（新鲜度测试通过）。

## 2. 单测/回归证据（可复跑）

```powershell
cd itsm-backend
go test ./service/     -run 'AssistantTelemetry' -count=1           # ok 2.084s（6 例）
go test ./handlers/ai/ -run 'Telemetry'          -count=1           # ok 1.920s（7 例）
go test ./middleware/  -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1  # ok
go test ./router/      -count=1                                     # ok 5.473s
go test ./migration/   -run 'FilesystemMigrations' -count=1         # ok（新迁移被自发现）
go test ./internal/bootstrap/ -run 'PostSchemaMigrations' -count=1  # ok
go run ./cmd/migration-lint -migration-sql ./migrations/20261018_ai_telemetry_events.sql  # exit 0
go vet ./service/ ./handlers/ai/ ./router/ ./middleware/            # exit 0
go build ./service/ ./handlers/ai/ ./router/ ./middleware/ ./cmd/authz-gen/  # exit 0
```

> 环境说明：`go build ./...` 在本机 Windows 因提交内存上限在链接 `cmd/cleanup-orphan-notifications`
> 时 OOM（`VirtualAlloc ... errno=1455`，环境资源问题，非代码错误）；改用受控范围构建 + vet + 目标测试覆盖。

## 3. 真实 PostgreSQL 迁移与隔离实测（embedded-postgres 18，一次性实例）

```powershell
cd .tmp/pgverify
node apply-sql.js <repo>/itsm-backend/migrations/20261018_ai_telemetry_events.sql   # SQL_APPLIED
node verify-o002-telemetry.js
```

实测输出（节选）：

```json
{ "table": "ai_telemetry_events", "rlsEnabled": true,
  "policies": ["tenant_isolation_ai_telemetry_events"],
  "indexes": ["ai_telemetry_events_pkey",
              "ai_telemetry_events_tenant_name_occurred_idx",
              "ai_telemetry_events_tenant_page_occurred_idx",
              "ai_telemetry_events_tenant_session_idx"],
  "inserted": 2,
  "aggregate": { "total": 2, "sessions": 1, "avg": 120, "p95": 120 },
  "rls": { "tenant1VisibleRows": 2, "tenant2VisibleRows": 0,
           "crossTenantInsertErrorCode": "42501", "sameTenantInsert": "ALLOWED" } }
```

结论：迁移可在真实 PG 干净执行；RLS 读隔离（租户 2 看不到租户 1 数据）与写隔离
（跨租户 INSERT → 42501，同租户放行）均成立；聚合口径（total/sessions/avg/p95）可执行。

## 4. 口径冻结

`/ai/telemetry/summary` 即看板（O0-03）与试点对照报告（§11）的聚合口径来源：
采用/质量/性能子集 = 事件总量、去重会话、`error_shown` 率、按事件名/页面分布、时延分位。

## 5. 条件项与遗留

- **O0-03 看板未建**：前端管理页 `/ai/observability` 归 O0-03，直接消费本端点；
- **前端接线未做**：AIChat/Dock 尚未调用 SDK（归 D0-05 / V 系列试点）——接线后即可开始基线采集；
- **保留期清理**：表注释已标注，定时清理任务待补（O 系列后续）；
- **全栈 E2E**：浏览器 → 后端 → PG 的真实链路未在本机跑通（需完整服务+登录态），归 O0-03 联调与 E2E；
- RLS 以表 owner 启用（与 `alerts` 同范式）；本证据用**非超级用户** `app_user` 证明策略对非 owner 生效。