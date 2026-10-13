# 238 演示栈部署证据：dashboard 跨租户总览分区 + MSP 角色感知落点（2026-10-13）

> 状态：**已上线**｜入口 `http://172.18.3.238:8089`（独立栈 `itsm-demo`）
> 版本：`1.8.0-dev.75cefde7` → **`1.8.0-dev.33a373c4`**（仅前端变更，后端/ai-service 同镜像 retag）
> 代码：`feat/msp-p0-hardening` @ `75cefde7`（feat）+ `33a373c4`（fix）

## 1. 变更内容

| 提交 | 内容 |
|---|---|
| `75cefde7` | `/dashboard` 新增「跨租户总览」只读分区（复用 `GET /msp/workbench/summary`：客户数/未关闭/超期/临近/未指派 + SLA 风险 Top5 + 深链下钻）；MSP 员工登录默认落 `/msp/workbench`；工作台顶栏新增「租户内总览」反向入口 |
| `33a373c4` | 门控收敛：落点与分区按**数据面操作码** `msp_ticket:read`（工作台 summary/tickets 端点级守卫）判定，不再用区域码集（含 `msp:read`）；平台超管（`['*']`）仍落 `/dashboard` |

## 2. 部署执行（幂等脚本，root/sudo）

1. 快照：`/home/n9e/itsm/repo/.env.demo.bak-20261013-145932`、`.env.demo.bak-20261013-151033`；
2. 代码：增量 bundle（48KB/3.9KB）→ `git fetch bundle` → `deploy-demo-b3`/`deploy-demo-b4` checkout；
3. 构建：`VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml -f docker-compose.test.yml build itsm-frontend`（两次均 `BUILD_EXIT=0`）；
4. 镜像：`itsm-frontend:$NEW` 新建；`itsm-backend`/`itsm-ai-service` 由 `5e4eab85` retag（代码未变）；
5. 上线：`sed VERSION=$NEW .env.demo` → `docker compose --env-file .env.demo -f docker-compose.demo.yml up -d`。

## 3. 验证证据

| 检查 | 结果 |
|---|---|
| 容器 | 6/6 healthy（frontend/backend/worker/ai-service/redis/nginx 均为 `1.8.0-dev.33a373c4`，init exit 0） |
| `/api/v1/version`（外网 8089） | `1.8.0-dev.33a373c4` |
| 站点 `/`、`/login` | 200 |
| 产物探针 | `itsm-frontend-demo` 内 `assets/index-CgGALlpm.js` 含「跨租户总览」 |
| MSP 员工登录（`demo.msp.manager`） | 落地 `/msp/workbench`；顶栏「租户内总览」→ `/dashboard` |
| dashboard 分区（MSP 视角） | 渲染：客户 3 / 未关闭 22 / 超期 5 / 临近 6（24h）/ 未指派 0；Top3 = 华辰 8/2/3、云帆 7/2/1、仁和 7/1/2；与工作台 SLA 看板一致 |
| 下钻深链 | 点击「华辰智能制造」→ `/msp/workbench?customerTenantIds=3` |
| 客户租户 admin（`demo.mfg.admin`） | 落地 `/dashboard`（非工作台）；dashboard 无跨租户分区、无请求；租户 KPI 正常（总工单 12） |

## 4. 缺陷与修复（部署期发现）

- **现象**：客户租户 admin 角色基线含区域码 `msp:read`（`/msp/status` 判其为「非MSP用户」），
  首版按 `MSP_AREA_PERMISSIONS` 判定，导致其登录落工作台、dashboard 分区渲染必然 403 的空数据。
- **修复**（`33a373c4`）：新增 `MSP_WORKBENCH_PERMISSIONS=['msp_ticket:read']`（服务商 5 个内置角色
  全持有，客户租户 `msp_*` 角色基线被守卫测试禁止持有，见 `internal/authz/roles.go` 与
  `msp_customer_scope_roles_test.go`），落点与分区门控统一收敛到该操作码；补 7+3 例回归测试。
- **遗留（未改，超出本次范围）**：`/msp` 区域路由守卫仍按 `MSP_AREA_PERMISSIONS`（含 `msp:read`）放行，
  客户租户 admin 手动访问 `/msp/*` 会看到页内「非 MSP 员工」错误——如需口收，建议单独任务处理。

## 5. 回滚

```bash
cd /home/n9e/itsm/repo
sudo sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.5e4eab85/' .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml up -d
```

> 镜像 `itsm-frontend:1.8.0-dev.5e4eab85` 及更早标签均保留；`.env.demo` 备份见 §2-1。
> 本次部署脚本与 bundle 位于本机 `.dev/deploy/`（deploy238_ui1 流程）与服务器 `/tmp/deploy_demo_ui1.sh`（均不入库）。
