# 生产部署清单（当前版本）

> Status: current
> 更新：2026-11-11
> 基线：8089 演示栈（`1.8.0-dev.af3cba9c`，当前工作区 HEAD 全量构建，Schema `20261111_service_request_delete_grants`）。
> 目标：把 8088 重建为新的生产/验收栈，或从同一 release tag 新建生产栈。
> 执行记录：[238 · 8088 重建记录](./238-8088-rebuild-2026-11-09.md)

本文是生产部署的**单一入口清单**。`make prod-deploy` 现在也转发到
`scripts/deploy-prod.sh`；生产发布应使用脚本的五阶段流水线
（校验 → 备份 → 构建 → 部署 → 验证），不要直接执行底层 `make deploy`。

## 1. 发布前门禁

- [ ] 从当前 HEAD（或已合并的 release 分支）创建 **不可变 git tag**，例如 `v1.8.0-rc.1`。
- [ ] 四个应用镜像由同一个 `VERSION` 构建：`itsm-backend`、`itsm-frontend`、`itsm-ai-service`、worker（复用 backend 镜像）。
- [ ] `docker compose --env-file .env.prod -f docker-compose.prod.yml config --images` 输出的每个应用镜像都带同一 tag。
- [ ] 已完成 `go build ./...`（Go 1.25.13）与 `npm run type-check`（建议 `NODE_OPTIONS=--max-old-space-size=4096`）。
- [ ] 已跑 `node scripts/check-engineering-contracts.js`（当前 8/8 通过）。
- [ ] 已跑 `bash scripts/docs-gate/run-all.sh`；C.3 的失效链接应清零或登记豁免。

## 2. 必须修改的生产配置

| 配置 | 要求 |
|---|---|
| `VERSION` | 固定 release tag；禁止 `latest` / 占位符 |
| `DB_PASSWORD` / `REDIS_PASSWORD` / `JWT_SECRET` / `ADMIN_PASSWORD` | 强随机值；不要沿用 8088 的测试口令 |
| `CONFIG_SECRET_KEY` | 64 位 hex；用于 system config 敏感字段 AES-256-GCM；生成后不可随意变更 |
| `LLM_PROVIDER_ENCRYPTION_KEY` | 64 位 hex；DB 内 AI Provider 密钥加密主密钥；生成后不可随意变更 |
| `DEPLOYMENT_MODE` | `private` / `saas` / `saas_msp` 按业务选择 |
| `APP_BASE_URL` | 对外访问地址，用于邀请/通知链接 |
| `CORS_ALLOWED_ORIGINS` | 生产域名 |
| `PROD_HTTP_PORT` | Nginx 宿主入口端口，默认 80；同机 80 被占用时设为 8088 等 |
| `SMTP_ENABLED` + `SMTP_*` | 需要邮件时显式启用；只配 `SMTP_HOST` 不会发信 |
| `RLS_MODE` | 默认 `off`；如需 `enforce`，先执行 `cmd/rls-apply` 并修复 G17 |
| `ITSM_AI_READY` | AI 默认启用；无 Provider 的环境应设为 `0` |
| TLS | Compose 只提供 HTTP 80；TLS 必须在企业入口网关终止 |
| `GRAFANA_ADMIN_PASSWORD` | `init` 会生成随机值；即使暂不启用 monitoring，`docker compose config` 也会做插值校验 |

完整变量与默认值见 `.env.prod.example`；标记 `[COMPOSE-IGNORED]` 的变量在
`docker-compose.prod.yml` 中已被硬编码，修改 Compose 部署不会生效。

## 3. AI 默认启用与 Provider 配置

- `ITSM_AI_READY=1`、`LLM_MULTI_PROVIDER_ENABLED=true`：AI 菜单与 Provider 管理页默认可见。
- **实际 Provider 以「系统配置 → LLM 模型」页面为准**（DB 表 `llm_provider_configs`，按租户生效）。
- `.env.prod` 中的 `LLM_PROVIDER` / `LLM_MODEL` / `LLM_API_KEY` 是 sidecar/静态回退值，不是多 Provider 模式下的实际选择。
- 上线前用管理员登录，配置 Provider 后执行一次 `/ai/triage` 或 `/ai/chat` 冒烟，并检查：
  - `GET /api/v1/ai/providers/available`
  - `GET /api/v1/ai/llm-calls/summary?days=1`
  - 成功率应 > 0；`429` 需要续费/换 Provider/换套餐。
- 8089 当前 `baidu / deepseek-v4.1-flash` 已配置但月度配额耗尽（26/26 `rate_limited`），不是代码问题。
- `bot.enabled` / `mcp.write_enabled` 默认关闭；不要直接复制 8089 演示栈的租户覆盖值。
- 当前 8088 已配置并测试 `baidu / deepseek-v4.1-flash`（Provider `status=ok`；
  `/ai/triage` 冒烟通过），详见[8088 重建记录](./238-8088-rebuild-2026-11-09.md)。

## 4. RLS 决策

