# M0-01 单元级证据（工程骨架与开关）

> 验收项：A0-01（对应任务 M0-01）｜目标级别：`unit_verified`
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

- 新增 `itsm-backend/mcp/README.md`（模块边界、依赖方向、SDK 升级策略、开关语义）；
- 新增 `itsm-backend/mcp/{transport,client,registry,manager,admin,provider}/doc.go`（六包骨架）；
- 新增 `itsm-backend/mcp/client/sdk_handshake_test.go`（SDK 握手回归，P2 spike 闭环）；
- 修改 `itsm-backend/go.mod`、`go.sum`（`github.com/modelcontextprotocol/go-sdk v1.4.0` 直接依赖，锁版）；
- 修改 `itsm-backend/config/config.go`（`MCPConfig` + `applyMCPDefaults` + `MCP_ENABLED` 环境兜底）；
- 修改 `itsm-backend/config.yaml.example`、`itsm-backend/.env.example`（mcp 配置块）；
- 修改 `itsm-backend/internal/bootstrap/app.go`（`cfg.MCP.Enabled` 预留装配点，关闭态零行为）；
- 新增 `itsm-backend/config/config_mcp_test.go`。

## 执行记录

| # | 命令（itsm-backend 目录） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l config mcp internal\bootstrap` | 无输出（全部已格式化） |
| 2 | `go build ./...` | `build-exit=0`（wall 337s，含首次缓存构建） |
| 3 | `go test ./mcp/... ./config/... -count=1` | 全绿：`ok itsm-backend/mcp/client 0.638s`、`ok itsm-backend/config 0.696s`；其余 mcp 包无测试文件 |

## 覆盖说明

- **SDK 握手（P2）**：`mcp/client/sdk_handshake_test.go` 使用官方 SDK 起 Streamable HTTP 服务端 → 客户端 `Connect`（内含 `initialize` 握手）→ `tools/list` → `tools/call` 全链路断言；同时作为 SDK 升版回归守卫。
- **配置开关**：`config_mcp_test.go` 覆盖「`${MCP_ENABLED:false}` 环境变量覆盖」与「未配置 `mcp` 块时默认关闭 + 默认值 10/30/10/20」；`test_timeout_seconds` 上限钳制 10s 已断言。
- **关闭态零行为**：`cfg.MCP.Enabled=false` 时 bootstrap 不初始化任何组件、不输出 MCP 日志；启用时输出装配点日志（含超时/限额），供启用态核验。

## 未覆盖 / 待办（不阻塞本任务 DoD）

- `go test ./...` 全量回归未在本轮执行（耗时较长，且需下载大量测试依赖）；建议 CI 门禁接入后补齐（M0-14 前）。
- 管理 API、传输、registry 等实现尚未开始（M0-02 起）。
