# 02 · 部署与配置：saas_msp 模式

> **状态**：当前
> **更新日期**：2026-09-28
> **适用**：运维/部署工程师；前置阅读 [01-architecture.md](./01-architecture.md)

> **定位（as-is）**：本文为**现状部署/运维手册**（2026-09-28 实测），**不是目标口径**；目标以 [canon](./plan/msp-concept-model-and-architecture-canon.md) 为准，差异与整改见[一致性审计](./plan/msp-docs-consistency-audit.md) §3（C1/C11：`saas` 门控、未知模式默认开启、`/tmp` 指引）。

## 1. 部署模式选择

| 模式 | MSP 路由（`/api/v1/msp/*`） | 适用场景 |
|---|---|---|
| `private` | **404（整族关闭）** | 单企业内部私有化部署 |
| `saas` | **404（目标态；2026-09-30 前为开启——缺陷 R12 已修）** | 纯多租户 SaaS（无服务商托管面） |
| `saas_msp` | **开启（唯一允许）** | **多客户单一服务商托管（本目录场景）** |
| 空值/未知值 | **启动失败（fatal：`invalid DEPLOYMENT_MODE`）** | 必须显式声明模式 |

配置来源：`itsm-backend/config.yaml:25-28`（`mode: "${DEPLOYMENT_MODE:private}"`，默认 `private`）；门控实现在 `middleware/msp_gate.go`，2026-09-30 起由 `internal/bootstrap/app.go` 在加载配置后应用（旧版为 `main.go` 读原始 env，见 IP-P0-1）。

> **目标口径（2026-09-30，IP-P0-1 已实现）**：仅 `saas_msp` 开放 MSP 路由；`saas`/`private` 关闭；空值/未知值启动失败。门控**单一来源** = `cfg.Deployment.Mode`（env `DEPLOYMENT_MODE`，默认 `private`），由 `internal/bootstrap/app.go` 在加载配置后应用（`main.go` 不再直接读原始 env）；启动日志输出 `deployment gate resolved` 与 `deployment self-check`（模式 / gate / provider 租户数三项）；`GET /api/v1/msp/status` 返回 `deploymentMode` + `mspRoutesEnabled`。

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
| `TENANT_USER_ADMIN_ENABLED` | `false`（按需开启） | 平台侧租户用户管理写通道灰度开关（重置密码/启停/强制下线，TUM-D3）；读列表/详情不受限；关闭时写操作 403 `TENANT_USER_ADMIN_DISABLED`；**例外：首管（`is_bootstrap_admin`）重置密码为兜底恢复通道，默认可用、不受开关约束（TUM-D3 修订，防租户锁死）**；服务器侧兜底 `cmd/reset_password`；与 `USER_PROVISIONING_CHANNELS_ENABLED` 独立回滚 |
| `MEMBERSHIP_EXPIRY_RECYCLE_ENABLED` | `true`（默认开） | membership 到期回收后台任务（IP-P2-9）：扫描 `expires_at` 已过的存活 membership（home 豁免），软删 + allocation 级联解除 + `membership.revoke` 审计 + 立即生效；显式 `false`/`0`/`no`/`off` 关闭（读路径惰性判定仍生效） |
| `MEMBERSHIP_EXPIRY_RECYCLE_INTERVAL_MINUTES` | `10` | 到期回收扫描间隔（分钟）；非法/<=0 回退默认 |

### 3.1 过渡开关登记（owner/到期，MSW-6 收口）

> 原则：每个过渡开关必须有 **owner + 默认值 + 退出计划**；到期未退出的须在 [07-known-gaps](./07-known-gaps.md) 登记。巡检联动：`cmd/membership-drift-check` 的 FAIL=0 是关闭 `AUTHZ_STATIC_FALLBACK` 的前置条件之一。

