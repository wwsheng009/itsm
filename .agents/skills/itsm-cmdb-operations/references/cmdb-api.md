# CMDB REST API 契约（references/cmdb-api.md）

> P0 调研稿（2026-10-17）。来源：`itsm-backend/router/cmdb_routes.go`（106 条路由、15 子组）+ `dto/cmdb_core_dto.go`。行号会漂移，以符号定位。全局信封 `{code,message,data}`；成功 `code=0`；错误码映射与工单域一致。
> 待补：写面 DTO 字段表、响应字段表、实测 GAPS。

## 0. 认证 / 权限

- 认证同全站：PAT / JWT，Bearer；租户绑定令牌无需 `X-Tenant-Code`。
- 权限码：`cmdb:read`（全部读面）/ `cmdb:write`（创建/更新/批量/生命周期/revert/导入导出/云资源/发现写）/ `cmdb:delete`。
- 路由分组（`/api/v1/cmdb` 下）：`capabilities / ontology / relationship-types / ci-types / attributes / tags / views / import / export / cis(+batch) / relationships / cloud-accounts / cloud-services / cloud-resources / discovery / reconciliation`。
- **弃用别名**：`/api/v1/configuration-items/*`（同 handler，勿新增端点）。

## 1. CI 读面（脚本已封装）

| 端点 | 说明 |
|---|---|
| `GET /cmdb/cis` | 列表，form 过滤：`page(default 1)`、`size(default 20, ≤200)`、`ciTypeId`、`ciType`、`status`、`environment`、`criticality`、`cloudProvider`、`cloudAccountId`、`cloudRegion`、`assignedTo`、`ownedBy`、`search`（宽语义模糊：名称/资产标签/序列号/型号/厂商/云资源ID/位置/负责人/归属人）、`ciNumber`（**精确自然键**）、`sortBy(id/name/status/environment/criticality/created_at/updated_at)`、`sortOrder(asc/desc)`、`dateFrom/dateTo`、`tagIds[]`、`withRelations` |
| `GET /cmdb/cis/:id` | 详情 |
| `POST /cmdb/cis/search` | 高级搜索：`{filters(CISearchFilter), page, pageSize(≤100), sortBy, sortOrder}` |
| `GET /cmdb/cis/stats` | 分组统计（status/ci_type/environment…，注意响应部分键为 snake_case，ent GroupBy 列） |
| `GET /cmdb/cis/:id/history` | 变更历史 |
| `GET /cmdb/cis/:id/relationships` | 该 CI 的关系（出/入边） |
| `GET /cmdb/cis/:id/topology` | 拓扑 |
| `GET /cmdb/cis/:id/impact-analysis` | 影响分析 |
| `GET /cmdb/cis/:id/attachments`（+`/:ref`、`/:ref/download|preview`） | 域内别名附件（multipart `file` 上传走 write） |

## 2. 元数据读面（脚本已封装）

| 端点 | 说明 |
|---|---|
| `GET /cmdb/ci-types`、`GET /cmdb/ci-types/:id` | CI 类型列表/详情 |
| `GET /cmdb/ci-types/:id/attributes` | 类型属性定义 |
| `GET /cmdb/attributes/:id` | 属性定义详情 |
| `GET /cmdb/tags`、`GET /cmdb/tags/:id` | CI 标签 |
| `GET /cmdb/views`、`GET /cmdb/views/:id` | 保存视图（含 filters 快照） |
| `GET /cmdb/relationship-types` | 关系类型词表 |
| `GET /cmdb/relationships`、`GET /cmdb/relationships/:id` | 关系列表/详情 |
| `GET /cmdb/ontology` | AI-Native 本体自描述（类型×属性×关系词表×枚举值域×AI 工具） |
| `GET /cmdb/capabilities` | 能力自描述 |
| `GET /cmdb/discovery/health` | 发现健康 |
| `GET /cmdb/reconciliation` | 对账 |

## 3. 写面（2026-10-17 起脚本已封装核心，子命令 `ci-create/ci-update/ci-delete/batch-*/lifecycle-*/revert/rel-*`）

### 3.1 CI CRUD / 批量（JSON body）

| 端点 | body 契约（`dto/cmdb_dto.go` / `cmdb_advanced_dto.go`） |
|---|---|
| `POST /cmdb/cis` | `CreateCIRequest`：`name`(必填 ≤255)、`status`(必填)、`ciTypeId`、`description`(≤20000)、`environment/criticality/assetTag/serialNumber/model/vendor/location/assignedTo/ownedBy/source/discoverySource`、`attributes{}`（自定义属性 map）、云字段（`cloudProvider/cloudAccountId/.../cloudTags/cloudMetrics`）、`lifecycleStatus`(oneof `draft/online/maintenance/offline/scrapped`)、`effectiveAt/expireAt`；`type` 不传时由 CIType 推导 |
| `PUT /cmdb/cis/:id` | `UpdateCIRequest`：同上字段全部可选（指针语义）；**`version` 乐观锁**（不带=服务端按当前处理，带旧值=冲突） |
| `DELETE /cmdb/cis/:id` | 软删 |
| `POST /cmdb/cis/batch` | `{items: CreateCIRequest[]}`，≤100 |
| `PUT /cmdb/cis/batch` | `{ids: int[], updates: UpdateCIRequest}` |
| `DELETE /cmdb/cis/batch` | `{ids: int[]}`（DELETE + body） |
| `PUT /cmdb/cis/batch/lifecycle` | `{ids, status(必填), remark?}` |

### 3.2 生命周期 / 版本回滚

