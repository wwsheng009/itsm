# MCP 管理页字段契约修复：camelCase 归一化 vs snake_case 契约

> 文档类型：缺陷修复证据（前端）
> Status: done
> 编制日期：2026-09-30
> 适用范围：`/admin/mcp-servers` 页面（服务器列表 / 工具治理 / 健康摘要）在真实前端+后端栈上的可用性
> 目标读者：前端/后端维护者、QA、里程碑出口评审
> 关联文档：`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M0-12 / M2-05）、`docs/plan/evidence/mcp-m1/gap-register-2026-09-27.md`
> 核查基线：分支 `feat/bot-mcp-integration`；后端 `itsm-backend/main.exe`（2026-09-30 构建，:8090，`mcp.enabled=true`）+ mock MCP :19090；前端 Vite dev :3000
> 核查方式：真实浏览器实测（页面渲染 + 应用自身 `httpClient` 模块直调）+ 单元测试（tsc / eslint / jest）
> 状态口径：`implemented → unit_verified → integration_verified → flow_verified → accepted`

## 1. 现象（用户报告 + 实测复现）

| # | 现象 | 实测证据 |
| --- | --- | --- |
| 1 | 服务器列表「工具（启用/总数）」显示 `undefined/undefined` | 页面文本 `undefined/undefined`；同页请求真实返回 `tool_count:6 / enabled_tool_count:3` |
| 2 | 汇总卡「生效工具」= 0（真实值 3） | 汇总卡显示 0；接口 `summary.enabled_tools=3` |
| 3 | 工具治理页「投影名」为空 | 表格投影名列 6 行全空；接口返回 `callable_name` 齐全 |
| 4 | 工具治理页开关恒 false（只读 / 启用工具） | `.ant-switch` 的 `aria-checked` 恒 `false`；接口 `read_only`/`configured_enabled` 有值 |
| 5 | 连带：单工具启停 / 批量 / 分类标注 / 凭据轮换等写操作不可用 | 由 3 推导——URL 中的投影名取自 `tool.callable_name`（undefined） |

## 2. 根因

- `itsm-frontend/src/lib/api/http-client.ts` 对**所有**请求/响应做 key 归一化：
  - 请求体：`toCamelCase(JSON.parse(body))`（修复前 `:368-371`，现 `:379-381`）；
  - 响应体：`toCamelCase(responseData.data)`（修复前 `:528-529`，现 `:538`）。
  这是仓库主流约定：后端新端点配套使用 camelCase tag（如 `itsm-backend/handlers/ai/bot_admin.go:41` `json:"riskLimit"`）。
- MCP 管理 API 的后端契约**冻结为 snake_case**：响应见 `itsm-backend/mcp/admin/service_types.go:136-138`（`tool_count`/`enabled_tool_count`/`quarantined_tool_count`）、`:218`（`callable_name`）；请求体同样 snake_case（`itsm-backend/handlers/mcp/handler.go` 的 DTO），且 M2-05 的 E2E 规格直接以 snake_case 调 API（`itsm-frontend/tests/e2e/flows/flow-mcp-admin.spec.ts:107-112`）。
- 前端 MCP 模块（`lib/api/mcp-api.ts` + 页面 + 助手 + 测试）全按 snake_case 编写 → 归一化后页面读到的是 `toolCount`/`callableName`/`configuredEnabled` 等，`server.tool_count` 恒为 `undefined`。
- 定向实测（在页面上下文调用应用自身模块，排除代理/后端因素）：
  `httpClient.get('/api/v1/ai/mcp-servers')` 返回的 item key 为
  `[id,name,displayName,transport,url,credentialType,trustLevel,enabled,status,runningStatus,lastError,protocolVersion,serverInfo,timeoutMs,maxParallelCalls,maxRetry,version,toolCount,enabledToolCount,quarantinedToolCount,headersMasked,credentialMasked,policy,createdAt,updatedAt]`
  ——**不存在任何带下划线的 key**。
- 为什么既有测试没拦住：页面/接口测试都 mock 了 `httpClient` 或 `mcpApi`（`src/pages/(main)/admin/mcp-servers/__tests__/index.test.tsx`、`src/lib/api/__tests__/mcp-api.test.ts`），真实归一化链路不在覆盖内；M2-05 的 Playwright 规格此前未在真实栈执行（CI 首绿尚未发生），M0-12 证据仅为单测。

## 3. 修复

| 文件 | 变更 |
| --- | --- |
| `itsm-frontend/src/lib/api/http-client.ts` | `RequestConfig` 新增 `rawKeys?: boolean`（:41）；`requestInternal` 在 `rawKeys` 下跳过请求体（:377-381）与响应体（:537-538）的 key 归一化；新增公开方法 `requestRaw<T>()`（:617-619），显式表达"该模块使用后端原始 key" |
| `itsm-frontend/src/lib/api/mcp-api.ts` | 13 个端点全部改走 `requestRaw`（本地助手 `rawGet` / `rawSend`）；模块头注释写明契约差异与原因 |
| `itsm-frontend/src/lib/api/__tests__/http-client.test.ts` | 新增 3 例：`requestRaw` 响应保持 snake_case、请求体保持 snake_case、默认路径仍归一化（回归守护） |
| `itsm-frontend/src/lib/api/__tests__/mcp-api.test.ts` | 断言改为 `requestRaw(url, { method, body })`；覆盖 13 端点的路径/方法/载荷 |

**取舍**：未把 MCP 前端模块改成 camelCase——那需要同步改动后端 API 契约（`handlers/mcp` 请求 DTO 的 json tag）以及已冻结的后端测试与 E2E 规格；本次只修前端调用层，**后端契约零变更**。

## 4. 验证

### 4.1 自动化

- `npx tsc --noEmit` → exit 0
- `npx eslint src/lib/api/http-client.ts src/lib/api/mcp-api.ts src/lib/api/__tests__/mcp-api.test.ts src/lib/api/__tests__/http-client.test.ts` → exit 0
- `npx jest`（http-client / mcp-api / mcp-helpers / 页面组件）→ 4 套件 / 54 用例全部通过

### 4.2 真实浏览器（2026-09-30，`http://localhost:3000/admin/mcp-servers`）

