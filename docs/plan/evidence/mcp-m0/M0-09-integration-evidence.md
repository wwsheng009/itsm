# M0-09 证据（ToolProvider 接入与只读工具执行）

> 验收项：A0-09（对应任务 M0-09）｜目标 DoD：`integration_verified`
> 实际状态：**`integration_verified`**（真实 ent 库 + 假 manager 源 + 服务层聚合 + handler 解析入口 + bootstrap 装配；全链路可编译、可启动）。
> 偏差：① 工具面 RBAC 需 `mcp:read`（Gate2 资源位 `Resource=mcp`）——权限码定义与授权在 **M0-10**，在此之前仅 `super_admin` / RBAC 关闭时可执行；② 写工具面默认关闭（`IncludeWriteTools=false`），Gate3 审批编排属 **M1-02**；③ 审计三元组（provider/mcp_server_name/mcp_raw_tool_name）落库属 **M0-11**（本任务已保证解析结果携带三元组可用）；④ `go test -race` 未跑。
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `e83da5fb`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 设计落地（对照任务卡要点 1–6）

| 要点 | 落地 |
| --- | --- |
| 1 工具面公式 | `mcp/provider.snapshot`：`server.enabled(租户内) ∧ manager.EffectiveTools（healthy ∧ enabled ∧ ¬quarantined）∧ registry 投影未被隔离 ∧ inputSchema 可解析`；租户隔离与开关关闭一律 fail-closed 返回空面 |
| 2 同一投影/解析函数 | 投影与解析都走 `mcp/registry`：投影用 `CanonicalToolName`，解析用 `registry.Resolve`（canonical 精确 → 唯一短名 → **歧义 fail-closed**）；provider 内 `snapshot` 同时产出「定义 + (serverID, rawName) 路由」，展示与执行不会漂移 |
| 3 provider 不感知审批 | `provider.Execute` 只做解析/schema 校验/调用/规范化；`service.ToolRegistry.Execute` 对 `!ReadOnly` 一律拒绝直执行（"requires approval"），Gate3 编排留待 M1-02 |
| 4 参数校验 + 结果规范化 | 执行前 `jsonschema.Resolved.Validate`（`github.com/google/jsonschema-go`，SDK 同源；无效 schema 的工具不进面）；结果 → `Output{provider,is_error,content[],truncated,bytes}`，256KB 上限、超限截断并标记，控制字符清理（保留换行/制表符） |
| 5 不可信数据 + 结构化失败 | 输出仅做结构化投影，不解析指令、不拼系统提示；传输/协议错误 → 稳定错误码（`tool_not_found/invalid_args/target_unavailable/tool_timeout/tool_canceled/auth_required/server_error/tool_error`），**不含** URL/IP/堆栈；工具自身 `IsError=true` 按 MCP 语义回填给模型而非抛错 |
| 6 并发/超时 | 复用 manager 治理（每服务器并发默认 4、单次超时取服务器 `timeout_ms` 默认 30s），provider 不重复叠加 |

## 变更文件

**新增**
- `itsm-backend/service/tool_provider.go`：`ToolProvider` 接口契约（同源 Gate1/2/3、同投影/解析、只读一期、不可信数据）。
- `itsm-backend/mcp/provider/provider.go`：工具面快照（租户/治理/健康/隔离/schema 过滤）、定义投影（`[MCP:<server>]` 前缀 + 描述清洗限长）、名字解析。
- `itsm-backend/mcp/provider/execute.go`：只读执行、参数 schema 校验、传输错误 → 稳定错误码映射、`SummarizeError`（审计用短摘要）。
- `itsm-backend/mcp/provider/normalize.go`：结果规范化与字节上限截断。
- `itsm-backend/mcp/provider/provider_test.go`（4 用例）、`itsm-backend/service/tool_registry_provider_test.go`（3 用例）。

