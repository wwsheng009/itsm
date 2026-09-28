# M2-05 浏览器 E2E — 就绪性证据（规格 + CI 接线）

> 文档类型：验收证据（**部分交付**：规格与自动化接线就绪，真实栈执行待 CI 首绿）
> 任务：M2-05「浏览器 E2E 硬门槛」（实施方案 §4.3）
> 验收项：A2-05 —— **状态：未完成**（本文件不构成 `flow_verified` 判定）
> 编制日期：2026-09-27

## 1. 本机结论（先说边界）

M2-05 要求「浏览器 E2E 全链路绿」。本机（Windows）**无法提供该证据**，原因已逐项实测：

| 依赖 | 实测结果 | 影响 |
| --- | --- | --- |
| Postgres | `Test-NetConnection localhost:5432` → False；本机无 Docker（环境能力清单） | 真实后端起不来（`config.yaml` 为 Postgres 配置，运行时无 sqlite 分支） |
| 已部署栈（nginx :80） | 仓库既有 E2E 约定 `VITE_API_URL` 默认 `http://localhost`（端口 80） | 本机无该栈 |
| Playwright 浏览器 | `%USERPROFILE%\AppData\Local\ms-playwright` 不存在（未下载）；系统仅装 Edge | 默认项目（chromium/firefox/webkit）不可用 |
| ffmpeg（Playwright 录制依赖） | 默认配置启动报 `Executable doesn't exist … ffmpeg-1011\ffmpeg-win64.exe` | 需 `video: 'off'` 或 `npx playwright install` |
| Edge 通道 | `channel: 'msedge'` + `video:'off'` 实测**可启动并加载页面**（本地冒烟跑通到页面 DOM 断言阶段） | 浏览器链路本身可用 |

因此本轮交付「规格 + CI 执行路径」，并把首次执行放到有 Postgres/Docker 的 CI 环境；**A2-05 保持未完成**，不虚报。

## 2. 交付物

| 交付物 | 路径 | 说明 |
| --- | --- | --- |
| MCP 浏览器/API E2E 规格 | `itsm-frontend/tests/e2e/flows/flow-mcp-admin.spec.ts` | 3 个用例：①管理面 API 全链路（建服务器→测试连接→启用→工具发现/治理→凭据轮换→禁用，含 D3 前缀与 D7 默认拒绝断言）；②管理页浏览器渲染与交互（UI 登录 → `/admin/mcp-servers` → 服务器行/状态徽标/健康摘要/工具抽屉 + console error 收集 + 截图）；③治理页冒烟 `/ai/approval`、`/ai/audit` |
| CI 作业 | `.github/workflows/e2e-mcp.yml` | 真实栈：postgres:16 service + mock MCP 服务器（`cmd/mcp-mockserver` :19090）+ 后端（`MCP_ENABLED/MCP_WRITE_ENABLED/MCP_ENCRYPTION_KEY`）+ 前端（vite dev :3000，`ITSM_BACKEND_URL` 指向 :8090）+ `playwright install --with-deps chromium` + 规格执行 + 证据归档（playwright-report / trace / 截图 / 后端与 mock 日志） |
| 确定性 LLM 替身（P7 前置） | `itsm-backend/service/llm_mock_provider.go` + `llm_mock_provider_test.go` | 实现 `LLMProvider` / `StreamingLLMProvider` / `ToolCallingStreamProvider` 三件套：固定回复切片流式下发；声明了工具且内容命中触发词（默认 `__tool__`）时发起**一次确定性**工具调用（`mock-call-N` + 指定/回落工具名 + JSON 参数）。**双条件启用**（`llm.provider=mock` ∧ `LLM_MOCK_ENABLED=true`），未显式开启时回退默认 provider（生产不会隐式启用替身）。CI 作业已注入 `LLM_PROVIDER=mock` / `LLM_MOCK_ENABLED=true` / `LLM_MOCK_TRIGGER=__tool__` |

**为什么此前没有 E2E 自动化**：全仓 `.github/workflows/*` 检索 `test:e2e` / `playwright install` / `PLAYWRIGHT` **零命中**——浏览器 E2E 从未进入 CI；本作业同时补上这一缺口。因无法在本机校准，首轮以 `continue-on-error: true` 观察（与 `backend-ci.yml` 的 `mcp-postgres-migrations` 同一处置范式），绿跑后删除该行转为阻断门。

## 3. 本机验证记录（可复现）

