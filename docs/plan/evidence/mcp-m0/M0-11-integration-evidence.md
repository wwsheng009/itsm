# M0-11 证据（审计写入与脱敏）

> 验收项：A0-11（对应任务 M0-11）｜目标 DoD：`integration_verified`
> 实际状态：**`integration_verified`**（工具调用三元组 + 脱敏入参 + 摘要 + 耗时 + 稳定错误码落库；管理操作审计落 `audit_logs`；脱敏原语独立成包并被三处复用；DB 往返与筛选断言 + 明文不漏断言）。
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `9d535365`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 决策与落点

| 要求（任务卡） | 落点 |
| --- | --- |
| 三元组落库 | `tool_invocations` 增写 `provider` / `mcp_server_name` / `mcp_raw_tool_name` / `mcp_callable_name`（M0-03 已建列，本任务打通写入） |
| 耗时与错误码 | `duration_ms`（provider 侧计时；ToolQueue 终态补写）、`error_code`（provider 稳定码 → ToolQueue/Service 回填；未知 = `internal_error`） |
| 脱敏入参 | 新增叶子包 `pkg/redact`（`ArgsJSON`：敏感键掩码 + 8KB 限额 + 合法 JSON 信封）；`args_redacted` 为审计/展示唯一来源 |
| 输出摘要 | `output_summary`（`redact.ValueSummary`，≤512 字符；不落原始 Value） |
| MCP 元数据来源 | `service.ToolExecution`（provider 返回）→ `ToolRegistry.ExecuteWithMeta` → `handlers/ai.Service` 审计写入；内置工具包装为 `provider=builtin` |
| 管理操作审计落库 | 新增 `mcp/admin/audit_ent.go`：`EntAuditSink` 写既有 `audit_logs`（`resource=mcp`、`method=MCP_ADMIN`、`path=/api/v1/ai/mcp-servers[/<id>]`、成功 200 / 失败 500、`request_body` = 脱敏 before/after/result/error_code/object）；bootstrap 由 `NewMemoryAuditSink` 切换为 `NewEntAuditSink(client)` |
| 凭据明文不入审计/日志 | 三层防线：① 服务层 before/after 已掩码；② `EntAuditSink` 再走 `redact.BodyJSON` 纵深脱敏；③ 工具链路错误只保留稳定码 + 短摘要（`redact.Summary`，移除原始 err 文本落库） |

## 变更文件

