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
| F8 | itsm-init 反复失败：`verify process definition change_emergency_flow: exists=false` → workflow-core 事务回滚 → compose 依赖链中止（站点 502） | 迁移 `20260620_process_routing_enhancement.sql` 的示例绑定引用了**内置模板目录中不存在的 4 个 key**（change_emergency_flow / incident_general_flow / release_test_flow / expense_approval_flow）；`verifyWorkflowTemplates` 对全新库必然失败 | 删除这 5 行陈旧示例绑定（对齐参考生产库 `itsm_prod` 的绑定集）→ run 4 init exit 0，`process_definitions=18`；**仓库跟进：OB-11** |
| F9 | 前端容器 unhealthy（nginx 未启动、3000 拒绝连接），站点 `/` 502 | `nginx:alpine` 入口脚本 `10-listen-on-ipv6-by-default.sh` 在本机 snap-docker 环境卡死（`apk manifest nginx` 实测 8s 超时不返回） | 服务端 override（`docker-compose.test.yml`）为 itsm-frontend 指定 `entrypoint: sh -c "exec nginx -g 'daemon off;'"`（静态 SPA 无需入口脚本）→ healthy；**仓库跟进：OB-12** |

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

## 5. 回滚方案（旧栈完整保留）

1. 恢复 `.env.prod`：`VERSION=1.8.0-dev.4ffde439`、`DB_NAME=itsm_prod`（备份见 §2-1）；
2. `cd /home/n9e/itsm/repo && docker compose --env-file .env.prod -f docker-compose.prod.yml -f docker-compose.test.yml up -d`；
3. 旧镜像（`1.8.0-dev.4ffde439`）与旧库 `itsm_prod` 均未被修改；nginx 8088 端口不变。

## 6. 备注

- 前端 entrypoint 覆盖位于**服务端本地 override**（`docker-compose.test.yml`，未入库）；换机重建需同步，或按 OB-12 在镜像层修复后重建；
- 部署期清理的日志/镜像/缓存均可再生；备份目录保留供回滚与审计。
