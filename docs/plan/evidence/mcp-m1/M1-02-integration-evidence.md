# M1-02 证据（写工具接入 Gate3 审批：pending → 审批 → 执行 → 回填 → 审计）

> 验收项：A1-02（对应任务 M1-02，与 M1-10 联调）｜DoD：`flow_verified`
> 实际状态：**`integration_verified`**（后端写路径端到端全通；`flow_verified` 待 M1-10 —— 对话内时间线 M1-04 / 待审批卡片 M1-05 / 审批页增强 M1-06 / SSE 契约 M1-03）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13，基线 `5dd72985`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 变更 |
| --- | --- |
| `service/tool_provider.go` | 新增 `WriteCapableProvider` 契约（审批后写执行；单次不重试；provider 不感知审批状态） |
| `service/tool_registry.go` | `ToolDefinition` 增 `Provider/ServerName/RawToolName/Risk`；`GetToolForTenant` 补 `builtin` 标记；新增 `HasProviderTool`、`ExecuteApprovedWrite`（**唯一写执行入口**，provider 不支持则 fail-closed） |
| `mcp/provider/provider.go` | 工具定义携带来源三元组与 risk（审批创建时即可落库） |
| `mcp/provider/execute.go` | `execute(..., allowWrite)` 重构 + `ExecuteApprovedWrite`；**直接 `Execute` 对写工具保持拒绝**；写调用单次不重试（用例锁定） |
| `service/tool_queue.go` | `Enqueue` 返回 `ErrToolQueueFull`（不再静默丢弃）；worker 增 provider 工具分流；`finalize` 回填三元组、耗时口径 ≥1ms |
| `handlers/ai/service.go` | pending 创建时落来源三元组；`ApproveTool` 状态机 + 队列 fail-closed + 入队失败回滚；新增 `ErrInvocationNotPending`/`ErrToolQueueUnavailable` |
| `handlers/ai/handler.go` | 审批列表返回来源/风险/决策人/结果字段；**移除原始 `arguments` 回显**；审批错误码分层（409/503） |
| `config/config.go`、`config.yaml.example` | 新增 `mcp.write_enabled`（**默认 false**，L1.5 回滚开关；开启后仍需 mcp:write + 审批） |
| `internal/bootstrap/app.go` | `IncludeWriteTools: cfg.MCP.WriteEnabled` + 启动日志 `write_enabled` |
| `itsm-frontend/src/lib/api/ai-api.ts`、`pages/(main)/ai/approval/index.tsx` | 类型与页面改用 `argsRedacted`（同步上述安全修正） |

## 写路径语义（实现事实）

```
对话/工具面提交（model → ExecuteTool）
  └─ Gate2：Resource=mcp / Action=write（需 mcp:write；Gate2 修复见 M1-01）
  └─ 落 pending tool_invocation：参数快照(执行真源) + args_redacted(展示唯一来源) + 来源三元组 + role_snapshot
审批（ApproveTool，需 ai:write）
  ├─ 仅 approval_state=pending 可审批（重复审批 409，防重复执行）
  ├─ 拒绝：approval_state=rejected + status=rejected + reason + 决策人/时间（不触发执行）
  └─ 通过：队列不可用/已满 → 不改状态直接失败（503，可重试）；否则 approved + 入队，入队失败回滚 pending
执行（ToolQueue worker）
  └─ provider 工具 → ToolRegistry.ExecuteApprovedWrite → provider.ExecuteApprovedWrite（单次、不重试）
  └─ finalize：status=done/failed + result/output_summary + duration_ms + error_code + 三元组兜底回填
```

## 用例与断言

