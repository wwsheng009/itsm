# ITSM MCP 模块（外部工具接入）

> 对应实施方案：`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M0–M2）
> 当前状态：M0-01 工程骨架（仅包边界、SDK 依赖与全局开关，无业务逻辑）

## 包职责与依赖方向

```text
transport ← client ← manager ← admin / provider
（registry 为纯逻辑包，无 IO，不依赖其他 mcp 包）
```

| 包 | 职责 | 落地任务 |
| --- | --- | --- |
| `transport` | Streamable HTTP / SSE 传输封装、出站安全（SSRF 校验） | M0-04 / M0-05 |
| `client` | 客户端会话：协议协商、连接与调用超时、错误分层 | M0-04 |
| `registry` | 命名投影 `mcp__<server>__<tool>`、解析 fail-closed、canonical 碰撞 quarantine（纯逻辑） | M0-02（最高优先级） |
| `manager` | 连接生命周期与状态三态（`configured_enabled` / `healthy` / `effective`） | M0-07 |
| `admin` | 管理服务与管理 API、凭据加密（AES-GCM，只写不读回） | M0-06 / M0-08 |
| `provider` | ToolProvider 聚合：投影进 `ToolRegistry`，与内置工具同源 Gate1/2/3、队列与审计 | M0-09 |

依赖规则：禁止反向依赖；禁止包级可变全局状态；`registry` 不引入 IO（便于纯单测）。

## 依赖与版本

- SDK：`github.com/modelcontextprotocol/go-sdk v1.4.0`（与参考实现 ai-agent-runtime 同版本；已在 `go.mod` 锁版）。
- 升级策略：升版必须通过 `go test ./mcp/...`（含 SDK 握手回归 `mcp/client/sdk_handshake_test.go`），并评审协议版本兼容策略（分析报告 §4.4：不匹配即拒绝，不降级）。

## 开关与默认值

- `mcp.enabled`（`config.MCPConfig.Enabled`，默认 `false`）：关闭时对现有系统**零行为变化**——不建连、不装配组件、工具面不含 MCP。
- 连接与超时（`config.yaml.example` 的 `mcp:` 块，均支持 `${ENV:default}`）：
  - `connect_timeout_seconds` 默认 10（建连超时）；
  - `call_timeout_seconds` 默认 30（单次 `tools/call`）；
  - `test_timeout_seconds` 默认 10，且强制上限 10（管理面「测试连接」同步短超时）；
  - `max_servers_per_tenant` 默认 20（单租户服务器数量上限）。
- 装配预留点：`internal/bootstrap/app.go` 中 `cfg.MCP.Enabled` 守卫块（M0-01 落位；M0-02/M0-04/M0-07 的组件按依赖顺序在其内部装配）。
