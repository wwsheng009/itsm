# 238 demo 增量部署证据：SLA 专项 P2-2 + P2-3 + P3（2026-10-14，1.8.0-dev.c700c060）

## 变更内容

- 范围：`1e60efee..c700c060` 共 5 提交（P2-2 结算报表 `6d06e194`、P2-3 类型受控下发 `71859a93`+`6b67ab26`、部署证据 `d86b8dc3`、P3 前端补全 `c700c060`）。
- 交付镜像：`itsm-backend:1.8.0-dev.c700c060`（sha256:500056a1…）、`itsm-frontend:1.8.0-dev.c700c060`（sha256:7099c9ef…）；`itsm-ai-service` retag 对齐（代码未变）。
- 迁移（init 自动）：`20261014e`（matched_layer 快照）/ `20261014f`（类型下发）；RLS `013`（下发追踪表策略）手动注入。

## 部署路径

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `itsm-1.8.0-dev.c700c060-full.bundle`（基线=远端 1e60efee，143,859B） | fetch/checkout OK（HEAD=c700c060）|
| A | `deploy238_full_c700c060_a.sh`：checkout + VERSION + retag ai + 启动顺序构建 | `A-STATUS: DONE` |
| W | backend → frontend 顺序构建（约 9 分钟；backend Go 在容器内编译较慢） | `BUILD_BACKEND_EXIT=0`、`BUILD_FRONTEND_EXIT=0`、`BUILD_ALL_DONE` |
| C | `up -d backend/worker/frontend`（init 随依赖重跑迁移 e/f） | 三容器切到 c700c060 |
| D | 健康等待 + RLS 013 注入 + 全量验证 | `D-STATUS: DONE`（主 agent 独立复跑） |

执行方式：**子 agent**（session `session_20261014231834_ue3XhpJh`，run_status=succeeded）执行 upload/A/W/C；**主 agent** 独立复跑 D 复核（本文件证据以复跑输出为准）。RLS 013 经 stdin 回退通路注入（`docker cp` 在该 sudo 环境不可用，与上次部署同因）。

## 验证证据（238 侧）

- **容器与镜像**：`itsm-backend-demo` / `itsm-worker-demo` = `itsm-backend:1.8.0-dev.c700c060`，`itsm-frontend-demo` = `itsm-frontend:1.8.0-dev.c700c060`；`itsm-init-demo` `exited/exit=0`；backend/worker/frontend 健康检查全绿（D1 首轮即就绪）。
- **迁移**：`schema_migrations` 20261014 六行（a/b/c/d/**e/f**）。
- **结构**：`tickets.sla_matched_layer`、`sla_violations.matched_layer / resolution_state`、`ticket_types.origin / controlled_by_provider / sync_status` 列在位；`ticket_type_distributions` 表在位。
- **RLS**：`sla_definition_grants|rls=true`、`msp_contracts|rls=true`、`msp_contract_sla_bindings|rls=true`、`ticket_type_distributions|rls=true`；策略 `tenant_isolation_ticket_type_distributions` + 011/012 全量在b位。
- **HTTP 探针（无 token）**：`demo-http=200`、`readyz=200`；
  - `GET /api/v1/sla/settlement-report`：404 → **401**
  - `POST /api/v1/sla/settlement-report/lock`：404 → **401**
  - `GET /api/v1/msp/customers/1/ticket-types/distributions`：404 → **401**（新路由）
  - `PUT /api/v1/sla/violations/1/settle`：**401**（保持）
- **A/B 特征对比**：
  - 前端资产 token `sla-settlement`：旧镜像 `1.8.0-dev.1e60efee`=**0** / 新镜像=**1**（结算报表页 chunk）
  - 后端二进制 token `settlement-report`：旧镜像=**0** / 新镜像=**3**

## 遗留项

- `itsm-ai-service-demo` 容器 tag 仍显示 `22ee3603`（代码同源未重建；镜像已 retag 到新 tag）。
- **§8 端到端验收锚点（SLA-A1…A17）待执行**——runbook：`.dev/deploy/ACCEPTANCE-RUNBOOK-c700c060.md`（含凭证获取、机械锚点覆盖表、反例清单）。
