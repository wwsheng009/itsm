# H1-04 帮助覆盖率看板（管理页） · 交付证据

- 日期：2026-10-18
- 任务：H1-04（方案 §5.3 覆盖率账本 / §7 P1；帮助覆盖率看板）
- 状态：`integration_verified`（后端数据面 + 前端看板 + 路由/菜单 + 用例；真实 PG 菜单冒烟与浏览器 E2E 归联调）
- 交付物：
  - 后端：`internal/helpindex/index.go`（`Pages()` / `OrphanDocs()` 访问器 + 账本 `orphanDocs` 解析）、
    `handlers/ai/help.go`（`GET /ai/help/coverage`）、`router/ai_routes.go`（`ai:read`）
  - 前端：`lib/api/ai-api.ts`（`aiGetHelpCoverage` + `aiGetHelpTodos` 及契约类型）、
    `pages/(main)/ai/help-coverage/index.tsx`（看板页）、`routes/index.tsx` + `routes/route-paths.ts`
  - 迁移：`migrations/20261018_add_ai_help_coverage_menu.sql`（菜单「帮助覆盖率」`/ai/help-coverage`，`ai:read`，sort 118）

## 1. 看板内容（数据全部只读）

| 区块 | 数据源 | 说明 |
|---|---|---|
| 覆盖率统计 | `/ai/help/coverage`（内嵌账本） | 覆盖率 / 页面总数 / 缺失 / 需复核 / 孤立文档五张卡 |
| 文档待办队列 | `/ai/help/todos`（H1-01） | 账本缺口 ∪ 近 30 天 `help_gap` 聚类；原因中文标签、缺口数、出稿数、最近时间 |
| 页面覆盖清单 | `/ai/help/coverage` | 页面键/标题/路由/状态 Tag/关联文档路径；状态筛选（全部/缺失/需复核/已覆盖）+ 关键词搜索 |
| 降级路径 | — | 待办失败 → 「仅显示账本覆盖」提示；`telemetry.available=false` → 「仅账本缺口」提示；账本失败 → 顶部错误提示 |
| orphan 文档 | 账本 `orphanDocs` | 非空时 Alert 列出 文档 → 未知 page_key |

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l -w internal/helpindex/index.go internal/helpindex/index_test.go handlers/ai/help.go handlers/ai/help_handler_test.go router/ai_routes.go
go test ./internal/helpindex/ ./handlers/ai/ -count=1     # 全 ok（新增 Pages/OrphanDocs 一致性与端点契约用例）
go run ./cmd/authz-gen
go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1

cd ../itsm-frontend
npx jest "src/pages/\(main\)/ai/help-coverage" "src/pages/\(main\)/ai/observability" --coverage=false --watchAll=false --runInBand
# → 2 suites / 11 tests（看板 6 例 + 观测回归 5 例）
npx jest src/routes --coverage=false --watchAll=false --runInBand          # 3 suites / 30 tests
npx jest src/lib/api/__tests__/ai-api.test.ts --runInBand                  # 53 tests
npx tsc --noEmit ; npx eslint "src/pages/(main)/ai/help-coverage/**" src/lib/api/ai-api.ts src/routes/index.tsx src/routes/route-paths.ts
```

页面关键断言（`help-coverage/__tests__/index.test.tsx`，6 例）：
- 渲染 `95.2%` 覆盖率、页面总数卡、页面清单（页面键 + 文档路径 `docs/help/tickets/detail.md`）；
- 待办队列展示「文档缺失」与最近时间；
- 状态筛选「缺失」→ 已覆盖页从清单消失；
- 待办接口失败 → `help-todos-unavailable` 降级提示且账本仍渲染；
- `telemetry.available=false` → 仅账本缺口提示；
- orphan 文档 Alert 展示 `gone.page`；辅助函数（百分比/原因标签）。

后端关键断言：`Pages()` 数与 `totals.pages` 一致、`ok` 数与账本一致、`OrphanDocs()` 数与 `totals.orphan` 一致、
返回副本（排序不污染索引）；覆盖率端点缺租户 → 401、正常 → 200 + 覆盖率>0 + 页面清单完整字段。

## 3. 边界与条件项（不阻塞）

- **菜单迁移真实 PG 冒烟未跑**：本机未连生产库；SQL 沿用 O0-03 已在真实 PG 验证过的
  「NOT EXISTS 守卫 + 以 `/ai/chat` 行为模板」模式（该模式已实测幂等）；联调时按 O0-03 同口径补冒烟；
- **待办认领/忽略状态持久化**（新表 + 迁移 + 接口）未在本批交付，单独立项；当前看板为只读消费；
- 新页面文案为静态中文（i18n 与帮助视图同批条件项）；
- 浏览器级 E2E（登录态打开 `/ai/help-coverage` → 统计/清单/待办渲染 + 菜单命中）归联调批；
- 页面源码级 stale 映射（H1-03 条件项）与本看板无耦合，可在后续批次共同评审。