| 开关 | 默认 | 作用 | Owner | 退出/到期计划 |
|---|---|---|---|---|
| `AUTHZ_STATIC_FALLBACK` | `false` | membership 缺失 / `role_id` 空时回退编译期静态 RolePermissions（迁移窗口专用；每次回退记 Warn） | 权限域（W6 Membership 批次） | IP-P1 收敛后删除：全量回填 + 巡检 FAIL=0 + 观察窗后移除代码路径（M4 登记） |
| `USER_PROVISIONING_CHANNELS_ENABLED` | `true`（2026-10-08 起默认开） | 平台/MSP 建号写通道；未设置/空即开启，显式 `false`/`0`/`no`/`off` 回退 legacy 建号逻辑（404 通道未开启） | 账号域（W6） | 通道已默认开启（含 membership 同步）；后续删除开关 |
| `TENANT_USER_ADMIN_ENABLED` | `false` | 平台侧租户用户管理写通道灰度（重置/启停/强制下线；首管重置豁免） | 平台治理域（TUM） | TUM-7 收口后评估默认开启（审计+通知闭环已就绪） |
| `MEMBERSHIP_EXPIRY_RECYCLE_ENABLED` | `true`（2026-10-09 起默认开） | membership 到期回收后台任务（惰性判定不受影响；关闭仅停止状态收敛） | 账号域（W6） | 保持默认开启（「到期回收有效」为 RLS enforce 前置验证项，见 user-lifecycle §7）；非过渡项，长期保留 |
| `WORKBENCH_VIEWS_ENABLED` | `false` | 工作台自定义视图（保存过滤器组合 + 同 provider 分享）灰度 | MSP 工作台域（IP-P2-4a） | 视图稳定后默认开启 |
| `MCP_ENABLED` / `MCP_WRITE_ENABLED` | `false` | MCP 读/写通道灰度（写通道另行审计与白名单） | AI/MCP 域 | 见 MCP 安全评估（独立登记，不随多租户批次） |
| `ITSM_AUTO_MIGRATE` / `ITSM_AUTO_SEED` | `false` | 应用进程自动迁移/播种（初始化职责在 `itsm-init`） | 运维 | 保持 `false`（生产禁止自动迁移；非过渡项） |

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

- `itsm-backend/cache/` 未发现租户维度处理：**缓存 key 必须自带租户维度**（`ADR-004:A8`），否则多客户会串数据；
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
| `/api/v1/msp/*` 全部 404（saas 部署） | `DEPLOYMENT_MODE=saas`（目标态不开放 MSP，2026-09-30 起） | 确有 MSP 业务则显式设 `saas_msp`；否则保持 `saas` |
| 启动失败：`invalid DEPLOYMENT_MODE` | 空值/未知值（拼写错误、大小写不符） | 改为 `private`/`saas`/`saas_msp` 之一（大小写敏感） |
| 启动日志 `deployment self-check mismatch` | gate 与租户形态不一致（如 saas_msp 无 provider 租户） | 检查 seed/迁移是否完成；该项仅告警不阻断 |
| 以为改了 typo 会关闭 MSP | 旧版本未知模式按 SaaS 默认**开启** MSP（已修，R12） | 2026-09-30 起空/未知值**启动失败**；关闭 MSP 用 `private`，变更后核对 `/msp/status` |
| 启动即失败并报 tenant_guard | 新表缺 `tenant_id` 且未登记豁免 | 补列或登记豁免（含 owner/reviewed_at + 测试） |
| `enforce` 后大量 500 | 存在未注入租户上下文的路径 | 退回 `shadow` 补齐缺失点，并切换低权角色 |

## 10. 多租户初始化操作（脚本化）

生产实测（2026-09-28，见 [06 文档 §7](./06-verification-and-troubleshooting.md)）确认：`saas_msp` 模式下从零开通"1 服务商 + N 客户"需同时处理若干存量数据与产品边界问题（详见 [07-known-gaps.md](./07-known-gaps.md)）。为此提供两个幂等运维脚本（位于 `scripts/msp/`）：

| 脚本 | 作用 |
|---|---|
| `scripts/msp/build-provision-tenant.sh` | 用 `golang:1.25.13-alpine` 镜像构建 `provision_tenant` 二进制，产物默认 `$HOME/itsm-artifacts/provision_tenant_linux_amd64`（snap docker 可见路径，见 G10） |
| `scripts/msp/setup-msp-tenants.sh` | 一键完成：租户创建 → 模板供给 → MSP 角色授权 → 首个用户 → 分配关系 → 隔离性验证 |

### 10.1 构建 provision_tenant

**方式 A（推荐，2026-09-28 实测通过）——本机交叉编译后上传：**

```bash
cd itsm-backend
mkdir -p "$HOME/itsm-artifacts"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$HOME/itsm-artifacts/provision_tenant_linux_amd64" ./cmd/provision_tenant
scp "$HOME/itsm-artifacts/provision_tenant_linux_amd64" <host>:~/itsm-artifacts/
```

**方式 B（备选）——在部署主机用容器构建：**

```bash
bash build-provision-tenant.sh
# 构建完成：$HOME/itsm-artifacts/provision_tenant_linux_amd64
```

