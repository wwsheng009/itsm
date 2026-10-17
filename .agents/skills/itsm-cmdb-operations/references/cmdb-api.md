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

## 3. 写面（**暂未封装脚本**，curl 口径）

| 端点 | 权限 |
|---|---|
| `POST/PUT/DELETE /cmdb/cis[/:id]` | write/delete |
| `POST/PUT/DELETE /cmdb/cis/batch`（batch.delete 走 DELETE + body）、`PUT /cmdb/cis/batch/lifecycle` | write/delete |
| `POST /cmdb/cis/:id/revert`（版本回滚）、`GET/PUT /cmdb/cis/:id/lifecycle[/history]` | write |
| `POST/DELETE /cmdb/cis/:id/tags` | write |
| CI 类型/属性/标签/视图 CRUD | write/delete |
| `POST /cmdb/relationships`、`PUT/DELETE /cmdb/relationships/:id` | write/delete |
| `GET/POST /cmdb/import[/:task_id]`、`GET/POST /cmdb/export[/:task_id]`（任务制，POST 后轮询 task_id） | read/write |
| `GET/POST/PUT/DELETE /cmdb/cloud-accounts|cloud-services|cloud-resources[/:id]` | read/write/delete |
| `GET/POST /cmdb/discovery/sources`、`POST /cmdb/discovery/jobs`、`GET /cmdb/discovery/results` | read/write |

## 4. GAPS（P0 初版，随实测回填）

1. `GET /cmdb/cis/stats` 响应分组键混用 snake_case（`ci_type`，ent GroupBy 列直出）——调用方按两套键兼容。
2. `sortBy` 白名单为 `id/name/status/environment/criticality/created_at/updated_at`（binding oneof），传其余值 400。
3. `size` 上限 200（form）、`pageSize` 上限 100（POST search）——两套分页参数名不一致，脚本已分别透传。
4. **POST search 未知 filter 键静默忽略（2026-10-17 实测）**：`CISearchFilter` 合法键= `keyword/ciTypeId/status/environment/criticality/assetTag/serialNumber/vendor/location/assignedTo/ownedBy/tagIds/attributes/dateFrom/dateTo/cloudProvider/cloudRegion/cloudResourceId`；传未知键（如 `ciType`）**不报错、不过滤**（Go JSON 绑定非严格），返回全量。排障时先核对键名。
5. 写面/响应 DTO 字段表待补；本文件为 P0 调研稿，写面封装后回填 §3 详情与实测证据。
