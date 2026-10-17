# PE-26 设计：采购/资产结项登记（结构化台账链接）

- 状态：**已实现 v1.0（2026-10-28）**；本文是 PE-26 的权威设计，§9 登记验证证据。
- 上游依据：`01-module-blueprint.md` §P2「采购/资产登记｜结项清单中登记资产/许可（链接既有 `asset`/`asset_license`/`vendor`/`contract`）；不做采购流程」；`07-completeness-review.md` §2-15/§4.2-B5；`06-implementation-roadmap.md` §2 PE-26 行 / §4.3 验收「结项清单中资产/许可登记可链接既有 asset/license/vendor/contract」；[p1-closure-design.md](./p1-closure-design.md) §10「P2/PE-26：资产/许可/合同/供应商台账链接（`project_documents`/链接表），把『资产登记确认』从人工留痕升级为结构化链接」。

## 1. 范围与目标

**范围内**

- 新表 `project_asset_links`：一项目多行，`link_type ∈ {asset, asset_license, vendor, contract}` + 弱引用 `target_id` + 备注 + 软删 + 未删行唯一；
- **API 4 条**：登记列表 / 新增 / 删除 / 候选选择器（统一选择器，覆盖四个既有台账域）；
- 结项清单读响应扩展：`assetsLinks`（明细）+ `assetsLinkSummary`（按类型计数），「资产/许可登记」清单项 detail 文案携带登记条数；
- 审计 `asset_link_create|asset_link_delete`（`resource=project`）；
- 迁移双文件（ent 建表兜底 + 索引）+ RLS 015 覆盖清单 + `cmd/rls-apply` 受管清单；
- 前端：工作台「结项清单」Tab 升级——「资产/许可登记」区新增**登记明细**（列表 / 添加 / 删除）+ 既有确认/豁免动作并存。

**范围外（明确不做）**

- 采购流程（采购申请/比价/订单/收货）——B5 裁决「不做采购流程」；
- 跨租户引用：MSP SLA 合同（`msp_contract`，provider 租户）**不在**链接目标内；`contract` 链接目标 = **同租户** `contracts` 表（intake/支持合同另有 `support_contract` 域，不在本表语义内）；
- 合同/供应商/资产/许可主数据自身的 CRUD 与页面（既有域负责；`contract` 域当前仅数据面无 HTTP 面，PE-26 只做只读候选）；
- 交付物索引 `project_documents`（仍为 P2 可选表，不由本切片落地）。

## 2. 决策（D-49 ~ D-53）

| # | 决策点 | 结论 | 依据 |
|---|---|---|---|
| **D-49** | 落库形态 | 新表 `project_asset_links`（一项目多行）；`(tenant_id, project_id, link_type, target_id)` **未删行唯一**；`note` ≤512；软删（`deleted_at`/`deleted_by`）。不落 `project_documents`——其语义是交付物索引 | 采购登记 =「项目 → 既有台账」多对多链接，独立表贴合 + 软删留痕；02 §3.6 `project_documents` 语义不同 |
| **D-50** | 引用完整性 | **弱引用 + 同租户存在性校验**（PE-21 `related_ticket_id` 先例）：写入校验目标存在且 `tenant_id` 相同，否则 400 `invalid_target`；不建外键（ent 无跨表约束，源域生命周期独立）；读时**批量富化**标签，目标缺失标 `targetMissing=true` 不阻断读 | 单租户原则（不做跨租户引用）+ 资产/供应商可被删除但历史登记应保留 |
| **D-51** | 门禁语义 | **保持 P1 不变**：清单项 `assets` 仍以「已确认（confirm）或已豁免（waive）」判定通过；链接是登记**明细**（`assetsLinks`/`assetsLinkSummary`），不新增门禁条件 | 06 §4.3 验收原文是「可链接」；P1 门禁已验收不回退，存量已确认项目不受升级影响 |
| **D-52** | 候选选择器 | `GET /projects/:id/asset-link-candidates`（`project:read`，可见性同详情 404）：`type` 四词表必填；`q` 模糊（编号/名称/标题）；`limit` 默认 20、上限 50；返回**最小字段** `{id,label,sublabel}`（不回传价格/密钥/联系方式/合同金额）；各域查询强制 `tenant_id` 过滤 | 项目模块需要统一选择器（`contract` 无 HTTP 面、`vendor` 前端无页面）；字段最小化控制跨域读暴露面（D-记录） |
| **D-53** | 写权限与终态 | 新增/删除 = `project:write` + 可见性谓词；**cancelled 项目拒绝写（400）**；`completed` 允许（结项后补登记是真实场景：采购/发票后到，结项动作同口径） | 与 PE-15 结项动作一致（仅 cancelled 拒绝）；审计留痕替代事后禁止 |

## 3. 数据模型 `project_asset_links`（P2 新表）

