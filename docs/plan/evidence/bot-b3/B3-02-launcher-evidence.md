# B3-02 实施证据（页面 launcher 组件与三处接入）

> 文档类型：实施证据（任务 B3-02）
> Status: draft
> 编制日期：2026-09-27
> 任务：B3-02（工单/事件/CI 详情页「问 AI」入口，携带上下文打开工作区；G9/G10）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.4 B3-02、§5.2 AB3-02）
> 核查方式：TypeScript 类型检查 + Jest/jsdom 组件与契约测试 + ESLint + 受控页面回归

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B3-02 | **`unit_verified`** | launcher 组件 + 路由 state 契约 + 工作区消费（请求回传/上下文条）+ 三处页面接入完成 |
| AB3-02 | ✅ | 组件测试（携带参数/权限态）+ 契约测试（构建/校验/请求片段）；手工截图归 BT-09 |
| 渐进增强 | ✅ | 入口为页头附加按钮，不改动页面主流程布局；无 AI 读权限时**置灰 + Tooltip**（可选 hidden） |

## 2. 交付物

| # | 文件 | 内容 |
| --- | --- | --- |
| 1 | `itsm-frontend/src/lib/ai/ask-ai-scope.ts`（新建） | 入口枚举（与后端 `KnownEntrypoints` 对齐）+ 目标类型 + 路由 state 契约：`buildAskAIState` / `readAskAIScope`（**一律校验**：来源/枚举/成对/正整数/截断）/ `buildAskAIRequestScope`（仅非空字段）/ 入口标签 |
| 2 | `itsm-frontend/src/components/ai/AskAILauncher.tsx`（新建） | 「问 AI」按钮：`navigate('/ai/chat', { state })`；`denied` → 置灰 + Tooltip（`denyMode='hidden'` 时整块不渲染）；`onNavigate` 供测试注入 |
| 3 | `itsm-frontend/src/components/ai/AIChat.tsx` | 读 `location.state` → 校验后作为**请求级**入口上下文：随 `aiChatStream` 与降级 `AIApi.chat` 回传；工作区右上角渲染上下文条（`entrypoint · targetType#id`，可关闭 → 关闭后按普通对话处理） |
| 4 | `itsm-frontend/src/lib/api/ai-api.ts` | `AIChatStreamRequest` 与 `AIApi.chat` 参数新增 `entrypoint/targetType/targetId/summary`；仅非空时落字段（缺省请求体与现状**逐字节一致**） |
| 5 | `itsm-frontend/src/components/ticket/TicketDetail.tsx` | 页头接入：`ticket_detail` + `ticket#<id>` + 标题摘要；`denied = !hasPermission('ai:read')` |
| 6 | `itsm-frontend/src/pages/(main)/incidents/$id/index.tsx` | 页头（返回按钮旁）接入：`incident_detail` + `incident#<id>` |
| 7 | `itsm-frontend/src/components/cmdb/ci-detail/CIDetail.tsx` | 页头（返回列表按钮旁）接入：`ci_detail` + `ci#<id>` + 名称摘要 |

> 三处目标 ID 均取自**当前页面已加载成功的对象**（不构造、不猜测）；服务端 B3-01 会再次解析并做对象存在性 + 读权限预检（前端值只是提示）。

## 3. 契约要点

| 场景 | 行为 |
| --- | --- |
| `location.state` 缺省/非法（来源不符、入口未知、目标不成对、ID 非正整数） | 一律按「无上下文」处理（返回 null，不抛错、不部分采纳） |
| `chat` + 无目标 | 请求片段为空对象 → 请求体与现状一致（零破坏） |
| 非 chat 入口或带目标 | 请求携带 `entrypoint`（非 chat 时）与 `targetType/targetId/summary`（成对且非空时） |
| summary 超长 | 前端按 300 字符截断（与后端 300 rune 同口径；后端再截断兜底） |
| 上下文条关闭 | 仅清除本次会话的**请求回传**（`scopeDismissed`），不改写路由（刷新后按 state 重新生效） |
| 权限预检 | 页面以 `hasPermission('ai:read')` 判定；无权限 → 置灰 + Tooltip「无 AI 使用权限」 |

## 4. 测试与运行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `npx tsc --noEmit` | ✅ 0 错误 |
| 2 | `npx jest src/lib/ai/__tests__/ask-ai-scope.test.ts src/components/ai/__tests__/ask-ai-launcher.test.tsx` | ✅ 2 套件 / **16 用例** |
| 3 | `npx jest src/components/ticket/__tests__/TicketDetailAssignSearch.test.tsx src/components/cmdb/ci-detail/__tests__/CIDetail.test.tsx` | ✅ 2 套件 / 8 用例（见 §5-1） |
| 4 | `npx eslint`（8 个改动文件） | ✅ 0 error（原 1 warning 已修） |
| 5 | `npx jest src/components/ai src/lib/ai src/lib/api/__tests__/ai-api.test.ts` | 见提交记录（AI 组件与 API 面回归） |

用例清单（文件 2）：

- 契约（`ask-ai-scope.test.ts`，12 例）：往返一致 / 无目标合法 / 来源不符 / 未知入口 / 目标不成对 / 目标类型未知 / ID 非正整数（5 种）/ 超长截断 / 请求片段三态 / 入口标签；
- 组件（`ask-ai-launcher.test.tsx`，5 例）：跳转携带上下文（含/不含目标）/ 拒绝态置灰不可点 / `denyMode=hidden` 不渲染 / 自定义文案 + `onNavigate`。

## 5. 遗留与登记

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | O-3（既有不稳定用例）：`TicketDetailAssignSearch` 在**双套件并发**运行时曾失败 1 次（`worker process failed to exit gracefully` 警告同现），单独运行与复跑均通过；本次改动对该用例唯一影响是 mock 需补 `hasPermission`（已补，语义为「有权限」）。 | 归入既有 flake 治理；B3-07 复跑观察 |
| 2 | 手工浏览器截图（三处入口可见性 + 上下文条 + 无权限置灰） | **BT-09**（需真实后端 + 登录会话）；B3-07 汇总条件项 |
| 3 | 列表页入口（`ticket_list`）与事件创建页入口（`incident_create`） | 本期未接入（枚举已就绪，接入零成本）；列入 B4 场景增强候选 |
| 4 | 上下文条措辞尚未进 i18n 词条池（中文硬编码，与既有 AI 组件同风格） | i18n 统一补齐任务 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B3-02 交付：路由 state 契约 + launcher 组件 + 工作区消费（请求回传/上下文条）+ 工单/事件/CI 三处接入；tsc/eslint 0 错误，契约 12 例 + 组件 5 例 + 受控页面回归通过；判定 `unit_verified`（手工截图归 BT-09） |
