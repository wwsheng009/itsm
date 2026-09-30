# Bot E2E run-summary（模板）

> 由 `e2e/run-bot-e2e.ps1` 生成；`<...>` 为占位符。归档路径：`docs/plan/evidence/bot-b4/run-summary-<yyyyMMdd-HHmmss>.md`。

## 0. 概要

| 项 | 值 |
| --- | --- |
| 运行时间 | `<yyyy-MM-dd HH:mm:ss zzz>` |
| 通道 | `<api\|browser\|all>` |
| 提交 | `<git rev-parse --short HEAD>`（分支 `<branch>`；工作树 `<clean\|dirty>`） |
| 环境 | Go `<go version>`；Node `<node -v>`；OS `<os>` |
| 结论 | `<通过/部分通过/失败>`（失败项见 §3） |

## 1. 结果总表

| # | 通道 | 目标 | 命令 | 结果 | 耗时 |
| --- | --- | --- | --- | --- | --- |
| A-1 | api | `./tests/botintegration/`（B0） | `go test ./tests/botintegration/ -run TestB0 -count=1` | `<ok/FAIL>` | `<x.xs>` |
| A-2 | api | `./tests/botintegration/`（B1） | `go test ./tests/botintegration/ -run TestB1 -count=1` | `<...>` | `<...>` |
| A-3 | api | `./tests/botintegration/`（B2） | `go test ./tests/botintegration/ -run TestB2 -count=1` | `<...>` | `<...>` |
| A-4 | api | `./tests/botintegration/`（B3） | `go test ./tests/botintegration/ -run TestB3 -count=1` | `<...>` | `<...>` |
| A-5 | api | `./handlers/ai/`（SSE 兼容） | `go test ./handlers/ai/ -run 'TestSSERegistry\|TestToolEvents' -count=1` | `<...>` | `<...>` |
| A-6 | api | `./service/bot/` | `go test ./service/bot/ -count=1` | `<...>` | `<...>` |
| W-1..3 | browser | `flow-bot-workspace.spec.ts` | `npx playwright test tests/e2e/flows/flow-bot-workspace.spec.ts --project=chromium` | `<passed/failed/skipped>` | `<...>` |

## 2. 通道摘要

- **api**：`<n>` 组通过 / `<m>` 组失败（明细见 §1）。
- **browser**：`<n>` 通过 / `<m>` 失败 / `<k>` skip（skip 原因：`<...>`）。

## 3. 失败与 skip 明细

| 项 | 类型 | 摘要 | 关联缺陷/登记 |
| --- | --- | --- | --- |
| `<A-x/W-x>` | `<失败/skip>` | `<错误摘要>` | `<issue/证据链接>` |

## 4. 遗留与后续

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | `<...>` | `<...>` |

## 5. 原始日志

- api：`<命令与退出码清单>`
- browser：`<playwright-report 路径>`（CI artifact）
