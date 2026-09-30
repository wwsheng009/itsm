# Bot E2E 用例清单（B4-01）

> 与 `e2e/README.md` 配套；`mock` 列标注是否依赖 mock 替身，`real smoke` 列标注是否需要真实 provider/MCP。

## 1. api 通道（`go test`）

| # | 目标 | 覆盖 | 结果判据 |
| --- | --- | --- | --- |
| A-1 | `./tests/botintegration/`（B0） | 元数据全量标注、dry-run 零写入、严格脱敏档与 MCP 标注 | `ok` |
| A-2 | `./tests/botintegration/`（B1） | 确认→执行→verify + run 归档、队列重启恢复（不重复执行）、确认状态机（过期/重放/冲突）、预算中止 | `ok` |
| A-3 | `./tests/botintegration/`（B2） | 角色×入口授权矩阵、草稿/GA 模板、审计执行面、跨租户 404 | `ok` |
| A-4 | `./tests/botintegration/`（B3） | 入口 target 落库、执行面入口过滤（chat 被拒留痕） | `ok` |
| A-5 | `./handlers/ai/` | SSE 注册表与**未知事件忽略**（旧客户端兼容）、工具事件双名叠加 | `ok` |
| A-6 | `./service/bot/` | 策略四重交集、黑名单、幂等、产物隔离、场景种子幂等与边界 | `ok` |

## 2. browser 通道（Playwright，`itsm-frontend/tests/e2e/flows/flow-bot-workspace.spec.ts`）

| # | 用例 | 覆盖 | mock | real smoke | 失败即 |
| --- | --- | --- | --- | --- | --- |
| W-1 | ① 详情页 launcher 打开工作区并携带入口上下文 | `AskAILauncher` → `/ai/chat` → `ask-ai-scope-chip` | ✅ | ⬜（可选） | 阻断 |
| W-2 | ② 对话触发工具调用并渲染时间线与运行状态 | SSE `tool_call_*` → `tool-call-timeline`；`run-status-bar`；无 console error | ✅ | ⬜（可选） | 阻断 |
| W-3 | ③ 写工具触发确认抽屉，确认入口可跳转审批页 | 写工具 Gate3 → `confirmation-drawer`（approve/reject 可见）→ 审批页 | ✅ | ⬜ | self-skip（工具非写/被策略拒绝时） |

## 3. 与既有 spec 的边界

| 既有 spec | 归属 | 与 B4-01 的关系 |
| --- | --- | --- |
| `flow-ai-chat-stream.spec.ts` | 既有 AI 对话（阶段 0 遗产） | 只读工具与历史持久化；B4-01 不重复覆盖 |
| `flow-ai-tool-rbac.spec.ts` | 既有工具 RBAC | 角色可见性；B4-01 的授权矩阵以 api 通道为准（A-3） |
| `flow-mcp-chat-chain.spec.ts` | MCP 方案 M2-05 | 外部工具链路；B4-01 覆盖内置工具与确认抽屉，二者互补 |

## 4. real smoke（可选，默认关闭）

| # | 用例 | 前置 | 说明 |
| --- | --- | --- | --- |
| R-1 | 真实 provider 下单轮问答 + 一次只读工具调用 | 配好 provider 密钥 | 只验链路连通与超时/错误提示，不评估质量（阶段一报告 §7-10） |
| R-2 | 真实 MCP 服务器工具调用 | 配好 `mcp.enabled=true` + 一台服务器 | 与 MCP 方案 M2-05 的 CI 互补 |

## 5. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | api 通道 6 组目标 + browser 通道 3 用例 + real smoke 2 项登记 |