```
tenant_id / project_id                BIGINT NOT NULL   -- 行归属租户 + 项目（RLS tenant 谓词）
link_type                             VARCHAR(16) NOT NULL -- asset|asset_license|vendor|contract
target_id                             BIGINT NOT NULL   -- 弱引用目标（同租户）
note                                  VARCHAR(512) DEFAULT ''
created_by                            BIGINT NOT NULL
created_at                            TIMESTAMPTZ NOT NULL DEFAULT now()
deleted_at                            TIMESTAMPTZ NULL  -- 软删
deleted_by                            BIGINT NULL
```

- 索引：
  - `UNIQUE (tenant_id, project_id, link_type, target_id) WHERE deleted_at IS NULL`（未删行唯一；软删后允许重新登记）；
  - `(tenant_id, project_id, created_at)`（列表读数）。
- 迁移：ent auto-migrate 建表 + `migrations/20261028_project_management_pe26.sql` 兜底（表不存在跳过 + 索引 ensure，幂等；`+_down` 反向）。
- RLS：`015_project_management_policies.sql` 覆盖清单追加 `project_asset_links`（只 ENABLE 不 FORCE，`tenant_id = get_current_tenant_id()` fail-closed）+ `cmd/rls-apply` 受管清单同步。
- 分页：登记列表**不分页**（项目内登记条数少），硬上限 200 + `truncated` 显式；不新增分页别名。

## 4. API 契约

| # | 方法与路径 | 权限 | 说明 |
|---|---|---|---|
| 1 | `GET /projects/:id/asset-links` | `project:read` | 登记列表（可见性同详情 404）。响应：`{items:[ClosureAssetLink], summary:{asset,asset_license,vendor,contract}, total, truncated}` |
| 2 | `POST /projects/:id/asset-links` | `project:write` | 新增登记 `{linkType,targetId,note?}`；重复（未删行同目标）409 `duplicate_link`；目标不存在/跨租户 400 `invalid_target`；类型非法 400。成功返回 GET 同构响应（含新行） |
| 3 | `DELETE /projects/:id/asset-links/:linkId` | `project:write` | 软删（记 `deleted_at/by`）；不存在/已删 404；成功返回 GET 同构响应 |
| 4 | `GET /projects/:id/asset-link-candidates?type=&q=&limit=` | `project:read` | 候选选择器（D-52）。响应 `{items:[{id,label,sublabel}], truncated}` |

**ClosureAssetLink 响应行**（列表与结项清单共用）：

```json
{"id":1,"linkType":"asset","targetId":3,"label":"AST-0001","sublabel":"笔记本 / in-use",
 "note":"","targetMissing":false,"createdBy":1,"createdAt":"2026-10-28T10:00:00Z"}
```

**结项清单响应扩展**（`GET /projects/:id/closure` 及四个动作响应同构）：新增 `assetsLinks`（数组，上限 200）、`assetsLinkSummary`（按类型计数）；清单项 `assets` 的 `detail` 在已登记时携带「已登记 N 条（asset×a, asset_license×b, vendor×c, contract×d）」。

**错误码**：`invalid_link_type` / `invalid_target` / `duplicate_link`（409）/ `link_not_found`（404）/ 项目不可见（404）/ 项目已取消（400）。

## 5. 审计与可观测

- `resource=project`；`asset_link_create`（含 `linkType`/`targetId`/`label`/`note` 长度）、`asset_link_delete`（含 `linkId`/`linkType`/`targetId`）；不落敏感字段。
- 幂等性：重复登记 409（不静默去重）；软删后可重新登记（新行）。

## 6. 前端（结项清单 Tab）

- 「资产/许可登记」区块升级为两段：
  1. **登记明细**：表格（类型 / 编号·名称 / 备注 / 登记人 / 时间 / 删除）+「添加登记」按钮 + 空态提示；`targetMissing=true` 行显示「目标已删除」警示标签（灰显）。
  2. **确认/豁免**：保留既有动作（文案更新：可先登记明细，确认时备注可选；豁免必填原因）。
- 添加登记弹窗：类型 `Select`（资产/许可证/供应商/合同）+ 候选远程搜索 `Select`（300ms 防抖，调候选端点；label/sublabel 展示）+ 备注输入；重复/无效目标后端错误透传提示。
- 删除二次确认；只读角色与 `cancelled` 项目仅展示。

## 7. 实现映射（文件清单）

- 后端：`ent/schema/project_asset_link.go`（新）→ `go generate ./ent`；`migrations/20261028_project_management_pe26.sql`（+`_down`）；`database/rls/migrations/015_project_management_policies.sql` + `cmd/rls-apply/main.go` 清单；`handlers/project/asset_link_entity.go` / `asset_link_service.go` / `asset_link_handler.go`；`repository.go` + `repository_impl.go`（列表/CAS 无，新增/软删/唯一冲突判定 + 四域批量富化 + 候选查询）；`closure_service.go`（清单视图并入明细）；`router/common_system_routes.go`（4 路由，均挂既有 `project` 权限）。
- 前端：`lib/services/project-service.ts`（4 方法 + 类型）、`pages/(main)/projects/detail.tsx`（结项清单区块）、`lib/services/__tests__/project-service.test.ts`。
- 文档：本节 + `06`/`02`/`01`/`03`/`07`/`README` 索引 + `CHANGELOG`/`ROADMAP`/根 `README`。

