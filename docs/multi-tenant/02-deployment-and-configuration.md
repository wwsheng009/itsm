# 02 · 部署与配置：saas_msp 模式

> **状态**：当前
> **更新日期**：2026-09-28
> **适用**：运维/部署工程师；前置阅读 [01-architecture.md](./01-architecture.md)

## 1. 部署模式选择

| 模式 | MSP 路由（`/api/v1/msp/*`） | 适用场景 |
|---|---|---|
| `private` | **404（整族关闭）** | 单企业内部私有化部署 |
| `saas` | 开启 | 纯多租户 SaaS（无服务商托管面） |
| `saas_msp` | 开启 | **多客户单一服务商托管（本目录场景）** |

配置来源：`itsm-backend/config.yaml:25-28`（`mode: "${DEPLOYMENT_MODE:private}"`，默认 `private`）；门控实现在 `middleware/msp_gate.go:21-33`，`main.go` 在路由注册前调用。

## 2. 前置条件

- PostgreSQL 17（生产），已完成可恢复备份与恢复抽检（`docs/delivery/production-initialization.md`）；
- Redis（缓存/消息）；
- 与 release 版本一致的制品、迁移文件与初始化 manifest；
- 显式提供生产环境变量文件，不得使用仓库默认凭据。

## 3. 关键配置项

| 配置 | 建议值 | 说明 |
|---|---|---|
| `DEPLOYMENT_MODE` | `saas_msp` | 必须显式设置；`private` 会关闭 MSP 路由 |
| `JWT_SECRET` | 强随机 | 认证与租户切换凭据签名 |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` | 按环境 | `itsm-backend/config.yaml:1-12` |
| `REDIS_PASSWORD` / `REDIS_URL` | 按环境 | 生产 compose 强制注入（`docker-compose.prod.yml:161-164`） |
| `ITSM_TENANT_GUARD_POLICY` | 生产保持 `fatal` | 未登记且缺 `tenant_id` 的表 → 拒绝启动（`internal/schema/tenant_guard.go:117-127`） |
| RLS 配置（`rls.mode`） | 起步 `off`，合规场景灰度 | `off`/`shadow`/`enforce`（`config/config.go:107-114`） |
| `rls.app_role_user/password` | 切 `enforce` 前配置 | 请求路径改用不带 BYPASSRLS 的低权角色（`config/config.go:153-157`） |
| `ITSM_AUTO_MIGRATE` / `ITSM_AUTO_SEED` | `false` | 初始化职责在 `itsm-init`，应用进程不自动迁移 |

## 4. 部署拓扑与初始化顺序

**拓扑**：一套 `itsm-api` / `itsm-worker` / Web + 单 PostgreSQL + Redis，服务全部租户（客户 A/B/C 与服务商）。生产 compose 固定项目名（`docker-compose.prod.yml:1-3`），避免与 dev 栈互相顶掉容器。

**初始化顺序**（对齐 `docs/delivery/production-initialization.md:21-23`）：

1. `itsm-init` 执行 migration + seed；
2. 启动 Web/API 实例；
3. `GET /api/v1/readyz` 返回 200 后才接入流量；
4. 对目标租户执行开通验证并保留 run ID（含 `saas_msp` 模式）。

## 5. RLS 灰度（可选强化）

1. `off`（默认）：中间件仍注入 `tenant_id`，不启用 policy，零风险；
2. `shadow`：每次请求设置 `app.current_tenant`，policy 未启用，仅观察上下文缺失点；
3. `enforce`：SESSION 变量 + policy 同时生效，数据库层强制隔离；**切换前先补齐全部缺失点，并将应用切到低权角色**（`config/config.go:107-114, 153-157`）。

## 6. tenant guard（启动门禁）

- 启动时扫描 `information_schema`，列出"缺 `tenant_id` 且未登记豁免"的表；
- `fatal` 策略下直接拒绝启动（`tenant_guard.go:222-229`）；
- 新增豁免必须：在 `TenantExemptTables` 登记 `reason/owner/scope/reviewed_at` + 补单元测试（`tenant_guard.go:9-20, 51-54`）；
- 多客户场景新增业务表时，默认应带 `tenant_id`，豁免需评审。

## 7. 缓存与外部依赖

- `itsm-backend/cache/` 未发现租户维度处理：**缓存 key 必须自带租户维度**（行动项 A8），否则多客户会串数据；
- AI 服务（`itsm-ai-service`）无租户状态，按请求参数接收 `tenantId`；
- 附件/对象存储按租户记录隔离（`attachments` 表带 `tenant_id`）。

## 8. 部署验证清单

| # | 验证项 | 方法 |
|---|---|---|
| 1 | 服务就绪 | `GET /api/v1/readyz` = 200 |
| 2 | MSP 路由已开启 | `GET /api/v1/msp/status` 非 404（登录态） |
| 3 | 平台租户可见 | `GET /api/v1/tenants`（`tenant.read`）能看到 provider 与客户租户 |
| 4 | tenant guard 通过 | 启动日志 `tenant_guard: pass` |
| 5 | RLS 模式符合预期 | 按 `rls.mode` 检查启动日志/配置 |
| 6 | 客户隔离抽检 | 客户 A 的 token 访问客户 B 数据 → 401/403 |

## 9. 常见配置错误

| 现象 | 原因 | 处理 |
|---|---|---|
| `/api/v1/msp/*` 全部 404 | `DEPLOYMENT_MODE=private`（或未设置，默认 private） | 显式设为 `saas_msp` 并重启 |
| 以为改了 typo 会关闭 MSP | 未知模式按 SaaS 默认**开启** MSP（`msp_gate.go:21-24`） | 关闭 MSP 只能用 `private`，变更后核对 `/msp/status` |
| 启动即失败并报 tenant_guard | 新表缺 `tenant_id` 且未登记豁免 | 补列或登记豁免（含 owner/reviewed_at + 测试） |
| `enforce` 后大量 500 | 存在未注入租户上下文的路径 | 退回 `shadow` 补齐缺失点，并切换低权角色 |
