# 安装

> Status: current。开发账号仅用于本地环境；生产初始化和认证以[部署优化报告](../DEPLOYMENT_OPTIMIZATION.md)及当前发布的 bootstrap/readiness 接口为准。

> 适用版本：ITSM v1.0+
> 阅读时间：约 5 分钟
> 难度：⭐⭐（需要 Docker 基础）

## 1. 环境要求

| 组件 | 最低版本 | 推荐版本 | 备注 |
|:---|:---|:---|:---|
| **Docker** | 24.0 | 27.0+ | 包含 Compose v2 |
| **Docker Compose** | v2.20 | v2.27+ | — |
| **磁盘** | 10 GB | 30 GB SSD | 包含 PostgreSQL 数据 |
| **内存** | 4 GB | 8 GB+ | 跑 LLM 建议 16 GB |
| **CPU** | 2 cores | 4 cores+ | RAG embedding 吃 CPU |

操作系统：Linux / macOS / Windows（WSL2）

## 2. 克隆仓库

```bash
git clone https://github.com/heidsoft/itsm.git
cd itsm
```

## 3. 配置环境变量

```bash
cp .env.example .env
```

最小配置（必须修改）：

```dotenv
# 数据库密码（生产环境务必使用强密码）
DB_PASSWORD=ChangeMeToStrongPassword123!

# JWT 密钥（生产环境务必使用 64+ 字符随机串）
JWT_SECRET=$(openssl rand -hex 32)

# 仅兼容旧版初始化；新生产部署应启用一次性 bootstrap token
BOOTSTRAP_TOKEN_ENABLED=1

# 日志级别
LOG_LEVEL=info  # debug | info | warn | error
```

完整配置项说明见 `.env.example` 注释。

### 3.1 附件域灰度开关（可选，默认全关）

附件域通用链路（方案见 `docs/plan/generic-attachment-richtext-control-plan.md` §6.1）默认全部关闭，即保留现网旧链路、零行为变化。灰度与回退开关分两级，**部署级默认值**写在 `itsm-backend/config.yaml` 的 `attachment` 块（样例见 `itsm-backend/config.yaml.example`；容器部署样例见 `itsm-backend/deploy/config.yaml`）：

| 配置键 | 默认 | 作用 | 关闭后行为 |
|:---|:---|:---|:---|
| `attachment.generic_read_enabled` | false | 通用读路径（A2/A3/A4/A6） | 回退旧表读取，URL 不变 |
| `attachment.generic_write_enabled` | false | 通用写入（A1/A5） | 回退旧工单写入路径 |
| `attachment.dual_write_enabled` | false | 双写对账（P2） | 停止双写，旧表为事实源 |
| `attachment.inline_image_enabled` | false | 富文本内嵌图片走通用链路 | 编辑器图片入口禁用（fail-fast） |

也可用环境变量覆盖同名配置：`ATTACHMENT_GENERIC_READ_ENABLED` / `ATTACHMENT_GENERIC_WRITE_ENABLED` / `ATTACHMENT_DUAL_WRITE_ENABLED` / `ATTACHMENT_INLINE_IMAGE_ENABLED`（取值 `true` / `false`）。

**按租户生效**：租户级覆盖走既有系统配置接口（`system_config`，`category=attachment`），键名与上表一致——例如只把灰度租户的 `attachment.generic_read_enabled` 置为 `true`。默认值由租户初始化写入（`service/system_config_service.go` 的 `InitDefaultConfigs`）。

**回退**：把对应开关改回 `false`（或删除租户覆盖项）即可，无需数据变更；新表可保留（见方案 §6.1「关闭后行为」列）。

**生命周期清理（BE-8，默认关闭，仅部署级）**：删除附件只做软删（`status=deleted`），物理文件保留；开启清理任务后，超过保留期的软删记录才会被回收（物理文件 + 元数据行），且**仍被宿主正文 / 评论引用**的记录一律跳过。上线顺序固定为「先开任务演练 → 核对清单 → 再开落删」：

| 配置键 | 默认 | 作用 | 关闭 / 为 false 时行为 |
|:---|:---|:---|:---|
| `attachment.cleanup_enabled` | false | 启动后台清理任务（按租户循环，启动即跑一轮） | 任务不注册，软删记录与物理文件都不动 |
| `attachment.cleanup_purge_enabled` | false | 真实落删（先删物理文件、后删元数据行） | **演练模式（dry-run）**：只统计并输出清单，不删任何数据 |
| `attachment.retention_days` | 30 | 软删保留期（天） | `deleted_at` 晚于 `now - retention` 的记录不进入候选 |
| `attachment.cleanup_interval_minutes` | 360 | 轮询间隔（分钟，6 小时） | 首轮仍在任务启动时立即执行 |
| `attachment.cleanup_batch_size` | 200 | 单轮单租户处理上限（条，硬上限 1000，超出按 1000 截断） | 候选多于上限时留待下一轮 |

