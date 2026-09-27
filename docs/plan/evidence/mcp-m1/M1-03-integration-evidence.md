# M1-03 证据（SSE 事件契约扩展：tool_call_started / tool_call_finished / tool_call_failed / approval_pending）

> 验收项：A1-03（对应任务 M1-03）｜DoD：`integration_verified`
> 实际状态：**`integration_verified`**（后端契约 + 前端解析兼容双双锁定；模型真实触发工具的事件序列由 M1-10 联调覆盖）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13 / jest（itsm-frontend），基线 `3c83cb72`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 变更 |
| --- | --- |
| `handlers/ai/tool_events.go`（新增） | `ToolStreamEvent` 契约（字段 `id/tool/provider/server/phase/status/summary/durationMs/errorCode`）、状态常量、`toolEventSummary`（`pkg/redact` 掩码 + 截断）、`toolEventErrorCode`（稳定错误码映射，结构化接口避免反向依赖） |
| `handlers/ai/tool_events_sse.go`（新增） | `writeToolEvent`：status → SSE 事件名唯一映射（未知状态不吞事件） |
| `handlers/ai/service.go` | `ChatStream`/`ChatStreamWithProviderInfo`/`chatStream` 增加 `onTool` 回调并逐层透传；`execTool` 内按「开始 → 成功/失败/待审批」发事件（来源与读写性质取自同一解析入口 `GetToolForTenant`） |
| `handlers/ai/handler.go` | `ChatStream` 注入 `onTool` → `writeToolEvent`；事件清单写入接口注释（叠加语义说明） |
| `itsm-frontend/src/lib/api/ai-api.ts` | 新增 `AIToolStreamEvent`/`AIToolStreamCallback` 类型、四种事件解析（共用 `onToolEvent`；缺字段按事件名补偿；未知事件 default 忽略） |
| `itsm-frontend/src/lib/api/__tests__/ai-api.test.ts` | 新增 3 用例：四事件透传、未知事件忽略且后续流不受影响、半截/非对象载荷静默丢弃 |

## 事件契约（对外冻结）

| SSE 事件名 | status | 关键字段 | 触发点 |
| --- | --- | --- | --- |
| `tool_call_started` | `started` | `tool/provider/server?/phase` | 每次工具调用（读与写）开始 |
| `tool_call_finished` | `done` | `+ summary/durationMs` | 读路径执行成功（`durationMs ≥ 1`） |
| `tool_call_failed` | `failed` | `+ errorCode` | Gate2 拒绝 / 未知工具 / 依赖不可得 / 执行失败 |
| `approval_pending` | `pending` | `+ id`（invocation） | 写工具提交审批（执行发生在审批之后，不在本次流内） |

- **脱敏与截断**：`summary` 经 `pkg/redact` 键级掩码（`password/token/...` → `****`）+ 值截断 + 整体上限 320 字符（超出追加 `…(truncated)`）；原始参数与凭据绝不进入事件。
- **兼容性**：既有 `sources/delta/done/error` 语义未变；事件为**叠加**语义（过程可见），最终答案仍由 `delta/done` 承载，事件丢失时前端按最终消息降级渲染（M1-04 落地渲染）。
- **错误码**：`tool_permission_denied / unknown_tool / tool_unavailable / tool_queue_unavailable / invocation_not_pending`，外部工具错误经 `ErrorCode()` 结构化提取（如 `server_error`），未分类统一 `tool_execution_failed`（不外泄底层错误串）。

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go build ./...`（itsm-backend） | exit 0 |
| 2 | `go test ./handlers/ai/ -run 'TestWriteToolEvent\|TestToolStreamEvent\|TestToolEventSummary\|TestToolEventErrorCode' -v` | 4 个测试函数全 PASS（含 8 个子用例） |
| 3 | `go test ./handlers/ai/ -count=1`（全包回归） | ok（11.0s） |
| 4 | `npx jest src/lib/api/__tests__/ai-api.test.ts`（itsm-frontend） | **44/44 passed**（含新增 3 用例）；命令退出码 1 仅来自单文件运行的全局覆盖率阈值告警，非测试失败 |
| 5 | `npx tsc --noEmit` | exit 0 |

## 兼容性验证要点

1. **旧前端**：`default: break` 忽略未知事件（jest「ignore unknown SSE events」用例锁定：未知事件后 `delta/done` 仍正常消费）。
2. **旧后端**（无工具事件）：前端不触发 `onToolEvent`，既有渲染路径零变化（44 个既有用例全绿）。
3. **半截载荷**（缺 `tool`、非对象）：静默丢弃，不影响消息流与 `conversationId` 返回。
4. **帧安全**：载荷单行（换行仅 JSON 转义），`event:`/`data:` 帧格式与既有事件一致。

## 已知缺口（不阻塞本任务 DoD）

1. **端到端事件序列**（模型真实触发工具 → 事件时序）由 **M1-10** 联调覆盖：本包无法脱离 RAG 主链路起真实工具循环，故此处锁定对外契约（事件名/字段/脱敏/兼容），链路时序由 E2E 补齐。
2. 前端**渲染**（时间线折叠块、待审批卡片）属 M1-04 / M1-05；本任务只做解析与类型，`AIChat.tsx` 未接入（避免半成品 UI）。
3. 事件不落库（纯过程信号）：审计真源仍是 `tool_invocations`（M0-11）；事件与审计记录通过 `id`（写路径）关联。
