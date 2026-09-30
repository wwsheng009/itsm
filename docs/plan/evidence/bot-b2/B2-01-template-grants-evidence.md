# B2-01 实施证据（模板/授权模型与管理 API）

> 文档类型：实施证据（任务 B2-01）
> Status: draft
> 编制日期：2026-09-27
> 任务：B2-01（`bot_templates` / `bot_tool_grants` 两表 + 管理 API；依赖 B1-10、BP6）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.3 B2-01、§5.2 AB2-01）
> 核查方式：`go build ./...` + 真实 ent/SQLite 迁移与服务层/路由层测试（ent/schema、service/bot、handlers/ai、router 四包全量）

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B2-01 | **`integration_verified`** | 两表落库（含租户维度唯一约束与索引）、服务层 CRUD/校验/级联/租户隔离、HTTP 管理 API（8 端点）、路由装配门禁（`bot.enabled=false` 时整组不注册）全部就位并测试通过 |
| 权限口径 | ✅ | 复用既有权限码：读 `ai:read`、写 `ai:write`（BD8），不新开权限码；`authz-gen` 已重新生成预检映射（新鲜度测试绿） |
| 兼容默认 | ✅ | 租户无模板时幂等种入内置「默认助手」（`default-assistant`，`ga`/`act_low`/`["chat"]`），与既有聊天行为等价 |
| 遗留 | 2 项 | ①AB2-01 的**运行时消费**（工具面按模板过滤）属 B2-02，不在此判定；②`internal/schema/tenant_guard.go:19` 注释引用的 CI 守卫 `TestTenantGuard_ExemptConsistency` 在仓库中不存在【观察项，非本任务引入】 |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/ent/schema/bot_template.go`（新建） | `bot_templates`：`tenant_id/slug/name/audience/risk_limit/entrypoints_json/system_prompt_ref/status(draft\|pilot\|ga)`；`(tenant_id, slug)` 唯一 + `(tenant_id, status)` 索引；edge `grants` |
| 2 | `itsm-backend/ent/schema/bot_tool_grant.go`（新建） | `bot_tool_grants`：`tenant_id/bot_id/tool_name/risk_limit/args_policy_json`；`(tenant_id, bot_id, tool_name)` 唯一 + `(tenant_id, tool_name)` 索引；edge `bot`（required） |
| 3 | `itsm-backend/ent/bottemplate/`、`ent/bottoolgrant/`（生成） | `go generate ./ent/` 生成（6 分钟级） |
| 4 | `itsm-backend/service/bot/admin.go`（新建） | `TemplateAdmin`：`ListTemplates`（空租户自动种入默认助手）/`GetTemplate`/`CreateTemplate`/`UpdateTemplate`/`DeleteTemplate`（**事务内先删授权再删模板**）/`ListGrants`/`UpsertGrant`/`DeleteGrant`/`SeedDefaultTemplate`；稳定错误 `ErrTemplateNotFound`/`ErrGrantNotFound`/`ErrTemplateSlugUsed`/`ErrValidation`；`RiskRank` 序 |
| 5 | `itsm-backend/handlers/ai/bot_admin.go`（新建） | 8 端点 handler；错误映射：404（不存在/跨租户）、400（校验/唯一冲突）、401（缺租户上下文）、500（兜底，不回显内部细节） |
| 6 | `itsm-backend/router/bot_routes.go`（新建） | `/api/v1/admin/bots` 读 3 + 写 5；`RequirePermission("ai","read"/"write")`；handler 为 nil 时整组不注册 |
| 7 | `itsm-backend/router/router.go`、`internal/bootstrap/app.go`（修改） | `RouterConfig.BotAdminHandler` 字段 + 注册块；`cfg.Bot.Enabled` 时装配 handler（默认 false = 零行为变化） |
| 8 | `itsm-backend/middleware/rbac_precheck_gen.go`（再生成） | `go run ./cmd/authz-gen`；`TestPrecheckMapIsFresh` 绿 |
| 9 | 测试 4 件 | `ent/schema/bot_template_migration_test.go`、`service/bot/admin_test.go`、`handlers/ai/bot_admin_test.go`、`router/bot_routes_test.go` |

## 3. 管理 API 契约

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/admin/bots` | `ai:read` | 列表（空租户自动种入默认助手） |
| GET | `/api/v1/admin/bots/:id` | `ai:read` | 详情（含 `grants`） |
| GET | `/api/v1/admin/bots/:id/grants` | `ai:read` | 授权列表 |
| POST | `/api/v1/admin/bots` | `ai:write` | 创建（默认 `draft`/`act_low`/`[]`；slug 租户内唯一） |
| PUT | `/api/v1/admin/bots/:id` | `ai:write` | 更新（slug 不可改，携带即 400） |
| DELETE | `/api/v1/admin/bots/:id` | `ai:write` | 删除（同事务级联删授权） |
| PUT | `/api/v1/admin/bots/:id/grants` | `ai:write` | 授权 upsert（同模板同工具唯一；风险 ≤ 模板上限） |
| DELETE | `/api/v1/admin/bots/:id/grants/:grantId` | `ai:write` | 删除授权 |

