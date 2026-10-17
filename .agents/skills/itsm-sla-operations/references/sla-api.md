# SLA REST API 契约（references/sla-api.md）

> 基线：2026-10-17（router/sla_routes.go，sla 域 39 路由 + SLATemplate 独立注册）。全局信封 `{code,message,data}`；成功 `code=0`。
> MSP 专属子面（grants/contracts/settlement-report）需 MSP 部署模式：非 MSP 部署 404（`RequireMSPDeploymentMode`）。

## 0. 认证 / 权限

- 读 `sla:read`；定义/告警规则写 `sla:write`；删除 `sla:delete`；**违约裁决独立权限 `sla:violation:settle`**（accepted/waived 归责终态）；MSP 子面另有 `sla:grant:read/write`、`sla:contract:read/write`、`sla:settlement:read`（联合 `RequireMSPPermission`）。

## 1. 路由面（规范前缀 /api/v1/sla）

| 端点 | 契约要点 |
|---|---|
| `GET /sla`、`GET /sla/definitions`、`GET /sla/policies`（兼容） | 定义列表 |
| `POST /sla/definitions`、`POST /sla/policies` | `CreateSLADefinitionRequest`：`name`(必填)、`responseTime/resolutionTime`(必填 ≥1 分钟)、`description/serviceType/priority`、`businessHours/escalationRules/conditions`（map） |
| `GET/PUT/DELETE /sla/definitions/:id`（policies 同） | Update 为指针字段（含 `isActive`） |
| `GET /sla/definitions/provided` | **customer 面只读**：服务商下发定义（D-SLA-1；独立端点，不开列表跨租户读洞） |
| `GET /sla/stats` | 统计 |
| `POST /sla/monitoring`（兼容 /monitor） | body 可空 `{startTime?, endTime?}`（RFC3339）；历史伪值（30d/now）不再支持——**必须 RFC3339 或留空** |
| `GET /sla/metrics` | query：`slaDefinitionId/metricType`（camelCase 约定，无 snake 别名）；响应 `{metrics[], count}` |
| `GET /sla/violations` | query：`isResolved(bool)/severity/violationType/slaDefinitionId/page/pageSize`（camelCase 优先，个别字段兼容 snake）；响应 `{items, total, page, pageSize}` |
| `PUT /sla/violations/:id` | `{isResolved?, notes?, resolutionState?, disputeReason?}`；**resolutionState 仅接受 `disputed`**（需 disputeReason）；accepted/waived 必须走 settle |
| `PUT /sla/violations/:id/settle` | `SettleViolationRequest`：`{decision(必填 oneof accepted/waived), reason?}`；`sla:violation:settle` |
| `GET /sla/performance` | query：`dimension`(必填 oneof `serviceType|priority`) + `serviceType/priority` 预过滤；供监控大屏 |
| `POST /sla/check-compliance/:ticketId` | 无 body；返回 `SLAComplianceResult`（含 `actual_response_minutes`） |
| `GET /sla/alert-rules[/:id]` + CRUD | 规则管理（write/delete） |
| `GET /sla/alert-history`、`GET /sla/compliance-report` | 历史/合规报表 |
| `GET /sla/grants`（+CRUD） | **MSP 面**：定义下发授权（D-SLA-1）；非 MSP 404 |
| `GET/POST/PUT /sla/contracts[/:id]` | **MSP 面**：SLA 合同链（D-SLA-4）；grant 写端点已 410，写入口唯一化到合同 |
| `GET /sla/settlement-report`、`POST /sla/settlement-report/lock` | **MSP 面**：结算报表（白名单口径+双口径对账，SLA-A6 客户归属 fail-closed）；lock=出快照锁定（§13.3，锁定后该周期违约不可变更） |

## 2. GAPS（调用方注意）

1. **monitoring 时间参数必须 RFC3339 或留空**（历史 "30d"/"now" 伪值已废弃，仓储层从未使用）。
2. **违约处置权限分离**：`disputed` 走 `PUT violations/:id`；`accepted/waived` 走 `PUT violations/:id/settle`（独立 `sla:violation:settle` 权限）——混用端点会被 400 拒绝。
3. metrics 响应键为 `{metrics, count}`；violations 为 `{items, total, page, pageSize}`——两套键。
4. `CreateSLADefinitionRequest.conditions/businessHours/escalationRules` 为自由 map（服务端不校验 schema）——调用方自证结构。
5. MSP 子面（grants/contracts/settlement）在非 MSP 部署一律 404，脚本直通输出错误。
6. SLA 模板走独立 handler（`SetupSLATemplateRoutes`，路由前缀由 handler 注册），本 CLI 暂未封装。