| 模式 | 适用 |
|---|---|
| `off` | 默认；应用层租户隔离仍生效 |
| `shadow` | 观察缺租户上下文；不阻断 |
| `enforce` | 需要先执行 `rls-apply` 全套策略 + 低权 DB 角色 + 跨租户回归；G17（`msp_contracts` 跨租户只读）修复前不得启用 |

当前 8088/8089 均为 `shadow`。生产若要求数据库级强隔离，按
`docs/multi-tenant/02-deployment-and-configuration.md` §5 和
`itsm-backend/cmd/rls-apply` 执行，不能只改 `RLS_MODE`。

## 5. 首次生产部署

```bash
# 1) 生成 .env.prod（不要先手动 cp，否则 init 会跳过密钥生成）
./scripts/deploy-prod.sh init

# 2) 编辑 .env.prod：VERSION、域名、ADMIN_PASSWORD、APP_BASE_URL、SMTP、AI/RLS 决策

# 3) 只读预检（不启动容器）
./scripts/deploy-prod.sh deploy --dry-run

# 4) 正式发布（校验→备份→构建→部署→验证）
./scripts/deploy-prod.sh deploy
```

## 6. 部署后验证

```bash
curl -fsS http://127.0.0.1:8090/api/v1/health
curl -fsS http://127.0.0.1:8090/api/v1/readyz
curl -fsS http://localhost/health          # nginx
curl -fsS http://localhost/api/v1/version
```

还需确认：

- `itsm-init` exit=0，初始化账本的六个组件均 `succeeded`；
- `itsm-backend-prod`、`itsm-frontend-prod`、`itsm-nginx-prod`、`itsm-worker-prod`、`itsm-ai-service-prod` 全部 running/healthy；
- 登录、建单、审批、知识库、CMDB、附件上传/预览、AI Provider 调用各一条冒烟；
- 审计日志、`/metrics`（启用 monitoring 时）、备份文件可读。

## 7. 备份与回滚

```bash
# 备份（保留最近 7 份，支持 --verify）
./scripts/prod-backup.sh --verify

# 恢复演练（隔离临时库，不触碰生产）
./scripts/prod-restore-drill.sh

# 应用回滚（读取 .deploy/previous）
./scripts/deploy-prod.sh rollback
```

Schema 回滚优先恢复升级前 `pg_dump`；Ent 自动迁移不提供通用 down migration。
近期迁移的 `_down.sql` 覆盖情况见 `docs/delivery/rollback-guide.md`，缺失项按
forward-fix 处理并在发布证据中登记。

## 8. 当前已知阻塞项

- 8088 仍是 2026-10-17 的 `1.8.0-dev.586bb07a`（Schema 024），落后当前 HEAD 146 个提交；重建前不能作为生产候选。
- 仓库当前没有 git tag；`release.yml` 不会产出 release 制品。
- G17：`msp_contracts` RLS 012 跨租户只读放宽，018/019 未实现；RLS enforce 前必须修复。
- `docs/delivery/rollback-guide.md` 仍有历史 TODO；近期 8 个迁移没有 `_down.sql`。
- CI 的 MCP Postgres / e2e-bot / e2e-mcp 仍是 `continue-on-error`，校准后应转阻断门。

## 9. 8088 重建执行单（待确认后执行）

以 8089 为基线时，8088 重建按以下顺序执行；执行前先确认是否保留
`itsm_prod_v2` 现有数据：

1. **备份**：复制 `.env.prod`，并用 `PG_DATABASE=itsm_prod_v2 ./scripts/prod-backup.sh --verify`
   生成可恢复备份（238 上 Docker 需 sudo，可用 `sudo -E env PG_DATABASE=itsm_prod_v2 ...`）。
2. **版本/入口**：在 `.env.prod` 设置 `VERSION=1.8.0-dev.af3cba9c`（8089 当前基线）
   或正式 release tag；不要使用 `latest`。
   同时设置 `PROD_HTTP_PORT=8088`（当前 8088 入口；避免与宿主 80 端口冲突）。
3. **配置**：补 `CONFIG_SECRET_KEY`、`LLM_PROVIDER_ENCRYPTION_KEY`、`APP_BASE_URL`、
   `GRAFANA_ADMIN_PASSWORD`；AI 保持 `ITSM_AI_READY=1` / `LLM_MULTI_PROVIDER_ENABLED=true`，
   Provider 由前端「系统配置 → LLM 模型」配置。
4. **构建**：`docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend itsm-frontend itsm-ai-service`。
5. **初始化**：`docker compose --env-file .env.prod -f docker-compose.prod.yml up itsm-init`，
   确认 exit=0；随后 `up -d`。
6. **验收**：`/api/v1/health`、`/api/v1/readyz`、`/api/v1/version`、登录、建单、
   附件上传/预览、AI Provider `available` 与一次真实调用。
7. **记录**：保存 git SHA、镜像 digest、迁移 result、初始化 run ID、Smoke 结果与回滚点。