- `itsm-backend/pkg/redact/redact.go`（新增）：`SensitiveKey` / `Map` / `ArgsJSON` / `Summary` / `ValueSummary` / `BodyJSON`（零内部依赖叶子包）。
- `itsm-backend/pkg/redact/redact_test.go`（新增）：敏感键、嵌套掩码、截断信封、摘要、审计体。
- `itsm-backend/service/tool_provider.go`：新增 `ToolExecution`（审计元数据载体）与 `Execute` 契约升级。
- `itsm-backend/service/tool_registry.go`：新增 `ExecuteWithMeta`（内置包装 builtin；provider 原样透传）；`Execute` 退化为兼容入口。
- `itsm-backend/service/tool_queue.go`：审批后执行改走 `ExecuteWithMeta`；`finalize` 补 `duration_ms`、`error_code`、`output_summary`（`SetError` 落脱敏摘要）；新增 `errorCodeOf`（duck-typing，避免 service → mcp/provider 反依赖）。
- `itsm-backend/mcp/provider/execute.go`：所有返回路径填充 `ToolExecution`（三元组/耗时/错误码/摘要）；`ExecuteError` 增 `ErrorCode()` 供 service 侧提取。
- `itsm-backend/mcp/provider/provider.go`：`faceTool` 增 `serverName`（三元组所需）。
- `itsm-backend/handlers/ai/entity.go`：`ToolInvocation` 增 8 个审计字段（provider/三元组/argsRedacted/outputSummary/durationMs/errorCode）。
- `itsm-backend/handlers/ai/repository_impl.go`：Create/Update/领域映射打通新字段（空值不写，保持内置工具行为与列默认值）。
- `itsm-backend/handlers/ai/service.go`：`ExecuteTool` 只读路径改走 `ExecuteWithMeta`；失败也留痕（`status=failed` + 错误码）；`recordToolAudit` 写入脱敏入参与执行元数据。
- `itsm-backend/mcp/admin/audit_ent.go`（新增）：`EntAuditSink`。
- `itsm-backend/internal/bootstrap/app.go`：管理审计 sink 接线为 DB。
- 测试：`handlers/ai/service_mcp_audit_test.go`、`handlers/ai/repository_mcp_audit_test.go`、`mcp/admin/audit_ent_test.go`（新增）；`service/tool_registry_provider_test.go`、`mcp/provider/provider_test.go`（契约升级适配）。

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go build ./...` | exit 0 |
| 2 | `go vet ./mcp/... ./handlers/ai/ ./handlers/mcp/ ./service/ ./pkg/redact/ ./internal/bootstrap/` | exit 0 |
| 3 | `go test ./pkg/redact/ ./mcp/provider/ ./service/ ./handlers/ai/ ./mcp/admin/ -run '…'` | 全绿（定向） |
| 4 | `go test ./service/`（全量） | ok（312.5s） |
| 5 | `go test ./handlers/ai/ ./mcp/provider/`（全量） | ok（13.9s / 5.6s） |
| 6 | `go test ./mcp/admin/`（全量） | ok（含新增 2 用例） |
| 7 | `go build ./...` + `go vet`（6 包） | 0 / 0 |
| 8 | `go test ./mcp/... ./handlers/ai/ ./handlers/mcp/ ./pkg/redact/`（收尾批次，含 build+vet） | 全绿：mcp/admin 26.9s、mcp/client 3.7s、mcp/manager 2.2s、mcp/provider 12.2s、mcp/registry 0.4s、mcp/transport 0.7s、handlers/ai 28.6s、handlers/mcp 12.4s、pkg/redact 0.6s |

## 断言清单（对应任务卡「测试与证据」）

| 断言 | 位置 |
| --- | --- |
| 成功路径落三元组 + 耗时 + 摘要；`args_redacted` 掩码且**不含**明文 token | `handlers/ai/service_mcp_audit_test.go:TestExecuteTool_MCPReadOnlyAuditTriple` |
| 失败路径 `status=failed` 且 `error_code=tool_timeout` 落库（不静默） | 同文件 `TestExecuteTool_MCPFailureRecordsErrorCode` |
| Gate2 拒绝时不带 provider 元数据（不误标 mcp） | 同文件 `TestExecuteTool_PermissionDeniedKeepsBuiltinProvider` |
| 真实 DB 往返 + **按 provider/服务器筛选**；内置行不误命中 | `handlers/ai/repository_mcp_audit_test.go:TestEntRepository_MCPAuditRoundTrip` |
| 管理审计：`resource=mcp` / `method=MCP_ADMIN` / 200·500 / before-after 脱敏 / token 与 Authorization 明文不出现 / 超长截断 ≤8KB | `mcp/admin/audit_ent_test.go`（2 用例） |
| 脱敏原语：敏感键（含嵌套、数组）、截断信封仍为合法 JSON、摘要、体限额 | `pkg/redact/redact_test.go`（5 用例） |
| provider 元数据：成功路径三元组/耗时/摘要；错误码映射不变 | `mcp/provider/provider_test.go:TestProvider_Execute`（扩展断言） |

## 偏差与说明（相对任务卡）

1. **脱敏函数落点**：任务卡建议 `mcp/admin/redact.go`；实际落在 `pkg/redact`（叶子包）——
   理由：`handlers/ai` 与 `mcp/admin` 都要用，放 `mcp/admin` 会引入 `handlers → mcp/admin` 的
   非必要耦合；接口与语义（敏感键掩码 + 限额 + 合法 JSON）与任务卡一致。
2. **管理审计表**：复用既有 `audit_logs` 表（未新建 `mcp_audit_logs`）——理由：平台已有审计检索/导出链路，
   新建表会引入第二次迁移（M0-03 联合窗口已冻结）；before/after 以 `request_body` JSON 承载并纵深脱敏。
3. **`Arguments` 仍存原始入参**：审批重放（ToolQueue 反序列化后执行）依赖原始参数，必须保留执行真源；
   审计/展示一律使用 `args_redacted`。此为**有意保留**，已在 `handlers/ai/service.go` 注释与本文档登记。
4. **`mcp:write` 路径**：写工具执行（ToolQueue 终态）已同时打通 `duration_ms`/`error_code`/`output_summary`，
   但 MCP 写工具的 provider 执行属 M1-02，故三元组在 M1 打通后自然生效（本任务不提前打开写面）。
5. 前端展示（审计列表字段渲染）不在本任务范围，属 M0-12/M1 管理页。

## 未覆盖 / 待办

- **M0-12/M0-13/M0-14**：管理页审计视图、可复用 mock 设施、端到端 200/403 与 `-race`、真实环境审计检索验证。
- **M1-02**：写工具审批编排（三元组届时自动补齐，无需改审计层）。
- 存量欠账：`pkg/seeder` 租户模板流程定义种子缺失（M0-10 已用 stash 探针隔离，与本任务无关）。
