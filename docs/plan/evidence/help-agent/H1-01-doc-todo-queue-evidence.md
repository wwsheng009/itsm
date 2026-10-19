# H1-01 遥测缺口聚类 → 文档待办队列 · 交付证据

- 日期：2026-10-18
- 任务：H1-01（方案 §5.6 / §7 P1；遥测缺口聚类 → 文档待办队列）
- 状态：`integration_verified`（数据管线 + 接口 + 前端缺口上报；管理端看板消费归 H1-04）
- 交付物：
  - 前端：`AssistantHelpPanel.tsx` 上报 `help_gap`（missing/stale 每次加载一轮；含 `coverageStatus`/`route`）
  - 后端服务：`service/ai_assistant_telemetry.go`（`AssistantHelpGapCluster` + `GetAssistantHelpGapClusters` +
    仓储 SQL `AggregateHelpGapClusters`；默认 30 天 / 50 条，上限 90 天 / 200 条）
  - 后端接口：`GET /api/v1/ai/help/todos`（`handlers/ai/help_todos.go`；`ai:read`）+ `handlers/ai/service.go` 装配
- 路由：`router/ai_routes.go`（新增后已跑 `authz-gen` 与预检新鲜度/对齐测试）

## 1. 数据流与口径

```
帮助面板（missing/stale）──help_gap──► POST /ai/telemetry/events ──► ai_telemetry_events
                                                        │
                          GET /ai/help/todos ◄── 聚类：help_gap / doc_draft_created（按 pageKey）
                                ▲
                                └── 合并 helpindex 账本缺口（missing/stale 页面；零遥测也有基础项）
```

- **聚类 SQL**：`tenant_id` 隐式条件 + `occurred_at >= NOW() - INTERVAL '1 day' * $2` + `page_key <> ''` +
  `name IN ('help_gap','doc_draft_created')`；`GROUP BY page_key`；
  排序 `gap_count DESC, COUNT(*) DESC, page_key ASC`；`LIMIT $3`。
- **待办合并与优先级**（纯函数 `buildHelpTodos`，稳定排序）：
  `missing(0) > stale(1) > unknown(2) > ok(3)`；同级按 `gapCount`、`draftCount`、`pageKey`。
- **原因码**（稳定取值）：`doc_missing` / `doc_stale` / `page_unregistered` / `user_help_gaps`。
- **降级**：埋点服务未装配 → 200 + `telemetry.available=false`，仅返回账本缺口（不 5xx）。

响应契约：

```json
{
  "days": 30,
  "items": [
    { "pageKey": "ghost.page", "docStatus": "unknown", "reason": "page_unregistered", "gapCount": 9, "draftCount": 0 },
    { "pageKey": "dashboard", "title": "仪表盘", "docStatus": "ok", "reason": "user_help_gaps",
      "gapCount": 3, "draftCount": 1, "lastSeen": "2026-10-18T02:00:00Z" }
  ],
  "totals": { "items": 2, "missing": 0, "stale": 0 },
  "telemetry": { "available": true }
}
```

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai src/lib/ai --coverage=false --watchAll=false --runInBand  # 19 suites / 140 tests
npx tsc --noEmit ; npx eslint src/components/ai/dock/AssistantHelpPanel.tsx src/components/ai/dock/__tests__/AssistantHelpPanel.test.tsx

cd ../itsm-backend
gofmt -l -w service handlers/ai router
go test ./service/ -run 'AssistantTelemetry|HelpGap' -count=1     # 含 3 个新增聚合用例
go test ./handlers/ai/ -count=1                                   # 含 3 个待办队列用例
go run ./cmd/authz-gen
go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1
```

关键断言：
- **前端**：缺失页面上报 `help_gap`（`pageKey=dashboard, route=/dashboard, coverageStatus=missing`）；已覆盖页面不上报。
- **服务层**（sqlmock）：`days/limit` 默认值与上限钳制（0→30/50；1000/500→90/200）；扫描两行聚类、
  `lastSeen` 为 RFC3339 UTC、NULL 时间省略；未装配仓储 → `ErrAssistantTelemetryUnavailable`。
- **待办合并**：未注册页面（unknown）排在「已覆盖但有求助缺口」（ok）之前；`limit` 截断；原因码正确。
- **端点**：`days=30` 默认、`telemetry.available=true` 且 items 顺序/计数与聚类一致；
  无遥测装配 → `available=false` + 空数组（非 null）+ `limit=1000` 钳制不报错。

## 3. 边界与条件项（不阻塞）

- `doc_draft_created` 目前无前端上报点（草稿完成由对话链路工具事件覆盖）；聚合侧已支持，待 H1-04/产物链路接入；
- 待办「认领/忽略/导出」等状态持久化归 H1-04（覆盖率看板）设计，需新表/迁移时单独立项；
- `help_list_gaps` 工具仍按内嵌账本（静态）返回；如需把遥测聚类带入工具面，可在 H1-04 同批评审（当前避免工具面
  依赖实时遥测，保持数据面只读稳定）；
- 真实浏览器 E2E（missing 页面触发 help_gap → 队列出现该 pageKey）归联调批。