说明：

- 仓库无 `vendor/` 目录，模块需联网下载；本环境实测主机**无法直连 `proxy.golang.org`**（超时）、可直连 `goproxy.cn`，故方式 B 默认 `GOPROXY=https://goproxy.cn,direct` 且 `GOSUMDB=off`，模块缓存持久化在 `$HOME/itsm-artifacts/gomod-cache`；
- **snap 版 docker 的路径约束（本机实测）**：`/snap/bin/docker` 的守护进程拥有独立 `/tmp`，宿主 `/tmp` 对其 `docker cp` / `docker run -v` 不可见（`docker cp` 会返回成功但容器内仍是旧文件、`-v` 产物会"写丢"）；snap 的 `home` 接口也不放行 `$HOME` 下的隐藏目录。因此产物、缓存与暂存目录一律使用 `$HOME` 下的**非隐藏**目录（默认 `$HOME/itsm-artifacts`）；
- 本次实测中方式 B 的容器内模块下载长时间无进展（容器被反复重建），最终采用方式 A 完成构建与供给验证；方式 B 保留给网络/缓存良好的主机；
- 方式 B 可覆盖变量：`REPO_DIR` / `OUT` / `IMAGE` / `GOPROXY_URL` / `GOMOD_CACHE` / `ARTIFACT_DIR` / `SUDO_PASS`。

### 10.2 一键初始化

```bash
# 默认 BASE=http://127.0.0.1:8088、ADMIN_USER=admin、容器名 itsm-backend-prod / itsm-postgres-prod
bash setup-msp-tenants.sh
```

脚本按 8 个阶段执行，全部幂等（重复执行输出 `exists` / `跳过`）：

| 阶段 | 动作 | 必要性 |
|---|---|---|
| 0 | admin 登录获取 Bearer token | 租户创建需 `tenant:write` |
| 1 | 回填 default 租户 6 个内置审批组 | 存量库缺组会导致供给 readiness 校验失败（G4） |
| 2 | 校正 `roles/permissions/menus/groups/role_permissions/...` 的 id 序列 | 序列落后于 `max(id)` 会触发 `duplicate key`（G5） |
| 3 | 创建 `msp_provider` + `msp_customer` 租户（含 `parentTenantId` / `mspProviderId` 绑定） | 场景骨架 |
| 4 | 对每个新租户执行 `provision_tenant -tenant-id <ID> -template-version 1.0.0` | 克隆 roles/permissions/menus/groups/SLA/CI 类型等模板 |
| 5 | 为 provider 租户 `msp_manager` / `msp_tech` / `msp_viewer` / `msp_specialist` 写 `role_permissions` | DB 权威模式下角色无权限行即 fail-closed（G6） |
| 6 | 用 SQL + pgcrypto 创建各租户首个用户 | **`07:G1`/`07:G2` 均已关闭（2026-09-30）**：平台/MSP 可经 API 建号（`POST /api/v1/tenants/:id/users`、`POST /api/v1/msp/customers/:id/users`；建号通道默认开启，显式 `USER_PROVISIONING_CHANNELS_ENABLED=false` 才关闭）；首个管理员走 `cmd/initialize -tenant-id/-tenant-code`（bootstrap token，账号策略 `admin-<tenantCode>`）或 `provision_tenant -create-admin`；首登强制改密经 `/api/v1/auth/change-password` |
| 7 | 由 mspadmin 的 MSP token 调 `POST /api/v1/msp/allocations` 建立分配 | 验证 MSP 分配链路 |
| 8 | 隔离性探针（06 文档 §7.2） | 验收证据 |

### 10.3 前置条件与注意

- 需 `sudo` 免密或提供 `SUDO_PASS`；脚本通过 `docker exec` 访问 PostgreSQL 与应用容器；
- `PROVISION_BIN_HOST` 默认 `$HOME/itsm-artifacts/provision_tenant_linux_amd64`（**禁止放 `/tmp`**：snap docker 守护进程不可见，见 G10），缺失时脚本报错退出；
- 注入容器前脚本会把二进制复制到 `$HOME/itsm-artifacts`（snap docker 可见）并**校验 sha256**，不一致即失败退出（规避 snap docker 下 `docker cp` 静默复制旧文件的问题）；
- 脚本只创建/补齐数据，**不删除**任何数据；对已有同 code 租户复用其 ID；
- 客户租户 ID 与 provider 绑定由 API 写入，若失败会打印原始响应并以非零码退出。