环境变量覆盖：`ATTACHMENT_CLEANUP_ENABLED` / `ATTACHMENT_CLEANUP_PURGE_ENABLED` / `ATTACHMENT_RETENTION_DAYS` / `ATTACHMENT_CLEANUP_INTERVAL_MINUTES` / `ATTACHMENT_CLEANUP_BATCH_SIZE`。

清理任务**不读取租户级 `system_config` 覆盖**（属运维动作，避免单个租户改配置影响全局回收策略），只取部署级 `config.yaml` / 环境变量。

**演练**：先只开 `cleanup_enabled`（保持 `cleanup_purge_enabled=false`），观察日志 `attachment cleanup task started` 与 `attachment cleanup completed`（`summary=scanned=… purged=… skipped_referenced=… failed=… dry_run=true`），并关注 `attachment cleanup: skipped, still referenced` 告警；清单核对无误后再开落删。完整演练步骤、SQL 核对口径与回滚见 `docs/testing/attachment-cleanup-drill-2026-09-22.md`。

**回滚**：把 `cleanup_enabled` 改回 `false` 并重启即可停止任务；已软删记录在保留期内仍可人工恢复（清理任务只回收 `deleted_at` 已过期的记录，先删文件后删行，失败自动留待下轮重试）。

## 4. 启动服务（开发模式）

```bash
cp .env.dev.example .env
make dev-start-docker
# 等价：docker compose --env-file .env -f docker-compose.dev.yml --profile dev up -d --build
```

首次启动会：

1. 拉取镜像（约 5-10 分钟，取决于网速）
2. 初始化 PostgreSQL schema（[Ent migrations](../../itsm-backend/ent/migrate)）
3. 播种基础数据（角色、权限、菜单）
4. 在本地开发模式创建开发管理员 `admin / admin123`

查看启动进度：

```bash
docker compose --env-file .env -f docker-compose.dev.yml --profile dev logs -f itsm-backend
```

看到 `Server started on :8090` 即后端就绪。

## 5. 验证

```bash
# 健康检查
curl http://localhost:8090/api/v1/health
# 期望返回：{"code":0,"message":"success","data":{"status":"ok",...}}

# 前端访问
open http://localhost:3000  # macOS
xdg-open http://localhost:3000  # Linux
start http://localhost:3000  # Windows
```

本地开发默认登录：`admin / admin123`。该凭据不得用于生产。

## 6. 生产部署

⚠️ **生产环境必须使用独立的 `.env.prod` 文件**：

```bash
# 1. 生成强密码和密钥
openssl rand -hex 32 > JWT_SECRET.txt
openssl rand -hex 16 > DB_PASSWORD.txt
openssl rand -hex 16 > REDIS_PASSWORD.txt

# 2. 创建 .env.prod（参考 .env.prod.example）
cp .env.prod.example .env.prod
# 编辑 .env.prod，填入上述密钥

# 3. 启动（必须显式传入 --env-file）
make prod-deploy

# 手工执行时必须显式传入环境文件
docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend itsm-frontend
docker compose --env-file .env.prod -f docker-compose.prod.yml up -d
```

生产环境不得假定存在默认管理员密码。部署后应先检查当前版本的 bootstrap 状态，由初始化 CLI 生成一次性 token，再通过受支持的 bootstrap API 创建首位管理员；token 必须过期、单次消费并产生审计。如果当前发布没有 bootstrap 状态接口，则该版本不满足本文的生产首次认证要求。

详见 [生产部署指南](../DEPLOYMENT_OPTIMIZATION.md)。

## 7. 卸载

```bash
# 停止并删除容器（保留数据卷）
make dev-stop-docker

# 停止并删除容器 + 数据卷（⚠️ 会清空所有数据）
docker compose --env-file .env -f docker-compose.dev.yml --profile dev down -v
```

## 下一步

- [本地开发命令](../dev-commands-reference.md)：如何修改代码、运行测试
- [项目快速开始](../../README.md#快速开始)：当前开发环境入口
- [架构总览](../architecture/overview.md)：理解模块划分
