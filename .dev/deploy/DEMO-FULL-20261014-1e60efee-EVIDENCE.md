# 238 demo 全栈更新证据：SLA 专项 P0+P1+P2-1（2026-10-14，1.8.0-dev.1e60efee）

## 变更内容

- 范围：`b86819be..1e60efee` 共 **19 个提交**（P0 全套：归属模型/四级匹配引擎/API/前端类型受控枚举；P1 全套：合同模型/合同服务与 grant 并轨/前端合同页/Pause-Resume 真实计时接线；P2-1：违约归责状态机）。
- 交付镜像：`itsm-backend:1.8.0-dev.1e60efee`（sha256:5d8b658b…）、`itsm-frontend:1.8.0-dev.1e60efee`（sha256:c0ee0413…）；`itsm-ai-service` 仅 retag 对齐（代码未变）。
- 迁移（init 自动应用）：`20261014` / `20261014b` / `20261014c` / `20261014d`；RLS `011`/`012` 手动注入 `itsm_demo`。

## 部署路径（三段式 + 验证）

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `itsm-1.8.0-dev.1e60efee-full.bundle`（基线=远端 b86819be，260,875B，含 HEAD 1e60efee） | fetch/checkout OK |
| A | `deploy238_full_1e60efee_a.sh`：checkout + `.env.demo` VERSION + retag ai + 启动顺序构建 | `A-STATUS: DONE` |
| W | 轮询构建（backend → frontend 顺序，约 6 分钟） | `BUILD_BACKEND_EXIT=0`、`BUILD_FRONTEND_EXIT=0`、`BUILD_ALL_DONE` |
| C | `deploy238_full_1e60efee_c.sh`：`up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo`（init 随依赖重跑迁移） | 容器重建，镜像引用 1e60efee |
| D | `deploy238_full_1e60efee_d.sh`：健康等待 + RLS 011/012 注入 + 全量验证 | `D-STATUS: DONE`（主 agent 独立复跑确认） |

执行方式：**子 agent**（session `session_20261014215018_Ku75ikJR`）执行上传/A/W/C/D 全流程并报告 `DEPLOY-RESULT: SUCCESS`；**主 agent** 复核远端状态并独立复跑 D 验证（本文件证据以主 agent 复跑输出为准）。
偏差说明：D2 的 `docker cp` 通路在远端 `sudo -lc` 环境下不可用（结构性限制），子 agent 以等价 stdin 通路完成 RLS 注入；主 agent 复跑时 RLS 已就位（策略全部在位，注入幂等未重复执行）。

## 验证证据（238 侧，2026-10-14）

- **容器与镜像**：`itsm-backend-demo` / `itsm-worker-demo` = `itsm-backend:1.8.0-dev.1e60efee`，`itsm-frontend-demo` = `itsm-frontend:1.8.0-dev.1e60efee`；backend/worker/frontend 健康检查 healthy，`itsm-init-demo` `exited/exit=0`；nginx/redis/ai-service 未变更（常态运行）。
- **迁移**：`schema_migrations` 含 `20261014*` 4 行（a/b/c/d）。
- **结构**：`sla_definitions.owner_provider_tenant_id`、`ticket_types.sla_exempt`、`sla_violations.resolution_state/settled_flag/waived_by` 列在位；`msp_contracts` / `msp_contract_sla_bindings` / `sla_definition_grants` 三表在位。
- **RLS**：`sla_definition_grants|rls=true`、`msp_contracts|rls=true`、`msp_contract_sla_bindings|rls=true`；7 条策略在位（`sla_grant_provider_all` / `sla_grant_customer_read` / `sla_definition_customer_read` / `msp_contract_provider_all` / `msp_contract_customer_read` / `msp_contract_binding_provider_all` / `msp_contract_binding_customer_read`）。注：`sla_definitions|rls=false` 为演示库既有状态（011 对该表只增策略不 ENABLE；demo `RLS_MODE=shadow`），与部署前一致。
- **HTTP 探针**：`http://127.0.0.1:8089/`=200；后端 readyz=200。
  新路由（无 token，部署前基线 404 → 部署后 **401**，证明新路由已生效）：
  - `PUT /api/v1/sla/violations/1/settle`：404 → **401**
  - `GET /api/v1/sla/contracts`：404 → **401**
  - `GET /api/v1/sla/grants`：404 → **401**
- **A/B 特征对比**（P8 法）：
  - 前端资产 token `sla-contracts`：旧镜像 `1.8.0-dev.b86819be`=**0** / 新镜像=**1**
  - 后端二进制 token `violations/:id/settle`：旧镜像=**0** / 新镜像=**2**

## 遗留项

- `itsm-ai-service-demo` 容器 tag 仍显示 `22ee3603`（代码同源未重建；镜像已 retag 到新 tag）；如需对齐可 `docker compose --env-file .env.demo -f docker-compose.demo.yml up -d itsm-ai-service-demo`。
- demo 登录口令以 `.env.demo` 当次 init 播种为准（P9：登录 401 ≠ 部署失败）。
- P0 验收锚点（SLA-A1/A2/A3/A4/A8/A12/A7）为后续验收测试项，不在本次部署范围内闭环。
