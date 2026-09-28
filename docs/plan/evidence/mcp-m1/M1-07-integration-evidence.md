# M1-07 证据（审计页来源维度）

> 验收项：A1-07（对应任务 M1-07）｜DoD：`flow_verified`
> 实际状态：**`integration_verified`**（全量拉取/筛选下发/列与详情渲染/空态回显全绿；`flow_verified` 待 M1-10 联调与浏览器验证）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13 / jest（jsdom + @testing-library/react），基线 `98b31057`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 数据源判定（先纠正一个易错前提）

审计页原有内容读的是 **AI 场景审计日志**（`GET /api/v1/ai/audit-logs` → `audit_logs.item_type='ai_audit'`，字段是 scenario/model/confidence 等），**不含工具调用三元组**。M1-07 要求的「按 provider / 服务器筛选 + 三元组 + 耗时 + 错误码」对应的是 **`tool_invocations`**（M0-11 扩展的那张表）。因此本任务**不改造 AI 场景审计表**，而是在审计页新增「工具调用审计（来源维度）」区块，直接复用 M1-06 的列表接口与筛选能力（`GET /api/v1/agent/tools/invocations`）。

## 交付物

| 文件 | 变更 |
| --- | --- |
| `handlers/ai/handler.go` | 列表接口新增 `?state=all` 语义 = **不限审批状态**（审计视图），与「未传参=待审批」区分开；响应回显 `state/provider/server` |
| `handlers/ai/handler_tool_source_test.go` | 新增断言：`state=all` 跨状态可见（3 待审批 + 1 已通过 = 4 条），`state=approved` 收敛到 1 条 |
| `itsm-frontend/src/components/ai/tool-invocation-detail.tsx`（新增） | 审批页/审计页**共用**的展示件：`formatToolSource`（来源口径，含老记录退化）、`riskColor`、`prettyArgs`、`ToolSourceTag`、`ToolInvocationDetail`（三元组 + 角色快照 + 权限校验 + 审批人时间 + 耗时 + 错误码 + 结果摘要 + 脱敏参数） |
| `itsm-frontend/src/pages/(main)/ai/approval/index.tsx` | 改为消费共用展示件（删除页面内重复实现；`formatSource` 保留为 `formatToolSource` 的再导出，口径单一来源） |
| `itsm-frontend/src/pages/(main)/ai/audit/index.tsx` | 新增「工具调用审计（来源维度）」卡片：来源/服务器/状态筛选（服务器选项取自治理面，失败退化为手填 + Enter 生效）、列（时间/来源/工具含原始名/风险/状态/耗时/错误码）、展开详情、空态写出生效筛选、`mcp:admin` 才显示治理跳转 |
| `itsm-frontend/src/pages/(main)/ai/audit/__tests__/index.test.tsx`（新增） | 2 用例：默认全量与列/详情渲染、来源筛选下发与空态回显 |

## 口径与取舍

| 项 | 口径 |
| --- | --- |
| 默认视图 | `state=all`（跨审批状态全量），符合「审计」语义；审批页默认仍是 `pending`（待办语义） |
| 过滤位置 | 全部在后端（`ToolInvocationFilter`），前端不二次筛选；空态回显**生效**的后端过滤值 |
| 脱敏 | 详情只展示 `argsRedacted`、`outputSummary`；原始 `arguments` 从不回显（后端 `toolInvocationItem` 不返回该字段）；用例断言页面文本不含未脱敏明文 |
| 展示件复用 | 审批页与审计页共用 `ToolInvocationDetail`，避免「同一字段两种解释」；后续 M2 若要加字段只需改一处 |
| 权限 | 治理跳转仅 `mcp:admin`；审计数据本身受 `ai:read` 路由权限保护（不额外放宽） |

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./handlers/ai/ -run TestListToolInvocations -v` | PASS（含 `state=all` 跨状态断言） |
| 2 | `npx jest --testPathPattern "ai/audit"` | **2/2 passed** |
| 3 | `npx jest --testPathPattern "ai/approval"`（共用件重构后回归） | **5/5 passed** |
| 4 | `npx tsc --noEmit` | exit 0 |

## 已知缺口 / 观察

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | 审计页筛选未写 URL query | 与 M1-06 同一缺口（刷新不持久、不能分享链接）；如需做，应统一在两个页面加 URL 同步（建议单开小任务，避免两个页面各写一套） |
| 2 | 分页 | 工具调用审计沿用前端分页（`pageSize=20`）；`tool_invocations` 尚无服务端分页参数，数据量大时需补 `page/pageSize`（**不阻塞 M1**，M2 运维加固时可评估） |
| 3 | 与 AI 场景审计的联动 | 两者仍是两张独立表/两个区块；跨表关联（如从一次对话跳到其工具调用）未做 |
| 4 | `ticket-attachment-api.test.ts` 预存在失败 | 仍为他人附件 WIP 的既有失败，与本任务无关 |
