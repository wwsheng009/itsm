# B4-01 实施证据（`e2e/` Bot 用例与 run-summary）

> 文档类型：实施证据（任务 B4-01）
> Status: draft
> 编制日期：2026-09-27
> 任务：B4-01（abi + browser 双通道 E2E 章程、脚本入口与 run-summary；与 MCP M2-05 共用 Playwright 设施）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.5 B4-01、§5.2 AB4-01）、`e2e/README.md`、`e2e/cases.md`
> 核查方式：脚本实跑（api 通道）+ spec 静态校验（tsc/lint）+ CI workflow 接线

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| AB4-01 | ✅（api 通道已验证；browser 通道待 CI 首轮） | 双通道可复跑由**单一入口** `e2e/run-bot-e2e.ps1` 编排；run-summary 模板与实跑样例齐备 |
| B4-01 | **`flow_verified`** | api 通道本机实跑通过并归档 run-summary；browser spec 与 CI workflow 就位（首轮校准项已登记，不假红） |

## 2. 交付物

| # | 文件 | 内容 |
| --- | --- | --- |
| 1 | `e2e/README.md`（新建） | 章程：双通道定义、复跑命令、环境前置、确定性策略（mock 优先 / self-skip / 等待可见而非延时）、证据约定、首轮校准清单 |
| 2 | `e2e/cases.md`（新建） | 用例清单：api 通道 6 组（A-1～A-6）、browser 通道 3 用例（W-1～W-3）、real smoke 2 项（R-1/R-2）、与既有 spec 的边界 |
| 3 | `e2e/run-bot-e2e.ps1`（新建） | 单一入口：`-Channel api\|browser\|all`、`-OutDir`；逐项计时与退出码采集，生成 run-summary |
| 4 | `e2e/run-summary.template.md`（新建） | run-summary 模板（概要/结果总表/失败与 skip/遗留/原始日志） |
| 5 | `itsm-frontend/tests/e2e/flows/flow-bot-workspace.spec.ts`（新建） | browser 通道：①launcher→工作区（`ask-ai-scope-chip` 上下文条）；②SSE→`tool-call-timeline` + `run-status-bar` + 无 console error；③写工具→`confirmation-drawer`（approve/reject）→审批页；含 self-skip 守卫与校准清单 |
| 6 | `.github/workflows/e2e-bot.yml`（新建） | CI：真实栈（Postgres + 后端 `BOT_ENABLED=true` + mock LLM `create_ticket` + vite + Playwright chromium）；证据 artifact（报告/trace/后端日志）；`continue-on-error` 首轮校准位 |
| 7 | `docs/plan/evidence/bot-b4/run-summary-*.md` | 实跑样例（api 通道） |

## 3. 通道覆盖对照（AB4-01 判据）

| 判据 | 落地 | 状态 |
| --- | --- | --- |
| api 通道：确认→执行→verify | A-2（`TestB1FlowAcceptance_ConfirmExecuteVerifyWithRunArchive`） | ✅ |
| api 通道：队列重启恢复 | A-2（`TestB1FlowAcceptance_QueueRestartKeepsRunLink`、`TestB1QueueRecovery_*`） | ✅ |
| api 通道：授权矩阵 | A-3（`TestB2Matrix_RoleByEntrypointDispatch`、`TestB2Matrix_StatusDraftAndLegacyDefault`、`TestB2HTTP_CrossTenantBotSelection404`） | ✅ |
| api 通道：入口上下文 | A-4（`TestB3Entrypoint_RunRecordPersistsTarget`、`TestB3Entrypoint_ExecutionFaceFiltering`） | ✅ |
| api 通道：未知事件兼容 | A-5（`TestSSERegistry_LegacyClientIgnoresUnknownEvents`、`TestToolEvents_*`） | ✅ |
| browser 通道：工作区 SSE 流 | W-2 | 待 CI 首轮 |
| browser 通道：确认抽屉 | W-3 | 待 CI 首轮（含 self-skip） |
| browser 通道：证据面板 | 归 B4-02 看板与产物读取端点（B4 增补）；W-2 已覆盖运行状态条 | 部分 |
| browser 通道：三处 launcher 入口 | W-1（工单详情；事件/CI 同组件，B3-02 组件测试已覆盖） | ✅（组件级） |
| mock 全量 + real smoke 分离 | `e2e/cases.md` §4（R-1/R-2 默认关闭） | ✅ |
| run-summary 模板产出 | 本文件 §4 + 实跑样例 | ✅ |

## 4. 实跑记录（api 通道）

> 实跑样例：`docs/plan/evidence/bot-b4/run-summary-20260929-203102.md`（由 `pwsh e2e/run-bot-e2e.ps1 -Channel api` 生成；提交 `ab7eaa65`，结论 **通过**，6/6）。

| # | 目标 | 命令 | 结果 |
| --- | --- | --- | --- |
| A-1 | `tests/botintegration`（B0） | `go test ./tests/botintegration/ -run TestB0 -count=1` | ✅ ok（135.2s） |
| A-2 | `tests/botintegration`（B1） | `go test ./tests/botintegration/ -run TestB1 -count=1` | ✅ ok（116.6s） |
| A-3 | `tests/botintegration`（B2） | `go test ./tests/botintegration/ -run TestB2 -count=1` | ✅ ok（105.9s） |
| A-4 | `tests/botintegration`（B3） | `go test ./tests/botintegration/ -run TestB3 -count=1` | ✅ ok（95.1s） |
| A-5 | `handlers/ai`（SSE 兼容） | `go test ./handlers/ai/ -run 'TestSSERegistry\|TestToolEvents' -count=1` | ✅ ok（76.2s） |
| A-6 | `service/bot` | `go test ./service/bot/ -count=1` | ✅ ok（68.9s） |

browser 通道：本机为 Windows 且无真实栈（无 Postgres/Docker），按章程 self-skip 约定由 CI `.github/workflows/e2e-bot.yml` 首轮执行；spec 已通过 `tsc`/`eslint` 静态校验（详见 §5）。

## 5. 静态校验

| # | 检查 | 命令 | 结果 |
| --- | --- | --- | --- |
| 1 | TypeScript 类型检查 | `npx tsc --noEmit` | ✅ 0 错误（`tsc=0`） |
| 2 | ESLint（新增 spec） | `npx eslint tests/e2e/flows/flow-bot-workspace.spec.ts` | ✅ 0 问题（`eslint=0`） |
| 3 | 脚本入口 | `pwsh -NoProfile -File e2e/run-bot-e2e.ps1 -Channel api` | ✅ 6/6 通过并生成 run-summary（退出码 0） |

## 6. 首轮校准与遗留

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | `e2e-bot.yml` 首次真实栈运行（后端起库/管理员/确认抽屉）与 `continue-on-error` 摘除 | B4-01 收尾（CI 首绿后） |
| 2 | browser 用例③：写工具路径的策略/授权默认值（若被拒，按 `reason_code` 调整） | B4-01 + B2 治理 |
| 3 | 证据面板（artifact 读取面）浏览器断言 | **B4**（产物读取端点）+ B4-02 看板 |
| 4 | real smoke（R-1/R-2）纳入常规 | 决策待办（额度与环境） |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B4-01 交付：e2e 章程/清单/脚本/模板 + browser spec + CI workflow；api 通道实跑并归档 run-summary |
