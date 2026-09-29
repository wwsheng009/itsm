# ITSM Bot E2E 章程（B4-01）

> 文档类型：测试章程（E2E 编排与证据约定）
> Status: draft
> 编制日期：2026-09-27
> 适用范围：ITSM Bot（阶段一方案 B0–B4）的端到端验证编排
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.5 B4-01、§5.2 AB4-01）、`docs/plan/evidence/bot-b4/`
> 协同：MCP 方案 M2-05（`itsm-frontend/tests/e2e/flows/flow-mcp-admin.spec.ts`、`.github/workflows/e2e-mcp.yml`）——共用 Playwright 设施与 mock LLM

## 1. 目标与非目标

**目标**：把 Bot 的端到端验证收口为**可复跑的单一入口**（双通道），并产出统一的 run-summary 证据。

| 通道 | 覆盖 | 运行方式 |
| --- | --- | --- |
| `api` | 后端集成面：B0 元数据/dry-run、B1 确认→执行→verify 与 run 归档、B2 授权矩阵与审计、B3 入口上下文，以及 SSE 未知事件兼容 | `go test`（`tests/botintegration` + 相关包） |
| `browser` | 浏览器面：详情页 launcher → 工作区（入口上下文）、SSE 流与工具时间线、写工具确认抽屉、审批/审计页 | Playwright（`itsm-frontend/tests/e2e/flows/flow-bot-workspace.spec.ts`） |

**非目标**：替代单元/组件测试；替代真实模型质量评估（real smoke 只验证链路连通，不评估回答质量）。

## 2. 入口与复跑

```powershell
# 全部（api + browser）
pwsh e2e/run-bot-e2e.ps1

# 仅 api 通道（无需前端/浏览器；本机即可跑）
pwsh e2e/run-bot-e2e.ps1 -Channel api

# 仅 browser 通道（需真实栈：后端 :8090 + vite :3000 + 账号）
pwsh e2e/run-bot-e2e.ps1 -Channel browser

# 指定输出目录（默认 docs/plan/evidence/bot-b4/）
pwsh e2e/run-bot-e2e.ps1 -OutDir docs/plan/evidence/bot-b4
```

脚本产物：`run-summary-<yyyyMMdd-HHmmss>.md`（模板见 `e2e/run-summary.template.md`），含每通道逐项结果、耗时、失败摘要与遗留登记。

## 3. 环境前置

| 通道 | 前置 |
| --- | --- |
| api | Go 工具链；无外部依赖（ent/sqlite 内存库；mock 工具源） |
| browser | 后端 `bot.enabled=true`；mock LLM（`LLM_PROVIDER=mock` ∧ `LLM_MOCK_ENABLED=true` ∧ `LLM_MOCK_TOOL_NAME=<工具>`）；vite dev :3000（`ITSM_BACKEND_URL` → :8090）；admin 账号（`AdminProd2026!`，与 `tests/e2e/fixtures/auth.ts` 对齐）；种子数据（至少一条工单） |

CI：`.github/workflows/e2e-bot.yml`（真实栈，含 mock LLM；首轮校准后转阻断门）。

## 4. 用例清单

见 `e2e/cases.md`（api 通道按包/用例名登记；browser 通道按 spec/用例登记，含 mock 全量与 real smoke 标注）。

## 5. 确定性与假红防护

- **mock 优先**：默认全部用例走 mock provider（`LLM_PROVIDER=mock`）与 mock MCP 服务器，不依赖模型额度。
- **self-skip 而非假红**：路由未注册（404/503）、登录无 token、无种子数据、mock 工具非写工具等前置缺失时，用例显式 `test.skip` 并给出原因。
- **等待可见而非固定延时**：一律 `expect(...).toBeVisible({ timeout })` / `waitForURL`，禁止 `sleep` 作为断言依据。
- **未知事件忽略**：v2 事件叠加后，旧解析器必须静默忽略（api 通道 `handlers/ai::TestSSERegistry_LegacyClientIgnoresUnknownEvents`；browser 通道用例②断言无 console error）。

## 6. 证据约定

| 产物 | 位置 | 生成者 |
| --- | --- | --- |
| run-summary | `docs/plan/evidence/bot-b4/run-summary-<ts>.md` | `e2e/run-bot-e2e.ps1` |
| Playwright 报告/截图/trace | CI artifact（`itsm-frontend/playwright-report`、`/tmp/itsm-playwright-results`） | `npx playwright test` |
| 后端/前端日志 | CI artifact（`/tmp/backend.log` 等） | workflow |

## 7. 首轮校准清单（CI 首次运行后逐条勾除）

1. `e2e-bot.yml` 的后端起库/迁移/种子与管理员创建流程（沿用 `e2e-mcp.yml` 的校准结果）；
2. browser 用例①：工单列表行选择器与详情页 URL 形态；
3. browser 用例②：对话输入框 placeholder 与发送方式（Enter / 按钮）；
4. browser 用例③：写工具路径下确认抽屉是否出现；若被策略拒绝，按后端日志校正 seed 的授权/白名单；
5. `run-bot-e2e.ps1` 在 CI（bash runner）中的调用方式（workflow 直接调用 `npx playwright test`，脚本仅用于本机/证据归档）。

## 8. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | 建立双通道章程、脚本入口、run-summary 模板与用例清单；browser spec 首版（含校准清单） |
