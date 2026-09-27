# M0-10 证据（权限位与路由装配）

> 验收项：A0-10（对应任务 M0-10）｜目标 DoD：`integration_verified`
> 实际状态：**`integration_verified`**（权限码三处同步 + 角色矩阵 + 路由声明守卫 + 预检重生成 + bootstrap handler 注入；协议/HTTP 层 403/200 实测）。
> 偏差：① HTTP 200/403 用例走硬编码兜底模式（空库）——DBOnly 模式的判定链路由角色矩阵（authz）与 seeder 一致性（parity）覆盖，真实环境 200/403 端到端属 **M0-14**；② `mcp:write` 默认仅 `sysadmin` 持有（admin 不显式授予）；③ 前端角色管理页的 mcp 分组展示未动（后端码位与 UI 分组自适应）。
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `3e49f7b1`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 决策落地（对照 M0-10 要点）

| 要点 | 落地 |
| --- | --- |
| 三码落地 | `internal/authz/catalog.go`：`mcp:read`（查看服务器/健康/工具面）、`mcp:write`（执行写工具，M1-02 起生效）、`mcp:admin`（服务器 CRUD/启停/工具治理/凭据轮换） |
| 默认拒绝（D7） | 除 `sysadmin`（全量）与 `admin`（`mcp:read` + `mcp:admin`）外**零授予**；`it_director`/`ops_director` 的 `allExcept` 显式排除三码（否则会随“除…外全给”继承） |
| 使用与治理分离 | 路由读组挂 `mcp:read`、治理写组挂 `mcp:admin`；匹配器层面 read/write 不蕴含 admin（负向用例锁定）；授予面 admin 不显式持 `mcp:write` |
| 路由守卫 | `router/mcp_routes.go` 由 `ai:read`/`system:write` 切换为 `mcp:read`/`mcp:admin`；`go run ./cmd/authz-gen` 重新生成 `middleware/rbac_precheck_gen.go`（17 条 MCP 条目从声明重算） |
| 路由装配 | `internal/bootstrap/app.go`：MCP 管理 handler 注入 `RouterConfig.MCPHandler`（开关关闭为 nil → 整组不注册、端点 404 即回滚语义） |
| 双权威表一致 | `middleware/rbac.go` 兜底表 admin 同步 `mcp:read`/`mcp:admin`（不写 `mcp:write`），守卫 `pkg/seeder/role_permission_guard_test.go` 通过 |

## 变更文件

- `itsm-backend/internal/authz/catalog.go`：新增三码定义（含决策注释）。
- `itsm-backend/internal/authz/roles.go`：`allPermissionCodes()` 纳入三码；`admin` 增 `mcp:read`/`mcp:admin`；`it_director`/`ops_director` 排除三码。
- `itsm-backend/internal/authz/mcp_roles_test.go`（新增）：默认授予面矩阵 + 全角色反向哨兵 + 分离语义守卫。
- `itsm-backend/middleware/rbac.go`：兜底表 admin 增两条 mcp 授权。
- `itsm-backend/middleware/mcp_permission_matrix_test.go`（新增）：Gate2 判定矩阵、分离反证、授予面断言、路由级 403/200。
- `itsm-backend/middleware/rbac_precheck_gen.go`：`go run ./cmd/authz-gen` 重生成（17 条 MCP 条目）。
- `itsm-backend/router/mcp_routes.go`：权限声明切换为 `mcp:read` / `mcp:admin`。
- `itsm-backend/router/mcp_routes_test.go`（新增）：装配门禁（nil→404）、17 条路由注册、路由声明码守卫。
- `itsm-backend/internal/bootstrap/app.go`：`MCPHandler` 注入 RouterConfig。

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go run ./cmd/authz-gen` | 生成 `middleware/rbac_precheck_gen.go`（17 增 17 删） |
| 2 | `go test ./internal/authz/ ./tests/parity/` | ok（0.220s / 0.536s） |
| 3 | `go test ./router/` | ok（15.288s；含新增 3 用例与预检一致性、声明码守卫） |
| 4 | `go test ./middleware/` | ok（3.217s；含新增 4 用例与 `TestPrecheckMapIsFresh`） |
| 5 | `go test ./middleware/ -run TestMCP` | ok（1.802s） |
| 6 | `go test ./router/ -run 'TestMCP\|TestSetupRoutes_MCP'` | ok（2.475s） |
| 7 | `go test ./pkg/seeder/ ./tests/parity/` | parity ok；**pkg/seeder 失败为存量问题**（见下） |
| 8 | `go build ./...` / `go vet ./router/ ./middleware/ ./internal/authz/ ./internal/bootstrap/` | 0 / 0 |
| 9 | `gofmt -l`（本次涉及目录） | `internal\bootstrap\app.go` 已格式化（编辑后 gofmt -w） |

### 存量失败隔离（pkg/seeder）

`TestProvisionTenantReadinessAcrossDeploymentModes/*` 与 `TestProvisionTenantRollsBackWhenSourceTemplateIsIncomplete` 失败，
错误均为 `resolve process definition incident_emergency_flow: ent: process_definition not found`。
**隔离方法**：`git stash push -- internal/authz/catalog.go … router/mcp_routes.go`（仅本次改动）后复跑同一用例 → **同样失败**；
`git stash pop` 已还原（`Dropped refs/stash@{0}`）。
结论：与 M0-10 无关的存量红（租户模板流程定义种子缺失），已登记为独立欠账，不在本任务修复范围。

## 覆盖说明

| 要求（任务卡） | 覆盖 |
| --- | --- |
| 角色矩阵（管理员/坐席/无权限 × read/write/admin） | `internal/authz`：sysadmin=全量、admin=read+admin（不显式 write）、总监/经理/坐席/技术员/最终用户=零；`middleware`：硬编码模式下 7 角色 × 3 码逐格断言 + 全角色反向哨兵 |
| 使用与治理分离 | `mcp:read` 单持 → admin/write 均 false；`mcp:write` 单持 → admin/read 均 false（反证）；说明：资源内 `admin` 动作为超集（既有匹配语义），故持 `mcp:admin` 者有效可读 |
| guard 测试通过 | 路由声明码 ⊆ 码空间（router）、预检新鲜度（middleware）、角色绑定 ⊆ 码空间与双表覆盖（seeder/parity）全部 ok |
| 路由 403/200 | 坐席 `GET /read`、`GET /admin` → 403；管理员同名路由 → 200（HTTP 实录） |
| 装配门禁 | handler nil → 无 `/mcp-servers` 路由且请求 404；注入后 17 条路由齐备、未带令牌 401/403 而非 404 |
| 跨租户引用 fail-closed | 属 M0-08/M0-09 已覆盖（服务层按 tenant 过滤 + 越权 404）；本任务不放松任何授权面 |

## 未覆盖 / 待办

- **M0-11**：`tool_invocations` 三元组/错误码落库与 DB 审计 sink（bootstrap 处 TODO 保留）。
- **M0-12/M0-13/M0-14**：管理页（含角色管理页 mcp 分组的展示回归）、可复用 mock 设施、真实环境端到端 200/403 与 `-race`。
- **M1-02**：`mcp:write` 的实际授予与 Gate3 审批编排（写工具参数冻结）。
- 存量欠账：`pkg/seeder` 租户模板流程定义缺失（与本任务无关，已隔离证明）。
