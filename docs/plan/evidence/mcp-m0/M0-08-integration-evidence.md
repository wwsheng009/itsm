# M0-08 证据（管理服务与管理 API）

> 验收项：A0-08（对应任务 M0-08）｜目标 DoD：`integration_verified`
> 实际状态：**`integration_verified`**（组件间集成：gin handler + 服务层 + 真实 ent/DB + manager + transport + SDK mock 服务器；路由守卫与 RBAC 预检映射已刷新）。
> 偏差：未走完整 `router.SetupMCPServerRoutes` + RBAC 中间件链的端到端 HTTP 用例；bootstrap 装配与 `mcp:read`/`mcp:admin` 权限码切换属 M0-10；`/:id/events` 端点当前为编译级覆盖（用例见下）。
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

**服务层（`itsm-backend/mcp/admin/`）**
- `errors.go`：字符串错误码枚举（§5.5）→ HTTP 映射（`invalid_transport` 400 / `duplicate_name` 409 / `ssrf_blocked` 422 / 连接类 502 / `not_found` 404 / `conflict` 409 / `credential_error` 500 类）+ `AsAdminError`；连接类失败的文本兜底归类（timeout / unreachable）。
- `validation.go`：name（与 registry 同源）、transport（一期仅 streamable/sse）、URL 形态、timeout/并发/重试范围、键值清洗（拒绝 CR/LF 注入、长度与数量上限）。
- `audit.go`：`AuditEntry`（actor/tenant/action/object/before-after/result/errorCode/ip/ts）+ `AuditSink` + `MemoryAuditSink` + 掩码辅助。
- `events.go`：`EventBuffer`（实现 `manager.EventSink`，按服务器环形保留，供 `/:id/events`）。
- `store.go`：`EntStore` 实现 `manager.StatusWriter`（运行态回写）与 `manager.ToolCache`（发现结果落库、消失工具置 `healthy=false`、canonical 冲突跳过落库），另提供 `DeleteServerTools`/`ToolCounts`。
- `service.go`：16 个管理操作（CRUD / test / enable / disable / reload / tools list / 单·批量启停 / classification / rotate-credential / health / events）+ 凭据掩码投影 + 审计 + 「先落库再异步重连」+ 乐观锁 + 凭据合并不改写（`mergeSecrets`）。
- `credential.go`：新增 `SecretValues.Values()`（连接装配层取值出口，文档明确禁入日志/审计/响应）。

**HTTP 与路由**
- `handlers/mcp/handler.go`：薄层（身份 → 绑定 → 服务 → 契约错误映射 → 统一响应）；enable/disable/reload 返回 **202 + 数据体**；错误体带 `errorCode` 字符串码。
- `router/mcp_routes.go`：`/api/v1/ai/mcp-servers`（读 = `ai:read`，写 = `system:write`；M0-10 切换 `mcp:read`/`mcp:admin`）。
- `router/router.go`：`RouterConfig.MCPHandler` + 注册块（nil 不注册）。
- `middleware/rbac_precheck_gen.go`：`go run ./cmd/authz-gen` 重生成（+17 行；禁手改，新鲜度受 `TestPrecheckMapIsFresh` 守卫）。

**manager 修复（集成测试暴露）**
- `manager.go`：`Enable/Reload` 在「已有建连在途」时不再改写状态（原实现会在 healthy→endAttempt 窗口把状态打回 `connecting` 并卡死）；`connectAndDiscover`/事件组装改用 `conn.config()` 快照（消除 cfg 无锁读取）。
- `pool.go`：新增 `conn.config()`；`health.go` 事件组装改用配置快照。

**测试**
- `mcp/admin/service_test.go`（9 用例：CRUD/校验/乐观锁/启停与发现/工具治理/轮换/测试连接三态/摘要与租户隔离）
- `handlers/mcp/handler_test.go`（4 用例：信封与掩码/409·404 错误码/202 语义/400 校验）
- `mcp/manager/manager_test.go`：超时用例改为行为断言（`outcome=canceled`），消除跨平台计时抖动。

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l mcp/admin handlers/mcp mcp/manager router` | 无输出 |
| 2 | `go test ./mcp/admin/ -count=3` | 通过（3 轮重复，25.055s） |
| 3 | `go test ./handlers/mcp/ -count=2` | 通过（2 轮，11.624s） |
| 4 | `go test ./mcp/...` | 全绿（admin / client / manager / registry / transport） |
| 5 | `go test ./router/ -run 'TestRoutePermissionCodesAreDefined\|TestWriteRoutesRequirePermission\|TestPrecheckMapIsFresh\|TestRoutePrecheckAlignment'` | ok（4.985s） |
| 6 | `go run ./cmd/authz-gen` | `已生成 .../middleware/rbac_precheck_gen.go` |
| 7 | `go vet ./mcp/... ./handlers/mcp/ ./router/` | `vet-exit=0` |
| 8 | `go build ./...` | `build-exit=0`（311.3s） |

## 覆盖说明（对照 M0-08 测试与证据要求）

| 要求 | 覆盖 |
| --- | --- |
| CRUD | create/get/update/delete 全链路（含删除级联工具缓存、租户隔离 404） |
| 重复名 | 预查重 + 唯一约束双保险 → 409 `duplicate_name`（服务层与 HTTP 层各一断言） |
| 非法传输 | `stdio` → 400 `invalid_transport`（服务层 + HTTP 层）；非法 name/URL/换行注入 → 400 |
| SSRF 拒绝 | 真实 `SSRFGuard`（禁私网）+ 内网 URL → 422 `ssrf_blocked` |
| 测试连接失败注入 | 已关闭端口（守卫放行私网）→ 502 类（`unreachable`/`connect_timeout`）；成功路径用真实 SDK mock 断言协议版本/服务器名/工具预览，且**不落库**（工具表 0 行、临时槽位已清理） |
| 202 状态回读 | `POST /:id/enable` → HTTP 202 + `message=accepted` + `running_status=connecting`；`GET /:id` 可回读 |
| 审计断言 | `create_server`（actor/tenant/object/ip/ts/result）+ `update_server`（before/after）+ `enable/disable`（含 `set_tool_classification`、`bulk_set_tools`、`rotate_credential`）；全部断言**不含明文 token** |
| 乐观锁 | 旧 `version` → 409 `conflict`；成功后 `version+1` |
| 附加 | 凭据掩码（`Bear****23`/`api-****ef`）、DB 无明文（原生 SQL 断言）、凭据空值不修改且**密文不变**、工具治理不触发重连（D6）、批量空数组 = 全部、单工具 404、健康摘要计数、HTTP 错误信封 `errorCode` |

## 未覆盖 / 待办

- **bootstrap 装配与权限码（M0-10）**：`RouterConfig.MCPHandler` 当前无装配点，生产不可达；`mcp:read/mcp:write/mcp:admin` 权限码（seeder 三处同步）+ 路由声明切换属 M0-10。
- **`/:id/events` 端点**：服务方法与路由已实现并编译，但尚无 HTTP 级用例（EventBuffer 行为由 manager 事件路径间接覆盖）。
- **完整 router + RBAC 链路的 HTTP E2E**：M0-14 与 M0-13 mock 设施就绪后补齐（含前端 M0-12 联调）。
- **`-race` 未跑**：manager 的 cfg 读取已收敛到快照，但仍建议 M0-10/M0-14 补 `go test -race ./mcp/...`。
- **M0-13 mock 复用**：当前测试内联 SDK mock，M0-13 抽取为 `testutil/mockserver` 后回填。
