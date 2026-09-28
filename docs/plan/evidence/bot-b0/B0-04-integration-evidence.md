# B0-04 集成证据（dry-run 分支与零写入契约）

> 文档类型：实施证据（任务 B0-04）
> Status: draft
> 编制日期：2026-09-27
> 任务：B0-04（dry-run 分支与零写入契约）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.1 B0-04、§5.2 AB0-04）
> 核查方式：本机编译 + 定向/全量测试；含一次真实编译失败与修复记录

## 1. 结论

| 任务 | 目标级别 | 本轮判定 | 依据 |
| --- | --- | --- | --- |
| B0-04 | `integration_verified` | **`integration_verified`** | 预览分支 `create_ticket`（字段投影）/`update_ticket`（diff，只读读取）落地；零业务写入由「未注入任何业务服务」的结构性约束 + 断言保证；预览快照随记录落库（`dry_run=true`、`status=preview`、`result=快照`、`version` 内容哈希）；不进审批队列 |

> `flow_verified` 待 B1-05（确认单冻结执行参数）与 B1-07（前端 dry-run 预览交互）落地后回写。

## 2. 交付物

| 变更 | 位置 | 说明 |
| --- | --- | --- |
| 预览实现 | `itsm-backend/service/tool_preview.go`（新增） | `ToolPreview{Mode: create/diff, Fields, Diff, Target, Version, GeneratedAt, Note}`；`PreviewTool(ctx, tenantID, name, args)`；`ErrPreviewNotWrite` / `ErrPreviewUnsupported`；`PreviewNotGuaranteedNote`（BQ7 固定文案） |
| 零写入保证 | 同上 | 预览只做 **参数投影**（create）与 **只读读取**（update 走 `TicketService.GetTicket`，不调用任何写方法）；`NewToolRegistry(nil,...)` 场景下若触碰业务写路径会立即失败，测试据此具备约束力 |
| 快照版本 | 同上（`previewVersion`） | 内容哈希（sha256 前 16 字节）：同输入同版本、参数变化即变版本，供 B1-05 确认时比对参数未被篡改 |
| 执行分支 | `handlers/ai/service.go` | 新增 `ExecuteToolOptions{ConversationID, DryRun}` + `ExecuteToolWithOptions(...)`（`ExecuteTool`/`ExecuteToolWithConversation` 变为薄封装，**签名与行为不变**）；`opts.DryRun` → `executeDryRun`：写工具预览、失败留痕（`preview_failed` + 稳定错误码 `preview_not_write`/`preview_unsupported`/`preview_failed`） |
| API 入口 | `handlers/ai/handler.go` | `POST /api/v1/agent/tools/execute` 新增可选 `dryRun`（与 `conversationId` 一并透传） |
| 测试 | `service/tool_preview_test.go`、`handlers/ai/service_b0_conversation_test.go` | 见 §3 |

## 3. 测试记录（本机）

| 用例 | 断言要点 | 结果 |
| --- | --- | --- |
| `TestPreviewTool_CreateTicketProjectsFields` | 字段投影（title/priority/category/ci_id/assignee_id）、`DryRun=true`、`Note` 含「预览不保证最终成功」、版本确定性（同输入相同 / 参数变则不同） | PASS |
| `TestPreviewTool_Guards` | 读工具 → `ErrPreviewNotWrite`；未实现预览的写工具 → `ErrPreviewUnsupported`；未知工具 → 报错 | PASS |
| `TestPreviewTool_CreateTicketRequiresTitle` | 缺 title 快速失败 | PASS |
| `TestPreviewTool_UpdateTicketRequiresTicketService` | 未装配 ticket 服务时明确失败（不静默返回空 diff） | PASS |
| `TestB0_04_DryRunProducesPreviewWithoutApproval` | 返回 `*service.ToolPreview`；记录 `dry_run=true`/`status=preview`/`needs_approval=false`/`approval_state=auto`/`conversation_id` 透传/`result` 含 `version`/元数据快照 `risk` | PASS |
| `TestB0_04_DryRunUnsupportedToolFailsWithAudit` | 读工具 dry-run → 报错 + `preview_failed` 审计 + `error_code=preview_not_write`，且 `dry_run` 不置位 | PASS |

编译/格式：`gofumpt -l ./service ./handlers/ai` 无输出；`go build ./...` exit 0。

**过程记录（真实失败与修复，保留以便复现）**
1. `service/tool_preview.go` 首版对 `*ticket.Ticket` 使用了不存在的 `Number` 字段，并直接把自定义类型 `ticket.Status` 当 `string` 用 → 编译失败；改为 `TicketNumber` 与显式转换 / `derefString`/`derefInt`（nil 安全）。
2. `handlers/ai/service.go` 新增 `errors.Is` 但漏导入 `errors` → 编译失败；补导入。
3. 测试文件内构造 `ExecuteToolOptions` 时（包外测试 `ai_test`）漏包名限定 → 编译失败；改为 `ai.ExecuteToolOptions`。

## 4. 设计取舍（登记）

1. **dry-run 不创建 pending 审批**：预览本身无副作用，若也生成 pending 会让审批口径混淆（审批的是「执行」，不是「预览」）。因此预览落 `status=preview/approval_state=auto/needs_approval=false` 的记录；真实执行仍走既有 pending → 审批 → 队列链路。B1-05 冻结参数时以「同一工具 + 同参数 + 预览 version」比对。
2. **预览范围**：本轮实现 `create_ticket`（投影）与 `update_ticket`（diff）。其余写工具（`link_ticket_ci`/`create_ticket_type`/`create_ci_relationship`/`delete_ci_relationship`）虽已标注 `SupportsDryRun`，但预览实现待 B1 按需补齐，当前返回 `ErrPreviewUnsupported`（fail-closed，不静默当成功）。
3. **快照内容**：`description` 单字段截断 2000 字符；完整脱敏档位（`strict`）由 B0-06 统一接入（当前预览只投影白名单字段，不含任意入参）。
4. **API 增量**：`dryRun` 与 `conversationId` 均为可选增量字段，旧客户端不传即保持既有行为。

## 5. 未闭环

- B1-05 的「确认单冻结执行参数」需消费本任务的 `version`；
- B1-07 前端 dry-run 预览 UI（含 BQ7 提示文案）；
- 其余写工具的预览实现（B1 按需）。

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B0-04 交付证据：预览分支、零写入保证、快照版本、测试与三次真实编译失败的修复记录 |