| 端点 | 契约 |
|---|---|
| `PUT /cmdb/cis/:id/lifecycle?status=&remark=` | **query 传参（非 JSON body）**；status 必填；返回 CI DTO（lifecycleStatus 可能不回显——2026-10-17 实测，确认状态以 lifecycle/history 为准） |
| `GET /cmdb/cis/:id/lifecycle/history` | 数组（无包络）：`{id, ciId, oldStatus, newStatus, operatorId, operatorName, remark, version, createdAt}` |
| `POST /cmdb/cis/:id/revert` | `RevertCIVersionRequest`：`{version(必填 ≥1), remark?}` |

### 3.3 关系

| 端点 | 契约（`dto/ci_relationship_dto.go`） |
|---|---|
| `POST /cmdb/relationships` | `{sourceCiId, targetCiId, relationshipType(必填), strength?, impactLevel?, description?, metadata?, isDiscovered?}` |
| `PUT /cmdb/relationships/:id` | 指针字段：`relationshipType/strength/impactLevel/description/metadata/isActive` |
| `DELETE /cmdb/relationships/:id` | — |

**relationshipType 枚举**：`depends_on / hosts / hosted_on / connects_to / runs_on / contains / part_of / impacts / impacted_by / owns`；**strength/impactLevel 枚举**：`critical/high/medium/low`。

### 3.4 导入导出（任务制，2026-10-17 起已封装）

| 端点 | 契约（`dto/cmdb_import_export.go`） |
|---|---|
| `POST /cmdb/import` | `ImportCIRequest`：`fileUrl`(**必填，合法 URL**)、`fileType`(必填 oneof `xlsx/csv`)、`updateMode`(必填 oneof `skip/overwrite/merge`)、`sheetName?` |
| `GET /cmdb/import`、`GET /cmdb/import/:task_id` | 任务列表 / 状态轮询（`{taskId,status,totalCount,createdAt,completedAt,expiresAt}`） |
| `POST /cmdb/export` | `ExportCIRequest`：`exportType`(必填 oneof `xlsx/csv`)、`filters?`(CISearchFilter)、`exportFields?` |
| `GET /cmdb/export`、`GET /cmdb/export/:task_id` | 同上任务制 |

**实测（2026-10-17 demo）**：export-create 返回 `taskId`（UUID）+ `status=pending` + `expiresAt=+7d`；无过滤条件全量导出任务可创建但轮询可能 `failed`（totalCount=0 场景）——调用方必须轮询 status 判断终态，勿假设创建即成功。

### 3.5 云资源三件套 / 发现（2026-10-17 起已封装）

| 端点 | 契约 |
|---|---|
| `POST/PUT/DELETE /cmdb/cloud-accounts[/:id]` | `CloudAccountRequest`：`provider/accountId/accountName` 必填、`credentialRef/regionWhitelist/isActive` 可选（更新为 `CMDBCloudAccountUpdateRequest` 指针字段） |
| `GET /cmdb/cloud-accounts[/:id]` | 响应含 `hasCredential`（不回明文） |
| `GET /cmdb/cloud-services[/:id]` | 云服务类型目录 |
| `POST/PUT/DELETE /cmdb/cloud-resources[/:id]` | Create：`{cloudAccountId, serviceId, resourceId}` 必填 + `resourceName/region/zone/status/tags/metadata/lifecycleState?`；**跨租户账号在 handler 边界拒绝**（handler_cloud_resource_identity_test） |
| `GET /cmdb/cloud-resources` | 分页 + provider/region 过滤 |
| `GET/POST /cmdb/discovery/sources` | `DiscoverySourceRequest`：`name/sourceType` 必填、`reconcilePolicy` oneof `manual/discovered_wins/cmdb_wins`、`schedule/serviceCodes/regions/cloudAccountId/staleThreshold?` |
| `POST /cmdb/discovery/jobs` | `{sourceId}` |
| `GET /cmdb/discovery/results` | 发现结果列表 |

## 4. GAPS（P0 初版，随实测回填）

1. `GET /cmdb/cis/stats` 响应分组键混用 snake_case（`ci_type`，ent GroupBy 列直出）——调用方按两套键兼容。
2. `sortBy` 白名单为 `id/name/status/environment/criticality/created_at/updated_at`（binding oneof），传其余值 400。
3. `size` 上限 200（form）、`pageSize` 上限 100（POST search）——两套分页参数名不一致，脚本已分别透传。
4. **POST search 未知 filter 键静默忽略（2026-10-17 实测）**：`CISearchFilter` 合法键= `keyword/ciTypeId/status/environment/criticality/assetTag/serialNumber/vendor/location/assignedTo/ownedBy/tagIds/attributes/dateFrom/dateTo/cloudProvider/cloudRegion/cloudResourceId`；传未知键（如 `ciType`）**不报错、不过滤**（Go JSON 绑定非严格），返回全量。排障时先核对键名。
5. **lifecycle-update 响应不回显 lifecycleStatus（2026-10-17 实测）**：切换成功返回 CI DTO 但无 `lifecycleStatus` 字段；确认当前状态以 `GET /:id/lifecycle/history` 为准（实测切换后 history 立即出现 `oldStatus→newStatus` 行）。
6. **云发现能力未配置时云面为空**（2026-10-17 实测）：`cloud-accounts/services/resources`、`discovery-sources/results` 返回 `null`（非空数组）；`capabilities` 显示 `cmdbDiscovery: unconfigured / tenantReadiness:false`——先配置云账号与服务目录再走发现链路。CI 类型/属性/标签/视图写面与 `POST /:id/tags` 仍为 curl 口径（低频，按需补）。
