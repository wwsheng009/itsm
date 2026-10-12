# 238 生产部署运行摘要（2026-10-12 · 新版程序 + 新数据库）

> 状态：**已上线（2026-10-12）**｜定位：`172.18.3.238` 全新数据库 `itsm_prod_v2` + 最新程序（`2d00a52e`）的 docker 前后端部署与访问验证证据（`saas_msp`）。
> 关联：[backlog OB-11/OB-12](../../backlog.md)｜部署脚本为本机 `.dev/deploy/deploy238_*.sh`（不入库）

## 1. 部署结果

| 项 | 值 |
|---|---|
| 代码版本 | `feat/msp-p0-hardening` @ `2d00a52e`（增量 bundle `4ffde43..2d00a52e`，4.2MB，二进制度量传输） |
| 镜像 | `itsm-backend` / `itsm-frontend` / `itsm-ai-service` = `1.8.0-dev.2d00a52e` |
| 数据库 | **新库 `itsm_prod_v2`**（同实例 `itsm-postgres-prod`；74 迁移 / 186 表 / admin=1）；**旧库 `itsm_prod` 原样保留** |
| 部署模式 | `DEPLOYMENT_MODE=saas_msp`（`mspRoutesEnabled=true`） |
| 对外入口 | nginx `0.0.0.0:8088`（保持既有端口不变） |
| 容器 | 7/7 healthy：backend / worker / frontend / nginx / postgres / redis / ai-service |

## 2. 执行步骤（脚本化、幂等）

1. **快照备份** → `/home/n9e/itsm/deploy-backup-20261012-223431`（`.env.prod` / compose / nginx conf / git 状态与 diff / 容器清单）；
2. **磁盘释放**：journal 真空（-3.0G）→ 轮转 `syslog.1`+`*.gz` 清理 → 构建缓存与 1.6.9/1.7.0 旧镜像清理；磁盘 96% → **80%（21G free）**；
3. **代码更新**：增量 bundle 传输 → `deploy-latest` checkout（`2d00a52e`）；复打服务端 8088 端口补丁；历史 `*.skipped` 迁移挪出（全新库跑完整链 73 条）；
4. **新库与配置**：创建 `itsm_prod_v2` + `vector` 扩展；`.env.prod` 切 `VERSION=1.8.0-dev.2d00a52e` / `DB_NAME=itsm_prod_v2`；ai-service retag 复用；
5. **构建**：`docker compose build itsm-backend itsm-frontend`（BUILD_EXIT=0）；
6. **上线**：`up -d` → itsm-init 迁移/播种 → backend/worker/frontend/nginx 启动。

## 3. 过程阻塞与修复（部署期）

| # | 现象 | 根因 | 处置 |
|---|---|---|---|
| F8 | itsm-init 反复失败：`verify process definition change_emergency_flow: exists=false` → workflow-core 事务回滚 → compose 依赖链中止（站点 502） | 迁移 `20260620_process_routing_enhancement.sql` 的示例绑定引用了**内置模板目录中不存在的 4 个 key**（change_emergency_flow / incident_general_flow / release_test_flow / expense_approval_flow）；`verifyWorkflowTemplates` 对全新库必然失败 | ① 现场：删除这 5 行陈旧示例绑定 → init exit 0；② **仓库修复（OB-11，提交 `7c90af22`）**：前向迁移 `20261013_prune_unbacked_process_bindings.sql`（幂等删除 4 个无模板 key 的绑定，不改历史迁移避免 checksum 漂移）+ 静态回归测试 `migration/process_bindings_consistency_test.go`；③ 验证：238 全新库探针 `init exit 0`（见 §3.1） |
| F9 | 前端容器 unhealthy（nginx 未启动、3000 拒绝连接），站点 `/` 502 | `nginx:alpine` 入口脚本 `10-listen-on-ipv6-by-default.sh` 在本机 snap-docker 环境卡死（`apk manifest nginx` 实测 8s 超时不返回） | ① 现场：服务端 override 指定 entrypoint 绕过；② **仓库修复（OB-12，提交 `7c90af22`）**：前端镜像生产阶段 `RUN rm -f /docker-entrypoint.d/10-listen-on-ipv6-by-default.sh`；③ 验证：移除 override 后重建前端，容器 t=5s 直接 healthy（见 §3.1） |

## 3.1 v2 修复部署与回归验证（2026-10-12 23:3x–23:4x）

> 版本标签升级：`1.8.0-dev.7c90af22`（来源提交 `7c90af22`；后端/ai-service 复用 retag，前端重建）。

| 验证项 | 结果 |
|---|---|
| itsm-init（v2 库，增量迁移） | **exit 0**；ledger 新增 `20261013_prune_unbacked_process_bindings`；`unbacked_bindings=0` |
| 前端容器（**移除 override 后**） | t=5s 直接 `healthy`（OB-12 修复生效） |
| 全容器 | 7/7 healthy，镜像全部为 `1.8.0-dev.7c90af22` |
| HTTP（公网 8088） | `/`、`/login`、`healthz`、`readyz` 均 200；`/api/v1/version = 1.8.0-dev.7c90af22` |
| **全新库探针**（`itsm_fresh_probe`，零人工干预跑完整 init） | **`probe_run_exit=0`**；ledger=75；`process_definitions=18`；**`unbacked_bindings=0`**；admin=1；探针库已删除（OB-11 修复生效） |