| 位置 | 修复后实测 |
| --- | --- |
| 服务器行 | 名称 `Mock MCP`（`display_name` 生效）/ 副标题 `mock` / `Streamable HTTP` / 状态 `healthy` + 协议版本 `2025-06-18` |
| 工具（启用/总数） | `3/6` |
| 汇总卡 | 服务器总数 1 / 已启用 1 / 已连接 1 / **生效工具 3** / 隔离工具 0 |
| 健康摘要页签 | mock / healthy / 协议版本 `2025-06-18` / 更新时间 `2026-09-30T00:20:49.651482Z` |
| 工具治理页签 | 投影名 6 行齐全：`mcp__mock__list_issues`、`mcp__mock__create_issue`、`mcp__mock__huge_output`、`mcp__mock__bad_name_73f637`、`mcp__mock__schema_probe`、`mcp__mock__slow_tool`；生效状态 = 生效中 / 已停用；开关与接口一致（list_issues、huge_output 为只读+启用；create_issue 非只读+启用；其余停用） |
| 写路径（应用自身模块直调） | `setToolClassification(1,'mcp__mock__list_issues',{read_only:true,risk:'read'})` → `{risk:'read',read_only:true,category:'issue'}`；`setToolEnabled('mcp__mock__schema_probe',true)` → `true`、随即 `false` → `false`（已还原）；`reloadServer(1)` → `running_status: healthy` |

### 4.3 未覆盖（残留）

- Playwright 规格 `itsm-frontend/tests/e2e/flows/flow-mcp-admin.spec.ts` 的**本机**执行：其 fixture 账号（`admin / AdminProd2026!`，`tests/e2e/fixtures/auth.ts:8`）与本机 seeder 口令不一致，无法按原样运行；A2-05 的验收级证据仍以 CI 作业 `.github/workflows/e2e-mcp.yml` 首绿为准。
- 页面截图：本次浏览器工具的工作区根不含 `docs/`，未落盘截图；CI 作业将产出 trace/video/screenshot。
- 其余管理面动作（新增服务器向导、测试连接弹窗、凭据轮换 UI、事件抽屉）本轮仅确认其字段来源已修复（同一 `mcp-api` 层），未逐一点击验证。

## 5. 影响与后续

- M0-12 页面由"仅单测（mock）"提升为"真实栈人工冒烟通过"；A0-12 的「+冒烟」判据**部分满足**（截图与 Playwright 仍待 CI）。
- A2-05 仍为**未完成**（以 CI 首绿为准）；本修复消除了其 UI 用例必然失败的首要原因。
- 同类风险提示：任何"后端 snake_case + 前端直接消费"的新模块都会踩同一坑；`requestRaw` 是这条约定的显式出口，已在 `http-client.ts` 注释中写明适用条件。

## 6. 变更记录

| 日期 | 变更人 | 内容 |
| --- | --- | --- |
| 2026-09-30 | AI 辅助执行 | 初稿：现象与实测复现、根因（key 归一化 × snake_case 契约）、前端修复与测试、真实浏览器验证、残留（CI E2E 与截图） |
