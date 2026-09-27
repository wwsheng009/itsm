# M0-02 单元级证据（命名投影与解析）

> 验收项：A0-02（对应任务 M0-02）｜目标级别：`unit_verified`
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

- `itsm-backend/mcp/registry/projection.go`：canonical 投影 `mcp__<server>__<tool>`（非法字符替换 + FNV-1a 防碰撞后缀；超长 >64 截断 + SHA-256 identity hash；与参考实现规则逐字对齐）；
- `itsm-backend/mcp/registry/registry.go`：投影索引（幂等注册、canonical 碰撞隔离、Unregister/Clear、并发安全）；
- `itsm-backend/mcp/registry/quarantine.go`：隔离原因与诊断结构（`canonical_collision` / `invalid_server_name` / `empty_tool_name`）；
- `itsm-backend/mcp/registry/errors.go`：`ErrNotFound` / `ErrEmptyName` / `AmbiguousToolError`；
- 契约测试：`projection_test.go`（投影五类）、`resolve_test.go`（解析四类 + 遮蔽）、`quarantine_test.go`（隔离四类 + 解除路径）。

## 执行记录

| # | 命令（itsm-backend 目录） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l mcp\registry` | 无输出（修正 1 处字段对齐后） |
| 2 | `go vet ./mcp/...` | `vet-exit=0` |
| 3 | `go test ./mcp/... -count=1` | 全绿：`ok itsm-backend/mcp/registry 0.502s`、`ok itsm-backend/mcp/client 0.694s` |

## 覆盖说明（契约逐条落测）

- **投影五类**：唯一名（无哈希后缀）；非法字符（`a.b` 追加哈希且与 `a_b` 不碰撞）；超长（严格 64 字符 + identity hash + 输入唯一性）；边界（恰好 64 原样、65 起截断）；空片段（`unnamed` 兜底且确定）。
- **解析四类**：canonical 精确优先；原始短名唯一命中可用；多候选返回 `*AmbiguousToolError`（候选排序、fail-closed）；未找到 / 空名错误；另含「短名遮蔽 canonical」场景断言 canonical 优先。
- **隔离四类**：canonical 完全碰撞（后到者隔离、先到者可解析、隔离工具不可列/不可解析、诊断含碰撞对象）；解除路径（Unregister 释放后重新注册成功且隔离记录清除）；非法服务器标识与空工具名直接隔离；同键重复注册幂等（Duplicates）。
- **执行归一化**：`Resolve` 返回值携带 `(Server, RawName)` 与 `Key()`（`server\x00raw_name`），供 manager/provider 路由使用，杜绝串服务。

## 未覆盖 / 待办（不阻塞本任务 DoD）

- 与连接生命周期（M0-07 manager）和工具面出口（M0-09 provider）的集成测试。
- 短名解析策略的最终消费方（LLM 工具面一律使用 canonical 名；短名解析仅作为兼容入口）。