**修改**
- `itsm-backend/service/tool_registry.go`：`providers` 字段 + `RegisterProvider/Providers/GetToolForTenant`；`ListToolsForTenant` = 内置 ∪ provider（内置优先、重名以内置为准）；`Execute` 拆为「内置优先 → provider 委派」，provider 写工具 fail-closed。
- `itsm-backend/handlers/ai/service.go:131`、`handlers/ai/handler.go:80`：工具解析改用 `GetToolForTenant`（聊天与 `/agent/tools/execute` 同一入口；`ListToolsForTenant` 已自动含 MCP）。
- `itsm-backend/mcp/admin/service.go`：新增 `Manager()` 与 `Startup(ctx)`（启动期拉起已启用服务器：装配运行态 + 异步建连 + 发现，幂等，单条失败不阻断）。
- `itsm-backend/internal/bootstrap/app.go`：M0-01 占位块替换为真实装配（凭据密钥解析：生产缺失即拒绝 → ent store → manager（SSRF 严格默认 + 状态/工具落库 + 事件）→ 管理服务 → `toolRegistry.RegisterProvider`），开关关闭时零装配零后台行为。
- `itsm-backend/go.mod`：`github.com/google/jsonschema-go` 提升为直接依赖。
- `itsm-backend/mcp/admin/service_test.go`：新增 `TestService_StartupLoadsEnabledServers`（跨「进程重启」重建运行态）。

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./mcp/provider/ -count=1` | ok（2.740s，4 用例） |
| 2 | `go test ./mcp/... -count=1` | 全绿（admin/client/manager/provider/registry/transport） |
| 3 | `go test ./service/ -run 'ToolRegistry\|ToolOntology\|Tool' -count=1` | ok（1.563s，含新增 3 用例） |
| 4 | `go test ./handlers/ai/ -count=1` | ok（15.490s；Gate2/工具契约回归） |
| 5 | `go test ./mcp/admin/ -count=1` | ok（5.440s，含 Startup 用例） |
| 6 | `go vet ./mcp/... ./service/ ./handlers/ai/ ./internal/bootstrap/` | `vet-exit=0` |
| 7 | `go build ./...` | exit 0（bootstrap 装配后全量编译通过） |

## 覆盖说明

| 要求（任务卡） | 覆盖 |
| --- | --- |
| 工具进面 | 只读 ∧ 服务器启用 ∧ 同租户 ∧ 未隔离 ∧ schema 可解析才进面；写工具/禁服务器/跨租户/坏 schema/非法名逐条负向断言 |
| 执行成功 | 路由到 `(serverID, rawName)`（断言源收到的 serverID/rawName/args），结果规范化 `provider=mcp` |
| 失败回填 | schema 拒绝（不发车）、未知名、写工具拒绝、传输超时/不可达映射；错误文本不含内网地址 |
| 未启用不可见 | `Options.Enabled=false`、`client=nil`、`source=nil`、租户非法 → 空面 |
| quarantine 不可执行 | registry 隔离记录不进面、不可解析 |
| schema 拒绝 | `additionalProperties:false` + 错类型 → `invalid_args` 且零调用；`{not-json` 工具不进面 |
| 并发上限 | 由 manager 信号量治理（M0-07 已测）；provider 不重复叠加 |
| 工具面组装单测 | `ListToolsForTenant` 内置 ∪ provider、内置优先去重、`Providers()` 副本语义 |
| 审计三元组（与 M0-11 联调） | 解析结果携带 `serverID/rawName/callableName`；`tool_invocations` 落库字段属 M0-11（当前 `handlers/ai` 审计仍记工具名） |

## 未覆盖 / 待办

- **M0-10**：`mcp:read`/`mcp:write`/`mcp:admin` 权限码与角色授予（Gate2 生效前提）、管理 API 路由装配（`RouterConfig.MCPHandler` 注入点已就绪但未接线）。
- **M0-11**：`tool_invocations` 三元组/耗时/错误码/`args_redacted` 落库；管理审计从内存 sink 换 DB sink（bootstrap 已标注 TODO）。
- **M1-02**：`IncludeWriteTools=true` + Gate3 审批编排（参数冻结）与 `ToolQueue` 委派路径。
- **M0-12/M0-13/M0-14**：管理页、可复用 mock 设施（当前测试内联假源/假会话）、端到端联调与 `-race`。
