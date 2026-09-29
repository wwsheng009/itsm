# B1-07 实施证据（ConfirmationDrawer 对话内确认）

> 文档类型：实施证据（任务 B1-07）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-07（确认抽屉组件；依赖 B1-04、B1-05）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-07）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M1-05 卡片边界）
> 核查方式：`tsc --noEmit` + Jest 组件测试 10 例 + 既有 AI 组件回归 34 例

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-07 | **`unit_verified`** | 新增 `ConfirmationDrawer`：脱敏参数、dry-run 预览快照、`expires_at` 倒计时、**拒绝原因必填**、通过/拒绝双动作、过期/已处理只读；接入 `AIChat`（消息内卡片出现「确认 / 拒绝」入口） |
| 数据口径 | ✅ | 与审批页**同源同形状**：数据 = `GET /agent/tools/:id`（复用卡片已拉取的详情，无二次请求）；决策 = `POST /agent/tools/:id/approve`（B1-05 状态机：过期/冲突/回放语义由后端保证） |
| 遗留 | 3 项 | ①手工 E2E 截图待补（需真实后端会话）；②`cancelled` 无触发点（B1-05 同源遗留）；③AIChat 的历史消息不重放抽屉（事件不落库的既定取舍） |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-frontend/src/components/ai/confirmation-drawer.tsx`（新建） | `ConfirmationDrawer` + 纯函数 `deriveConfirmationView`（已决 > 过期 > pending > unknown，优先后端 `confirmationState`，旧后端按 `approvalState/status` 降级）与 `formatRemaining`（天/时/分/秒粒度） |
| 2 | `itsm-frontend/src/components/ai/tool-approval-card.tsx`（修改） | 新增**可选** `onRequestConfirm`：注入时展示「确认 / 拒绝」入口（仅 `pending` 且未过期），未注入时保持一期行为；pending 文案按是否可内联确认自适应 |
| 3 | `itsm-frontend/src/components/ai/AIChat.tsx`（修改） | 消息项托管抽屉（详情来自卡片，不二次请求）；决策调用 `aiApproveTool`，错误在抽屉内可见（过期/冲突/队列不可用）；`onRequestConfirm` 作为启用开关（默认在聊天页启用） |
| 4 | `itsm-frontend/src/lib/api/ai-api.ts`（修改） | `ToolApproval` 类型补 B1-05 字段：`confirmationState?: string`、`expiresAt?: string \| null` |
| 5 | `itsm-frontend/src/components/ai/__tests__/confirmation-drawer.test.tsx`（新建） | 10 例：pending 渲染（脱敏参数/倒计时）、过期禁用、拒绝原因必填、通过、dry-run 快照、已处理只读、API 错误展示 + 三个纯函数用例 |

## 3. 交互与状态口径

```text
deriveConfirmationView(detail, now)：
  1) confirmationState ∈ {confirmed, rejected, cancelled} 或 approvalState ∈ {approved, rejected} → decided（只读）
  2) confirmationState/approvalState/status = expired，或 pending 且 now ≥ expiresAt          → expired（禁用 + 提示重新发起）
  3) confirmationState = pending 或 approvalState/status = pending                            → pending（可操作）
  4) status ∈ {done, failed, rejected}                                                       → decided
  5) 其余                                                                                     → unknown（只读）

操作面：仅 pending 时可点；拒绝按钮在原因非空前保持 disabled；提交中双按钮 loading（防重复点击）。
```

**为什么「已决」优先于「pending 兜底」**：历史数据/半途状态可能出现 `approvalState=rejected ∧ status=pending`，若按 status 判 pending 会重新打开操作面（违反「禁止重复操作」），故已决状态先行判定。

## 4. 运行记录（本机，pwsh）

```text
npx tsc --noEmit → exit 0
npx jest src/components/ai/__tests__/confirmation-drawer.test.tsx --coverage=false → PASS；10 passed
npx jest src/components/ai --coverage=false → 4 suites / 34 passed（含卡片 9、时间线 9、Markdown 既有回归）
```

## 5. 与既有契约的关系

| 契约点 | 变更 | 影响面 |
| --- | --- | --- |
| `ToolApprovalCard` 文案 | pending 文案按是否启用内联确认自适应 | 未注入 `onRequestConfirm` 的调用方（含 MCP 既有用例）文案与行为不变 |
| AIChat 待审批交互 | 新增「确认 / 拒绝」入口与抽屉 | MCP 线的写工具待审批在聊天页同样获得内联确认（见 MCP 方案变更记录）；「非 pending 只读」「禁止重复操作」不变量保持不变 |
| 数据请求 | 不增加请求数（复用卡片拉取的详情） | 无额外轮询 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-07 交付：确认抽屉（脱敏参数/预览快照/倒计时/原因必填/只读态）+ AIChat 接线；10 例组件测试 + 34 例组件回归全绿，tsc 干净；判定 `unit_verified` |