| 用例（文件） | 断言 |
| --- | --- |
| `TestM1WriteApproval_EndToEnd`（`tests/mcpintegration`） | ① 提交 → pending 且三元组（provider=mcp / server=mock / raw=create_issue / callable=mcp__mock__create_issue）与 `args_redacted` 掩码（`"token":"****"`，无明文）一次落库；② **参数冻结**：再次提交生成新记录、原记录参数不变；③ approve → 队列执行 → `done` + `approved_by/approved_at` + `duration_ms≥1` + `output_summary`，且 mock **恰好被调用一次**、执行参数为落库快照（篡改的标题未被执行）；④ 重复审批 → `ErrInvocationNotPending` 且不产生第二次执行；⑤ 拒绝 → `status=rejected`/`approval_state=rejected`/原因/决策人，且不触发执行；⑥ 租户内共 3 条记录全部留痕 |
| `TestM1WriteApproval_FailClosedWithoutQueue` | 未装配队列时 `ApproveTool` → `ErrToolQueueUnavailable`；记录保持 `pending`（可重试），mock 零调用 |
| `TestProvider_ExecuteApprovedWrite`（`mcp/provider`） | ① 未审批的直接执行仍 `tool_not_found` 且**不触达服务器**；② 审批后执行成功一次（元数据齐全、耗时 ≥1ms）；③ 失败路径**累计恰好 2 次调用**（失败不重试，错误码 `server_error`）；④ schema 不通过不发请求；⑤ 只读工具走审批入口等价执行 |
| `TestToolQueue_EnqueueFailClosedWhenFull`（`service`） | 队列满 → `ErrToolQueueFull`，任务不入队（不静默丢弃） |
| `TestListToolInvocations_ExposesMCPSourceAndRisk`（`handlers/ai`） | 列表响应含 `provider/serverName/rawToolName/callableName/risk/roleSnapshot` 与 `argsRedacted`；**响应体不含明文敏感值、不含 `"arguments"` 原始参数** |

## 执行记录

| # | 命令（workdir=`itsm-backend`） | 结果 |
| --- | --- | --- |
| 1 | `go build ./...` | exit 0 |
| 2 | `go vet ./tests/mcpintegration/ ./mcp/provider/ ./service/` | exit 0 |
| 3 | `go test ./tests/mcpintegration/ -count=1 -timeout 300s` | ok（M0-14 + M1-01 + M1-02 共 9 用例，9.1s） |
| 4 | `go test ./handlers/ai/ -count=1` | ok（9.9s） |
| 5 | `go test ./service/ ./mcp/... ./middleware/ ./config/ ./tests/mcpintegration/ -count=1` | 除下述偶发外全绿；`service 310s`、`mcp/admin 22s`、`mcp/provider 12s`、`mcp/manager`、`mcp/registry`、`mcp/testutil/mockserver`、`mcp/transport`、`middleware`、`config`、`tests/mcpintegration` 全部 ok |
| 6 | `npx tsc --noEmit`（itsm-frontend） | exit 0 |

## 真实问题与修正（本轮）

| # | 问题 | 影响 | 处置 |
| --- | --- | --- | --- |
| 1 | 审批列表接口回显**原始参数**（`arguments`），前端审批页直接渲染（含展开全文） | MCP 写工具的口令/token 会出现在审批页面与浏览器网络面板；违反 M0-11「`args_redacted` 为审计/展示唯一来源」 | 后端列表移除 `arguments`、仅返回 `argsRedacted`；前端类型与页面同步改用 `argsRedacted`；用例断言响应体无明文、无 `"arguments"` |
| 2 | `healthy` 状态与工具发现落库之间存在毫秒级窗口（M0-07 状态机先置 healthy，随后写 ToolCache） | 测试助手在 `healthy` 后立即取工具面可能拿到空集合（本轮偶发 1 次） | 测试助手改为「healthy **且** 工具面非空」双条件；产品侧行为记录为已知语义（状态语义 = 连接可用，不承诺发现已完成） |

## 已知缺口（不阻塞本任务 DoD）

1. **拒绝/过期的会话回填**依赖阶段一 G3（SSE 事件 `approval_pending` 等属 M1-03，对话内卡片属 M1-05）；本任务保证的是记录状态与审计留痕。
2. **队列持久化**：ToolQueue 仍是内存队列（阶段一 B1 未落地），进程重启会丢失已入队任务；M1-08（运维收口）与阶段一一并处理。
3. **审批列表 risk 为实时解析**：服务器不可达/工具被隔离时退化为空字符串（三元组仍从落库快照返回，不影响追溯）。
4. 观察到的偶发：`mcp/client` 的 `TestConnect_SSE_Handshake` 在本次全量并发负载下失败 1 次，单包单独运行通过（疑似资源竞争）；建议 CI 侧重跑或纳入 flaky 跟踪，与本次改动无因果关系（未触碰 `mcp/client`、`mcp/transport`）。
