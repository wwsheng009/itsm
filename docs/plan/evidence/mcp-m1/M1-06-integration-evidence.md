# M1-06 证据（审批页来源增强）

> 验收项：A1-06（对应任务 M1-06）｜DoD：`flow_verified`
> 实际状态：**`integration_verified`**（筛选/展示/权限隐藏/降级全绿；`flow_verified` 待 M1-10 的审批页 E2E 合流）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13 / jest（jsdom + @testing-library/react），基线 `2e8b46ae`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 变更 |
| --- | --- |
| `handlers/ai/entity.go` | 新增 `ToolInvocationFilter`（State/Provider/Server，零值=不过滤） |
| `handlers/ai/repository.go` · `repository_impl.go` | `ListToolInvocations` 由「state 字符串」改为「filter 结构体」；`provider` 走 `ProviderEQ`、`server` 走 `McpServerNameEQ`，**过滤在数据库层**完成（前端不做二次筛选，避免分页/计数口径不一致） |
| `handlers/ai/handler.go` | 列表接口支持 `?provider=builtin\|mcp`（非法值 400，不静默返回空表）与 `?server=<标识>`；响应回显生效过滤条件 |
| `itsm-frontend/src/lib/api/ai-api.ts` | `aiGetToolApprovals(state, filter)` + `ToolApprovalListFilter`；列表响应新增 `provider/server` 回显字段 |
| `itsm-frontend/src/pages/(main)/ai/approval/index.tsx` | 新增来源/服务器筛选（服务器选项取自治理面 `mcpApi.listServers`，拉取失败退化为手填标识）、`来源` 与 `风险` 列、`工具` 列展示可调用名 + 原始名、详情展开完整三元组与执行字段、空态含生效筛选、`工具治理` 跳转（**无 `mcp:admin` 不渲染**） |
| `handlers/ai/handler_tool_source_test.go` · `itsm-frontend/src/pages/(main)/ai/approval/__tests__/index.test.tsx` | 后端筛选/隔离用例 + 前端筛选/详情/权限隐藏用例 |

## 行为口径

| 维度 | 口径 |
| --- | --- |
| 来源筛选 | `全部来源 / 内置工具 / MCP 外部工具` → `?provider=`；切回内置时自动清空服务器条件（内置记录无服务器维度） |
| 服务器筛选 | 选中服务器时自动把来源置为 `mcp`（避免出现「服务器 + 内置」这种空集组合） |
| 权限隐藏 | 顶部「工具治理」与详情内「前往工具治理页」链接仅在 `hasPermission('mcp','admin')` 时渲染；后端路由另有 RBAC 兜底（不依赖前端隐藏做安全） |
| 服务器列表不可用 | 下拉退化为「服务器标识」手填输入框（审批流程不被治理面故障阻塞） |
| 详情 | 来源 / 服务器 / 风险 / 原始工具名 / 可调用名 / 角色快照 / 权限校验与原因 / 审批人与时间 / 耗时 / 错误码 / 结果摘要 + 脱敏参数；**仍不回显原始参数** |

## 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./handlers/ai/ -run 'TestListToolInvocations\|TestGetToolInvocation'` | ok（含新增 `TestListToolInvocations_FiltersByProviderAndServer`：不过滤 3 条 / provider=mcp 2 条 / +server=mock 1 条且三元组可追溯 / builtin 1 条且服务器为空 / 非法 provider 400 / 跨租户为空） |
| 2 | `go build ./...` | exit 0 |
| 3 | `npx jest --testPathPattern "ai/approval"` | **5/5 passed**（口径纯函数 + 筛选下发 + 详情展开 + 权限隐藏 + 降级） |
| 4 | `npx jest src/lib/api/__tests__/ai-api.test.ts` | 44/44 passed |
| 5 | `npx tsc --noEmit` | exit 0 |

## 实现细节与取舍

1. **接口签名升级而非新开方法**：`ListToolInvocations(ctx, tenantID, filter)` 直接替换旧的 `state string` 形参（全仓仅 2 处调用：handler 与测试 mock），避免长期并存两套语义。
2. **非法 `provider` 返回 400**：拼错的值若被静默接受会返回空列表，排障时容易误判为「没有数据」；显式报错更安全（`provider=mcp-servers` 这类常见笔误被用例锁定）。
3. **筛选条件回显**：响应回带 `provider/server`，前端据此做筛选回显与空态说明（空态直接写出「来源=…，服务器=…」）。
4. **服务器选项来源**：复用 M0-12 治理面 `GET /api/v1/mcp/servers`（非新造接口）；该调用失败不影响审批主流程。

## 已知缺口 / 观察

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | 跨页共享筛选状态 | 刷新后筛选不持久（未写入 URL query）；如需分享「某服务器的待审批」链接，M1-07 可一并做 URL 同步 |
| 2 | 审批页未做 invocation 深链 | 与 M1-05 卡片「前往审批页确认」一致：跳转不带 id 过滤，靠列表展示定位 |
| 3 | `ticket-attachment-api.test.ts` 预存在失败 | 仍为他人附件 WIP 的既有失败，与本任务无关（未触碰其文件） |
| 4 | E2E | 审批页来源维度的浏览器验证并入 M1-10/M2-05 |
