# M0-03 迁移与约束证据（ent 数据模型与迁移）

> 验收项：A0-03（对应任务 M0-03）｜目标级别：`integration_verified`（本轮达到 `unit_verified`，见「未覆盖」）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

- 新增 `itsm-backend/ent/schema/mcp_server.go`（服务器配置；默认关闭/不可信/凭据密文；`version` 乐观锁）；
- 新增 `itsm-backend/ent/schema/mcp_server_tool.go`（工具治理三态；默认 `read_only=false` / `risk=high` / `enabled=false`）；
- 修改 `itsm-backend/ent/schema/tool_invocation.go`：
  - MCP 字段：`provider`（默认 `builtin`）、`mcp_server_name`、`mcp_raw_tool_name`、`mcp_callable_name`、`args_redacted`、`output_summary`、`duration_ms`、`error_code`；
  - Bot 运行态字段（B0-02，**联合迁移一次加列**）：`run_id`、`step_id`、`risk`、`category`、`target_type`、`target_id`、`support_ref`、`idempotency_key_hash`、`expires_at`、`verify_state`、`verify_note`、`attempt_count`、`last_error_code`；
  - 索引：`(tenant_id, idempotency_key_hash)` 唯一、`(tenant_id, conversation_id)`；
  - 命名统一：B0-02 的 `input_redacted` 与 M0-03 的 `args_redacted` 属同一语义 → 联合窗口统一为 `args_redacted`（两侧方案已回写）。
- ent 生成代码刷新：`ent/mcpserver*`、`ent/mcpservertool*` 新增实体与查询/变更构建器；`ent/client.go`、`ent/ent.go`、`ent/migrate/schema.go` 等同步（复核：diff 均为新增实体/条目，无删除既有表或列）。
- 迁移与约束测试：`itsm-backend/ent/schema/mcp_migration_test.go`（外部测试包 `schema_test`，独立 sqlite 内存库）。

## 执行记录

| # | 命令（itsm-backend 目录） | 结果 |
| --- | --- | --- |
| 1 | `go generate ./ent` | `generate-exit=0`（512s；生成 2 个新实体，共 14 个既有文件同步更新） |
| 2 | `gofmt -l ent\schema` | 无输出 |
| 3 | `go test ./ent/schema/ -count=1` | `ok itsm-backend/ent/schema 6.152s` |
| 4 | `go build ./...` | `build-exit=0`（415s，含 ent 全量重编译） |
| 5 | `go test ./mcp/... -count=1` | 全绿（registry 0.248s、client 0.368s） |

## 覆盖说明

- **空库建表**：`mcp_servers`、`mcp_server_tools` 表存在；`tool_invocations` 联合字段 21 列齐备；`Schema.Create` 重复执行幂等。
- **旧数据兼容**：仅按旧列插入的行可被新代码读取，新列取默认值（`provider=builtin`、数值列为 0、`idempotency_key_hash` 为空且不破坏唯一索引）。
- **安全默认（D7）**：服务器默认 `enabled=false`、`trust_level=untrusted`、`credential_type=none`、`status=configured`；工具默认 `enabled=false`、`healthy=false`、`read_only=false`、`risk=high`；超时/并发/重试默认 30000ms / 4 / 1。
- **唯一约束**：`(tenant_id, name)`、`(tenant_id, server_id, raw_name)`、`(tenant_id, callable_name)`、`(tenant_id, idempotency_key_hash)` 均生效；未设置幂等键的多行不冲突（NULL 语义）。
- **乐观锁字段**：`version` 默认 1、可递增写回（管理端并发覆盖防护待 M0-08 使用）。
- **索引租户维度**：全部新增索引首列含 `tenant_id`（多租户红线，R-B04/R-M 对应项）。

## 未覆盖 / 待办（`integration_verified` 的剩余门槛）

- **Postgres 双驱动冒烟**与 SQLite/Postgres 迁移差异【未核实】：本地无 Docker/Postgres 环境，须在 CI 覆盖（M0-14 前，同 MCP U-2/U-3）。
- **真实旧库升级路径**（既有 `tool_invocations` 表 ALTER 加列）：本轮以「旧列插入行可读 + 迁移幂等」近似；真实旧库快照升级建议纳入 CI 阶段。
- `tool_invocations` 新字段的消费逻辑（审计写入、脱敏、幂等、verify）分别在 M0-09/M0-11 与 B0-02/B0-03/B0-05/B0-06 落地。
