# 238 新库三类单据读面冒烟运行摘要（2026-10-12）

> 状态：**PASS（15/15）**｜定位：全新生产库 `itsm_prod_v2` 上「问题/变更/服务请求」跨租户读面冒烟（对齐 OB-6 场景），经公网入口 `http://172.18.3.238:8088`。
> 关联：[238 部署运行摘要](./run-summary.md)｜[建租户冒烟](./smoke-tenants.md)｜[建号+建单+协同冒烟](./smoke-users-tickets.md)

## 1. 结果（PASS=15 / FAIL=0）

| # | 用例 | 结果 | 证据 |
|---|---|---|---|
| 1 | 平台管理员登录 | PASS | — |
| 2–4 | 建号：服务商经理2 / 服务商技术员2 / 客户管理员 | PASS | id=10（provider_admin）、id=11（provider_agent）、id=12 |
| 5 | 服务商经理登录 | PASS | — |
| 6 | 分配：技术员2 → 客户 3（primary） | PASS | `POST /msp/allocations` 200 |
| 7 | 客户管理员登录 | PASS | — |
| 8 | 服务目录项就绪 | PASS | `catalogId=23`（复用/新建） |
| 9 | 造变更样本 | PASS | `id=1` `CHG-20261012-0001` |
| 10 | 造问题样本 | PASS | `id=1` `PRB-20261012-0001` |
| 11 | 造服务请求样本 | PASS | `id=1`（catalogId=23，`complianceAck=true`） |
| 12 | 技术员登录 | PASS | — |
| 13 | 读面可见**变更**样本 | PASS | `GET /msp/workbench/changes` items=1 含 `SMOKE-238-…` |
| 14 | 读面可见**问题**样本 | PASS | `GET /msp/workbench/problems` items=1 |
| 15 | 读面可见**服务请求**样本 | PASS | `GET /msp/workbench/service-requests` items=1 |

## 2. 说明

- 脚本：本机 `.dev/deploy/deploy238_smoke_records_readface.sh`（未入库），在 238 上执行；口令仅脚本内生成、不回显。
- 与本地 FDB-B1 的 OB-6 探针口径一致：三类单据在工作台读面按 `allocation` 收敛可见（本租户各 1 条样本，均可见）。
- 未覆盖（如后续需要）：三类单据的**写面**（状态推进，`POST /msp/{changes,problems,service-requests}/:id/status`）与审批类动作（保留客户治理面）。
- 资源：新增用户 id=10/11/12；样本 change=1 / problem=1 / SR=1（租户 3）；分配 id=11 → 客户 3。