| 命令 | 结果 |
| --- | --- |
| `npx tsc --noEmit`（itsm-frontend） | **exit 0**（新规格参与类型检查） |
| `PLAYWRIGHT_SKIP_CHANNELS=1 npx playwright test --list tests/e2e/flows/flow-mcp-admin.spec.ts` | **exit 0**：`Total: 9 tests in 1 file`（3 用例 × firefox/webkit/chromium） |
| 本地浏览器冒烟（临时规格 + `channel:'msedge'` + `video:'off'` + vite webServer） | Edge 成功启动并导航 `/login`（失败点仅为我方临时断言的登录表单可见性：无后端时会话探活未完成，页面停在骨架/介绍态）→ **证明 Playwright+Edge+webServer 链路在本机可用**；临时文件已删除，未入库 |
| 默认 Playwright 配置（`video: 'retain-on-failure'`） | 失败于 ffmpeg 缺失 → CI 需 `playwright install --with-deps`（作业已含） |
| `go test ./service/ -run 'TestMockProvider|TestMockLLM|TestNewProviderFromConfig_Mock'` | **ok 1.2s / 8 用例**：流式切片、无工具声明不触发、触发词命中调用指定工具（`mock-call-1`）、未命中不调用、指定名未声明回落第一个、工具名回落消息挂载声明、非法 JSON 规整 `{}`、开关仅 `true\|1` 开启 |
| `go build ./...` + `gofumpt -l ./service` + `staticcheck v0.6.1 ./service/ ./mcp/... ./metrics/ ./config/` | build exit 0；gofumpt 无输出；staticcheck **exit 0**（本轮顺带修复既有 `service/user_service.go:623 validatePassword unused (U1000)`——以 `//lint:ignore U1000` 注释保留作者「预留入口」意图，未删除代码） |

## 4. CI 首轮校准清单（作业头部已注明）

`e2e-mcp.yml` 中以下 4 处假设**未在本机验证**，首次运行按实际输出校正：

1. **健康探针路径**：脚本同时探测 `/api/v1/health` 与 `/health`（`router.go:332/336` 注册 `/health`、`/healthz`，分组前缀待实测确认）；
2. **首个管理员创建**：`cmd/initialize -action generate-bootstrap-token -tenant-id 1` 的输出解析 + `POST /api/v1/bootstrap/create-admin` 请求体；密码固定为 `AdminProd2026!` 以对齐 `tests/e2e/fixtures/auth.ts` 的 `TEST_ACCOUNTS.admin`（注：该密码**不在仓库内**，由部署侧 seeder/运维设定，故 CI 必须自建管理员）；
3. **后端起库/迁移/种子**：以 `config.yaml.example` 作配置文件（含 `${ENV:default}` 占位符）直接启动，若迁移需显式命令则补充；
4. **登录响应形状**：规格从 `POST /api/v1/auth/login` 响应读取 `data.accessToken`；若为纯 Cookie 会话，规格会在该用例 `skip` 并给出提示（不假红），此时需按实际会话形态调整前置方式。

## 5. 规格覆盖面与残留

**已覆盖（对 A2-05 的部分映射）**：
- 管理面 API 全链路（真实后端 + 真实 mock MCP 服务器，无打桩）；
- 管理页浏览器真实渲染与交互（服务器行/状态/摘要/工具抽屉/无 console error/截图）；
- 审批页与审计页的浏览器冒烟。

**未覆盖（A2-05 仍缺）**：
1. **对话内工具调用→审批卡片→执行→时间线** 的浏览器**用例**：本轮已补齐确定性驱动（mock LLM provider，见 §2 与下方测试记录），但对话页的浏览器用例尚未编写——需要先校准对话页/时间线/审批卡片的选择器（本机无真实栈可试跑），故与 `e2e-mcp.yml` 的其他校准点一并留待 CI 首轮。M1-05/M1-06/M1-07 的对话内卡片与来源维度当前仍由 jest 组件/页面用例覆盖（`tool-approval-card`、`tool-call-timeline`、审批页、审计页）；
2. **M0-14 遗留的管理页人工冒烟截图**：本规格的截图可作为其替代证据，但需在真实栈产出（待 CI 首绿）；
3. **R-16（前端套件性能）**：属 M2-05 出口的另一项，本轮未处理。

## 6. 证据锚点

| 内容 | 位置 |
| --- | --- |
| E2E 规格 | `itsm-frontend/tests/e2e/flows/flow-mcp-admin.spec.ts`（3 用例；`skipIfMCPDisabled` / `loginViaUI` / `waitFor` 辅助函数） |
| CI 作业 | `.github/workflows/e2e-mcp.yml`（10 步；calibration 注释见文件头） |
| 既有 E2E 约定 | `itsm-frontend/playwright.config.ts`（webServer 起 vite :3000）、`tests/e2e/fixtures/auth.ts`（`loginAs/apiGet/apiPost`） |
| 后端锚点 | `itsm-backend/mcp/admin/credential.go:191`（`MCP_ENCRYPTION_KEY`）、`itsm-backend/router/mcp_routes.go:22`（路由装配）、`itsm-backend/cmd/mcp-mockserver/main.go:39`（mock 服务器参数） |
| 工件定义 | `itsm-frontend/src/pages/(main)/admin/mcp-servers/index.tsx`（`data-testid`：`mcp-row-*` / `mcp-status-*` / `mcp-tool-row-*` / `mcp-summary-*`） |

## 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-09-27 | 首次登记：M2-05 规格（3 用例）与 `e2e-mcp` CI 作业落地；本机验证 tsc/用例收集/Edge 浏览器链路；真实栈执行与 A2-05 判定待 CI 首绿（明确未完成，含 4 项首轮校准点） |
