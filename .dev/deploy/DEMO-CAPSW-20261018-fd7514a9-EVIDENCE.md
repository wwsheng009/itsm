# DEMO 部署证据：AI 能力开关迁移至系统配置 + Bot 平台级开关免重启（fd7514a9）

- 目标环境：`172.18.3.238` demo（`docker-compose.demo.yml`，端口 8089）
- 部署版本：`1.8.0-dev.fd7514a9`
- 远端 HEAD：`fd7514a937713486f4cc4c0c755e42c0aa7091db`（`feat(ai): Bot 能力改平台级开关免重启，能力开关迁移至系统配置 AI 页签`）
- 上一版本：`1.8.0-dev.00aaa26e`；bundle：`.dev/deploy/itsm-capsw-fd7514a9.bundle`（`00aaa26e..fd7514a9`，含 `c43e6636` 证据文档提交）
- 部署脚本：`/tmp/deploy238_capsw_a.sh` / `_b.sh` / `_w.sh` / `_c.sh` / `_verify.sh`；UI 脚本 `.dev/deploy/ui-verify-ai-cap.cjs`

## 1. 部署步骤与结果

| 步骤 | 结果 |
|---|---|
| A：bundle 上传 + checkout（分支 `deploy-demo-capsw-fd7514a9`） | ✅ `HEAD-SHA=fd7514a9…`；`.env.demo` `VERSION=1.8.0-dev.fd7514a9` |
| B：`docker compose build`（backend + frontend） | ✅ `/tmp/build-capsw-1.8.0-dev.fd7514a9.log`：`Image itsm-backend:… Built`、`Image itsm-frontend:… Built`、`BUILD_EXIT=0` |
| C：`up -d`（backend/worker/frontend）+ 健康检查 | ✅ 三容器 `Up (healthy)`、`itsm-init-demo` `Exited (0)`、`GET /api/v1/version` → `1.8.0-dev.fd7514a9`、root/health=200 |

备注：

- 首次 B 步以 `n9e` 直跑失败（`permission denied … docker.sock`），按既有流程改 `sudo -S` 重跑成功。
- C4 指纹脚本按 `/usr/share/nginx/html/assets` 取数（prod 镜像实际产物目录为 `/static/`）得 0；复查：`ai-capability` 命中 `static/index-BO-KzrWi.js`、`mcp-capabilities-moved` 命中 `static/index-CRqg_pUV.js`（镜像内均存在）。

## 2. API 验收（`deploy238_capsw_verify.sh`，`V-STATUS: DONE`）

| # | 检查项 | 结果 |
|---|---|---|
| V0/V8 | backend 容器 `StartedAt` 前后对比 | `2026-10-18T13:52:39.683691089Z`（**一致，开关切换全程免重启**） |
| V2 | `GET /system-configs/ai-capabilities` 初始态 | `mcpEnabled=true mcpWriteEnabled=true botEnabled=true`；`defaults.botEnabled=false`、`overridden={bot.enabled,mcp.write_enabled}` |
| V3 | `PUT {"botEnabled":false}` | `code=0`，立即生效 |
| V4 | 关闭态 `GET /admin/bots` | **200**（读端保留），响应内 `capabilities.botEnabled=false` |
| V5 | 关闭态 `POST /admin/bots` | **403** `Bot 能力已关闭（bot.enabled=false），请在「系统配置 → AI 配置」中启用后再操作`（MSG-OK） |
| V6 | `PUT {"botEnabled":true}` | `code=0`，免重启恢复 |
| V7 | 开启态 `POST /admin/bots`（空 body） | **400** `bot: 参数非法: slug/name 必填`（门禁放行、进入校验层，非 403） |
| V8 | 初始态恢复 | `botEnabled` 保持 `true`（与部署前一致） |

## 3. 浏览器验收（`ui-verify-ai-cap.cjs`，Playwright，5/5 PASS）

| # | 检查项 | 结果 |
|---|---|---|
| A1 | 深链 `/admin/system-config?tab=ai` | `AI 配置` 页签 `aria-selected=true`、`ai-capability-card` / `ai-capabilities-info` 可见 |
| A2 | 三开关渲染 | `ai-capability-switch-{mcp.enabled,mcp.write_enabled,bot.enabled}` 各 ×1、来源徽标、保存/恢复按钮齐备 |
| A3 | 无参数进入 → 点击「AI 配置」 | URL 回写 `?tab=ai`、卡片可见 |
| B1 | `/admin/mcp-servers` | `mcp-capabilities-moved` 可见（含「能力开关已迁移」）、旧 `mcp-capability-card`=0、`ai-capability-card`=0 |
| B2 | 迁移提示链接 | 「前往 AI 配置」→ `/admin/system-config?tab=ai`、卡片可见 |

截图：`.dev/deploy/ui-shots/CAP-1-system-config-ai-tab.png`、`CAP-2-tab-url-sync.png`、`CAP-3-mcp-moved.png`、`CAP-4-moved-link-nav.png`；`pageErrors`：无。

## 4. 本地回归

- `go build ./handlers/ai/... ./router/... ./internal/bootstrap/... ./capability/...` ✅
- `go test ./router/ -run "TestSetupRoutes_BotRoutes|TestBotRouteDeclarationsUseAICodes|TestBotSelectorRouteDeclaresAIRead" -count=1` ✅
- `go test ./handlers/ai/ -run "TestBotAdminHandler_|TestGetBotMetrics|TestChatToolDecisionBlacklistNeverDispatched|TestValidateBotSelection" -count=1` ✅
- `npm run type-check` ✅；`npx eslint`（8 个改动文件）✅
- jest：`AICapabilitiesCard` 5 passed / `AICapabilitiesSection` 3 passed / `mcp-servers` 4 passed
