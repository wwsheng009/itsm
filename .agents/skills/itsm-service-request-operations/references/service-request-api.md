# 服务请求 + 服务目录 REST API 契约（references/service-request-api.md）

> 基线：2026-10-17（router/service_request_routes.go 20 路由 + service_catalog_routes.go 10 路由 + provisioning 附属）。全局信封 `{code,message,data}`。

## 0. 认证 / 权限

- 读 `service_request:read` / 写 `service_request:write` / 删除 `service_request:delete` / **审批独立 `service_request:approve`**（POST /:id/approval 与 /:id/approvals 同 handler）。
- 目录：`service_catalog:read/write/delete`。

## 1. 服务请求状态机（单一事实源 `handlers/service_request/service.go:isValidServiceRequestOperationalTransition`）

常量源 `domain/servicerequest/status.go`：

```
submitted → cancelled
manager_approved → cancelled
it_approved → cancelled
security_approved → provisioning, cancelled
provisioning → delivered, failed
failed → provisioning(重试), cancelled
rejected / delivered / cancelled → 终态
```

- 多级审批链：`submitted → manager_approved → it_approved → security_approved`（由 `POST /:id/approval {action}` 驱动，服务端按当前态推进到下一级）。
- 运营状态迁移走 `PUT /:id/status {status}`（仅允许上表合法转换，违规 409 语义）。

## 2. 端点契约

| 端点 | 契约要点 |
|---|---|
| `POST /service-requests` | `CreateServiceRequestRequest`：`catalogId?`（目录下单）、`title?`(≤255)、`reason?`(≤20000)、`formData{}`、合规字段 `costCenter/dataClassification(public|internal|confidential|restricted)/needsPublicIp/sourceIpWhitelist/expireAt/complianceAck` |
| `GET /service-requests`（/me 别名） | query：`page/size(≤100)/status/scope` |
| `GET /service-requests/approvals/pending` | 待审批列表（同分页） |
| `GET/PUT/DELETE /service-requests/:id` | Update 指针字段（同 Create 字段） |
| `PUT /service-requests/:id/status` | `{status(必填)}`，运营状态机校验 |
| `GET /service-requests/:id/approvals`、`POST /:id/approval`（/approvals 同） | `{action(必填 approve|reject), comment?(≤2000)}` |
| `POST /service-requests/:id/provision`、`GET /:id/provisioning-tasks` | 开通任务（provisioning handler） |
| `POST /provisioning-tasks/:id/execute` | 独立路径执行任务 |
| 附件 | `/service-requests/:id/attachments`（域内别名，multipart `file`） |

## 3. 服务目录（/api/v1/service-catalogs）

| 端点 | 契约 |
|---|---|
| `GET /service-catalog`、`GET /service-catalogs`（同 handler） | 列表 |
| `POST /service-catalogs` | `name`(必填 ≤255)、`category`(必填 ≤100)、`description/icon`、`serviceType`(oneof `vm/rds/oss/network/storage/security/custom`)、`price/deliveryTime/unit`、`requiresApproval/approvalLevel` |
| `GET /service-catalogs/search?keyword=` | 搜索 |
| `GET /service-catalogs/stats` | 统计 |
| `GET/PUT/DELETE /service-catalogs/:id` | Update 指针字段 |
| `GET /service-catalog-services[/:id]` | 目录-服务映射（只读） |

## 4. GAPS（调用方注意，含 2026-10-17 实测）

1. **审批与运营状态是两条正交链**：审批链由 `POST /:id/approval` 逐级推进（服务端决定下一级），运营状态由 `PUT /:id/status`（仅 cancelled/failed 重试/provisioning→delivered 等白名单）；把 approved 当 status 传会被拒。
2. 列表分页参数是 **`size`**（≤100），与 tickets 的 `pageSize` 不同。
3. **`catalogId` 实际必填（2026-10-17 实测）**：尽管 DTO 标记 omitempty，服务端强制 `catalogId is required`——自由请求也必须挂目录条目。
4. **合规确认必填（实测）**：创建含 dataClassification 的请求需 `complianceAck=true`，否则 400 `Compliance acknowledgement required`。
5. provisioning 任务执行是独立路径 `/provisioning-tasks/:id/execute`（不在 /service-requests 前缀下）。
6. **目录列表响应键为 `catalogs`**（非 items）——实测 `{catalogs[], total, page, size}`。
7. demo 实测审批链：`approval approve` 一次推进 submitted→manager_approved（approvalLevel=1 目录单级即到 manager）；**运营白名单 409/4090 守卫实测**（submitted→delivered 被拒）→ cancelled 合法。