## 3.2 v3 修复部署与回归（OB-14，2026-10-12 23:1x）

> 版本标签升级：`1.8.0-dev.b50e7c7a`（来源提交 `b50e7c7a`；后端重建，前端/ai-service retag）。

| 验证项 | 结果 |
|---|---|
| 部署 | 7/7 容器 healthy；`/api/v1/version = 1.8.0-dev.b50e7c7a`；itsm-init exit 0 |
| 写路径（admin+provider_admin，已分配客户 3） | `POST /api/v1/msp/problems/1/status → investigating` **200**，复读状态 = `investigating`（修复前 403 权限不足） |
| 读面口径 | `allowedActions.status=true` 与写路径一致 |
| 反例：未分配客户写路径 | 403 `MSP_ALLOCATION_REQUIRED`（fail-closed，较早门拒绝） |
| 反例：平台通道建 `customer_user` MSP 角色 | `MSP_ROLE_NOT_ALLOWED`（白名单仅 provider_admin/provider_agent，设计内 fail-closed） |
| 单测/回归 | `middleware/msp_precheck_fallback_test.go` 5/5；`middleware`、`router`（MSP）包回归通过 |

## 3.3 v3 UI 全链路复验（浏览器，2026-10-13 07:1x CST）

账号 `prov.ui.op21013071836`（`admin` + `provider_admin`，平台通道建号后**自分配**客户 3；自分配直接成功——兜底对 `/msp/allocations` 同样生效，OB-13 手工绕行不再需要）：

| 验证项 | 结果 |
|---|---|
| 工作台 `跨租户单据 → 问题` 行内「推进状态」 | 弹窗正常（当前状态「调查中」）；下拉 已定位/已解决/已关闭；选「已定位」后「推 进」转可点，提交成功 |
| 行状态变化 | 「调查中」→「**已定位**」；行更新时间 `2026/10/13 07:21:10` |
| API 读回（同会话） | `GET /msp/problems/1?customerTenantId=3` → `status=identified`、`updatedAt=2026-10-12T23:21:10Z`、`allowedActions=[view:true,status:true]` |
| 口令卫生 | 复验账号口令已轮换作废（旧口令登录 → `2001`；新口令未回显）；详见 [smoke-records-writeface.md](./smoke-records-writeface.md) §6 |

## 4. 验证证据（公网入口 `http://172.18.3.238:8088`）

| 检查 | 结果 |
|---|---|
| 静态页 `/`、深链 `/tickets`、`/login` | 200（SPA index.html） |
| `/api/v1/healthz`、`/api/v1/readyz` | 200（readyz：`ready=true`、`schemaVersion=024_add_msp_route_enabled_and_routed_at`） |
| `/api/v1/version` | 200 → `version=1.8.0-dev.2d00a52e` |
| 管理员登录（真实口令，仅服务端读取/不回显） | 200 → `user=admin`、`role=super_admin`、`tenantId=1` |
| `/api/v1/msp/status` | `deploymentMode=saas_msp`、`mspRoutesEnabled=true` |
| 容器状态 | 7/7 healthy（全部新镜像标签） |
| 外部可达（本机 → 172.18.3.238:8088） | `/`、`/tickets`、healthz、readyz、version 均 200 |
| 建租户冒烟（API，9/9 PASS） | 服务商+客户租户创建 → 模板开通 ready=true → 首管创建/首登强制改密 → 列表/用量对账；详见 [smoke-tenants.md](./smoke-tenants.md) |
| 建号+建单+协同冒烟（API，14/14 PASS） | 三账号建号（provider_admin/provider_agent/客户）→ 客户建单 → 分配 → 工作台回复/改状态 → 客户侧回看；详见 [smoke-users-tickets.md](./smoke-users-tickets.md) |
| 三类单据读面冒烟（API，15/15 PASS） | 建号 → 分配 → 客户侧造样本（目录项/变更/问题/SR）→ 工作台 changes/problems/service-requests 读面可见；详见 [smoke-records-readface.md](./smoke-records-readface.md) |
| UI 级冒烟（浏览器，4/4 PASS） | 238 公网站：登录（MSP 菜单）→ 工作台 2 单 → 跨租户单据三类可见 → 行内回复「回复已发送」+ 评论落库；演示账号口令已轮换作废；详见 [smoke-ui.md](./smoke-ui.md) |

## 5. 回滚方案（旧栈完整保留）

1. 恢复 `.env.prod`：`VERSION=1.8.0-dev.4ffde439`、`DB_NAME=itsm_prod`（备份见 §2-1）；
2. `cd /home/n9e/itsm/repo && docker compose --env-file .env.prod -f docker-compose.prod.yml -f docker-compose.test.yml up -d`；
3. 旧镜像（`1.8.0-dev.4ffde439`）与旧库 `itsm_prod` 均未被修改；nginx 8088 端口不变。

## 6. 备注

- 前端 entrypoint 覆盖已**不再需要**（OB-12 已进镜像）：服务端本地 override（`docker-compose.test.yml`）现仅保留 `itsm-init` 的迁移目录挂载；换机重建时该挂载需同步（或可按需改为镜像内迁移）；
- 部署期清理的日志/镜像/缓存均可再生；备份目录保留供回滚与审计。
