# MSP 跨租户 REST API 契约（references/msp-api.md）

> 基线：2026-10-17（router/msp_routes.go，45 路由）。全局信封 `{code,message,data}`；成功 `code=0`。
> 全族挂 `MSPMiddleware`：**需 MSP 员工身份**，非 MSP 用户 403「仅MSP员工可访问」。MSP 跨租户访问不走租户中间件。

## 0. 权限码（端点级 RequireMSPPermission）

| 码 | 覆盖 |
|---|---|
| `msp:read` | status（免码）/ context |
| `msp_allocation:read/write` | 分配管理（allocations/history/deallocate） |
| `msp_customer:read/write` | 客户列表、建号（write）、角色候选、模板/工单类型下发 |
| `msp_ticket:read/write` | 工作台全族（跨客户工单/任务/变更/问题/服务请求）、指派、回复、状态 |
| `msp_report:read` | 报表 + 审计聚合 |

**授权链双层**：端点级 RBAC + 条目级服务端二次校验（allocation + 客户 active，mspguard 唯一入口）。

## 1. 路由面（/api/v1/msp）

| 子面 | 端点 | 要点 |
|---|---|---|
| 基础 | `GET /status`、`GET /context` | MSP 员工均可 |
| 分配 | `GET /allocations`、`GET /allocations/history`（含已解除）、`POST /allocations`、`POST /allocations/deallocate` | msp_allocation 族 |
| 客户 | `GET /customers`、`GET /customers/:ctid/tickets`、`POST /customers/:ctid/users`（IP-P0-5 建号，mspguard 二次校验）、`GET /customers/:ctid/roles`（建号角色候选，IP-P2-7） | msp_customer 族 |
| 模板/类型下发 | `GET/POST /customers/:ctid/workflow-templates[/:key/distribute]`（copy-in+来源追踪）；**工单类型受控下发 D-TT-1**：`POST .../ticket-types/:id/distribute` → `GET .../distributions[/:distId/preview]` → `POST .../:distId/publish`（发布同步）→ `POST .../:distId/unbind`（解绑转 native） | msp_customer 族；copy-in + 受控锁 |
| 工作台 | `GET /workbench/tickets`、`GET /workbench/summary`、`GET /msp/tickets/:id`（跨租户详情：声明租户+allocation 双重校验）、`GET /msp/tickets/:id/comments`（公开评论双向互通，**客户内部备注不下发** XFD8） | msp_ticket 族 |
| 工单操作 | `POST /msp/tickets/:id/assign`、`POST /:id/reply`、`POST /:id/status`、`POST /workbench/batch`（**≤100 条 + 低危动作白名单 + 逐条授权/审计 + 每租户限流** IP-P1-6） | msp_ticket:write |
| 视图 | `GET/POST /workbench/views`、`PUT/DELETE /workbench/views/:id`、`POST /views/:id/default`（IP-P2-4a，**灰度 WORKBENCH_VIEWS_ENABLED 默认关**） | msp_ticket 族 |
| 任务投影 | `GET /workbench/tasks`、`POST /tasks/:taskId/claim|complete`（写回在**客户租户内**执行，逐条审计 source=workbench，IP-P2-11） | msp_ticket 族 |
| 变更/问题/服务请求接入 | `GET /workbench/changes|problems|service-requests`、`GET /msp/changes|problems|service-requests/:id`、`POST /:id/status`（IP-P2-13/15 处理类动作 D8） | msp_ticket 族 |
| 报表/审计 | `GET /reports/customers`、`GET /reports/performance`、`GET /audit/summary`（越权尝试/冲突告警面板 IP-P1-8） | msp_report:read |

## 2. GAPS（调用方注意）

1. **审批类动作不在 MSP 面**：变更 approve/reject、服务请求 approval 保留客户治理面（IP-P2-13 明确不注册）——需用 itsm-change-operations 以客户身份操作。
2. `workbench/views` 依赖灰度开关 `WORKBENCH_VIEWS_ENABLED`（默认关）——未开启时 403/404。
3. `workbench/batch` 有 **每租户限流** + 低危动作白名单；传高危动作会被拒。
4. 任务 claim/complete 写回在客户租户上下文执行；审计 `source=workbench`。
5. 客户内部备注（isInternal=true）**不下发**给 MSP（XFD8）。
6. 建号（ProvisionUserToCustomer）需 allocation 二次校验；角色候选来自目标客户租户的角色列表（数据驱动）。