**租户隔离**：租户只来自 gin 上下文，请求体不接收 `tenant_id`；跨租户读写一律 404（不泄露存在性）。

## 4. 运行记录（本机，pwsh）

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go generate ./ent/` | exit 0；生成 `ent/bottemplate`、`ent/bottoolgrant` |
| 2 | `go test ./ent/schema/ ./service/bot/ ./handlers/ai/ ./router/ -count=1 -timeout 25m` | **全部 ok**（20.2s / 12.8s / 12.7s / 12.2s；含既有回归用例） |
| 3 | `go build ./...` | exit 0 |
| 4 | `go run ./cmd/authz-gen` + `go test ./middleware/ -run TestPrecheckMapIsFresh` | 生成成功；**ok 0.85s** |

## 5. 本次发现与修复

### D-1（已修复）级联删除顺序错误触发 FK 约束

- **现象**：`DeleteTemplate` 首版在同一事务内**先删模板再删授权**，SQLite（`_fk=1`）报 `FOREIGN KEY constraint failed`，删除模板 500。
- **根因**：父行仍被 `bot_tool_grants.bot_id` 引用时删除被驱动拒绝；顺序写反。
- **修复**：改为**先删授权、后删模板**（同一事务），并在删除前后各做一次存在性判定（并发删除返回 `ErrTemplateNotFound`）。
- **验证**：`TestTemplateAdmin_GrantUpsertLimitsAndCascade` 断言「删模板 → 授权随删 → 再删 404」通过。

### O-1（观察项）schema 注释引用的租户守卫测试缺位

- `internal/schema/tenant_guard.go:19` 提到「CI 跑 TestTenantGuard_ExemptConsistency」，但全仓无该测试实现。新增两表已按「索引首列含租户」自证合规（`(tenant_id, slug)` 唯一、`(tenant_id, bot_id, tool_name)` 唯一），不受该缺位影响；建议后续补实现或修正注释。

## 6. 遗留与下一步

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | 工具面按模板过滤（授权 ∩ RBAC ∩ 风险上限 ∩ 入口）与硬编码写白名单迁移 | **B2-02**（消费本任务的 `bot_tool_grants`） |
| 2 | 管理页模板/授权 CRUD（前端） | B2-03 |
| 3 | 授权负向安全测试集（未授权不可见/不可调用、跨租户 fail-closed） | B2-05 |
| 4 | 新租户开通时的默认助手种入（现为首次列表惰性种入，已覆盖；如需开通即种入可在租户 provisioning 调用 `SeedDefaultTemplate`） | B2 后续/运维 |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B2-01 交付：两表 + 服务 + 8 端点管理 API + 路由门禁 + 权限复用；四包回归全绿、`go build ./...` 干净、预检映射再生成；判定 `integration_verified` |