## 8. 验证矩阵（计划）

- `go test ./handlers/project/ -run 'AssetLink'`：① 登记 CRUD（新增/重复 409/软删/再登记）+ 同租户/跨租户/不存在目标 400；② 候选选择器（四类型、`q` 过滤、`limit` 上限、不可见项目 404）；③ 结项清单集成（明细/摘要/detail 文案；confirm 无链接仍通过 = 门禁不变）；④ 审计断言（create/delete）；⑤ cancelled 拒绝写 + completed 允许（补登记）。
- 门禁：`go build -p 2 ./...`；`cmd/authz-gen` + ACL 重生成（**946 路由**，预计 +4）；Swagger 重生成；前端 `tsc`/改动文件 `eslint`/`project-service` 单测（**预计 63+**）/`check-api-paths`（**1074/1074** 预计）/`npm run build`；`docs/project-management` 链接校验 0 broken。

## 9. 验证证据（收尾填写）

| # | 项 | 命令/方式 | 结果 |
|---|---|---|---|
| 1 | 单元/集成（服务层） | `go test ./handlers/project/ -run 'AssetLink' -count=1` | ✅ 4 用例：登记工作流（CRUD/词表/重复 409/跨租户与不存在目标 400/软删后可重登记/cancelled 拒绝写/completed 允许补登记/200 上限截断）· 候选选择器（四类型最小字段/关键词 `ContainsFold`/limit 截断/跨租户不可见）· 结项清单集成（`assetsLinks`/`assetLinkSummary`/detail 条数/目标缺失 `targetMissing`/**零登记确认仍通过**=门禁不变）· HTTP 端点（400/404/409 映射 + 审计 `asset_link_create\|asset_link_delete` 留痕） |
| 2 | 包/守卫回归 | `go test ./handlers/project/ ./middleware/ ./router/ -count=1` | ✅ 全包 + 预检新鲜度（`TestPrecheckMapIsFresh`）+ 路由守卫全绿（`middleware` 首次因未重生成预检而红 → `cmd/authz-gen` 后转绿） |
| 3 | 构建 | `go build -p 2 ./...` | ✅ exit 0 |
| 4 | 授权生成与清单 | `go run ./cmd/authz-gen` + `node scripts/generate-acl-manifest.js` | ✅ 预检重生成；ACL 清单 **946 路由**（+4，权限覆盖 93.76%） |
| 5 | Swagger | `go run github.com/swaggo/swag/cmd/swag init -d . -g main.go -o docs --parseDependency --parseInternal` | ✅ 3 条路径与注解对齐：`/api/v1/projects/{id}/asset-links`（GET/POST）、`/api/v1/projects/{id}/asset-links/{linkId}`（DELETE）、`/api/v1/projects/{id}/asset-link-candidates`（GET） |
| 6 | 前端 | `npx tsc --noEmit` / 改动文件 `eslint` / `jest project-service` / `jest api-contract` / `npm run build` | ✅ 单测 **62/62**（新增 4：列表/新增/删除/候选查询参数）；API 契约 0 mismatch；生产构建通过（52.1s） |
| 7 | 迁移 / RLS | `migrations/20261028_project_management_pe26.sql`（+`_down`，幂等 ensure 索引与 partial unique）；`database/rls/migrations/015_*.sql` 覆盖清单 + `cmd/rls-apply/main.go` 受管清单追加 `project_asset_links` | ✅ **真实库已执行（P2 收口）**：embedded-postgres 55432 上 ent 全量迁移 + 五份 P2 兜底 SQL + RLS 001–015 全量应用与 `-verify`；`project_asset_links` 建表/索引（含 `WHERE deleted_at IS NULL` 的 partial unique）/RLS 策略经探针断言 |
| 8 | 文档 | 本文件 + 01 §P2 / 02 §3.10+清单 / 03 §1-21+§2.22 / 06 PE-26 条目+§4.3 / 07 §2-15+B5+回写 / README 索引+D-49~D-53 / CHANGELOG / ROADMAP / 根 README；`docs/project-management` 链接校验 | ✅ 0 broken |
| 9 | P2 收口（真实库 + HTTP + 浏览器） | `.tmp/pgverify`：`p2-probe.js` / `p2-e2e.js` / `p2-ui-smoke.cjs`（+ `p2-static-proxy.js`） | ✅ 探针 **11/11**（含登记表同租户写读/重复拒绝/跨租户读写隔离）· HTTP E2E **38/38**（含四类型登记、重复 409、无效目标 400、候选命中、标签富化、结项清单明细/确认、软删与二次删 404、审计七类 action）· UI 冒烟 **15/15**（登记明细行 + 添加登记弹窗 + 候选下拉） |
