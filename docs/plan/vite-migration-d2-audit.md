# FE-V1-03 · D2 旁路部署核查报告（是否存在直连前端 3000 的场景）

- 日期：2026-09-24
- 分支：`feat/vite-migration`
- 范围：仓库内所有可能绕过主 nginx、直接访问前端容器 `3000` 端口的部署、脚本与配置
- 结论：**不存在**。浏览器流量在生产全部经主 nginx（80/443）进入；`/api/` 本来就由主 nginx 直连后端，
  因此 **D2-a（不保留生产 BFF）成立**，已按 `docs/plan/vite-migration-plan.md` §5.9 方案 i 实施静态产物 + 容器内 nginx。

## 1. 证据清点

| # | 检查项 | 位置 | 结果 |
|:--|:--|:--|:--|
| 1 | 生产前端服务是否向宿主机发布 3000 | `docker-compose.prod.yml:387-394` | 仅 `expose: 3000`（容器网络内可见），**无** `ports:` 发布 |
| 2 | 主 nginx 的 `/` 上游 | `nginx/conf.d/default.conf:43` | `set $frontend_upstream http://itsm-frontend:3000;`（容器网络内地址，非宿主机） |
| 3 | 主 nginx 的 `/api/` 上游 | `nginx/conf.d/default.conf:52-54` | 直连 `itsm-backend:8090`，**不经过**前端容器 |
| 4 | 仓库内 `itsm-frontend:3000` 的其他出现位置 | `rg -n 'itsm-frontend:3000'` | 仅后端/`itsm-ai-service` 的 `CORS_ORIGINS` 白名单与文档注释 |
| 5 | dev 环境的 3000 | `docker-compose.dev.yml:366-368` | `ports: "3000:3000"` 发布到宿主机供本地开发（Vite dev + HMR）；`9229` 调试端口为 Next 时期遗留（Vite dev 未启用 `--inspect`，可后续清理） |

> `CORS_ORIGINS` 中的 `http://itsm-frontend:3000` 是容器间调用白名单，浏览器实际来源是主 nginx 的对外地址；
> 本次迁移未改变该配置，也不需要改变。

## 2. 被删除的服务端能力与承接方

| 能力 | 迁移前 | 迁移后 | 差异 / 风险 |
|:--|:--|:--|:--|
| `/api/[...path]` BFF（`PUBLIC_PATHS`、`BLOCKED_PATHS`、JWT 校验、`X-Forwarded-For`） | 仅 dev 命中（生产 nginx 直连后端，见 §1 第 3 条） | 删除；dev 改由 `vite.config.ts` 的 `server.proxy` 转发 `/api` → `ITSM_BACKEND_URL` | dev 不再有 `BLOCKED_PATHS` 服务端拦截，改由客户端守卫（`src/routes/guards.tsx`）+ 后端 RBAC 兜底；生产链路语义不变 |
| `/api/health`（Next route） | 前端容器健康检查 | `itsm-frontend/nginx/default.conf:36-40` 直接返回 `200 ok` | 探针路径不变，compose healthcheck 仍打 `/health` |
| `/uploads/**` 附件预览 | Next 容器内静态目录 | 前端容器 nginx `alias /usr/share/nginx/uploads/`（只读挂载 `./uploads`） | URL 前缀不变（D5 契约不受影响） |
| SSR / RSC 取数 | 无：166 个 `page.tsx` 中 `export default async function` = 0（方案 §12.1 实测） | 无 | 无影响 |

## 3. 待闭环

- Q2（方案 §11）：dev 环境 `BLOCKED_PATHS` 的等价后端鉴权需后端确认；生产链路不受影响。

## 4. 复现命令

```powershell
cd E:\projects\itsm
rg -n 'itsm-frontend:3000' .
Select-String -Path docker-compose.prod.yml -Pattern 'expose|ports' -Context 1,2
Select-String -Path nginx/conf.d/default.conf -Pattern 'upstream|proxy_pass' -Context 0,1
```
