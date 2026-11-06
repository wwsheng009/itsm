# 上游 heidsoft/itsm 新增内容清单

> **状态**：只读梳理（2026-11-06），**未合并任何代码**
> **对比基线**：merge-base `e7fd79e9`（2026-09-20）；上游 `heidsoft/main = 63b9feb5`，本地 `HEAD = 6e33fe2f`
> **方法**：全部结论来自 `git diff --diff-filter=A`、`git ls-tree` 目录对拍与源码注释核对，
> **不采信 commit message**（消息会说谎，文件不会）
> **体量**：上游新增 **955** 文件（同期本地新增 1971）；双方同路径各自新增 **0**

---

## 1. 新增后端能力

### 1.1 新 handler 域（目录级：上游有、本地无）

| 域 | 上游现有文件数 | 是什么（源码注释/路由核对） |
|---|---|---|
| `handlers/on_call` | 3 | 值班排班独立领域入口 `/api/v1/on-call/*`；本地**完全没有该域** |
| `handlers/cloudsignal` | 20 | 云信号接入：安全漏洞 + CloudMonitor 告警进入 ITSM 主流程 |
| `handlers/workbench` | 8 | 工作台域 |
| `handlers/account` | 2 | 账号域 |
| `handlers/change_review` | 17 | ⚠️ **不是新能力**：与本地 `handlers/cab` 是同一功能改名（见 §8.2） |

### 1.2 新 service 层实现（非测试文件）

```
service/cloud/                     29 个新文件（多云发现服务；本地只有 handlers/cloud 3 文件）
service/marketplace/               13（商品目录/版本发布回滚/依赖闸门）
service/bpmn_compensation.go       BPMN 补偿
service/bpmn_pending_inventory.go  BPMN 待办清点
service/bpmn_template_digest.go    内置模板版本感知升级摘要
service/ai_quota.go                租户 token 配额与成本口径
service/sla_breach_predictor.go    SLA 违约预测器
service/service_quality_report_service.go  管理者统一服务质量视图（单请求聚合 + metricVersion）
service/system_stats.go            系统统计
service/tool_executor.go           工具执行器（配 tool_registry schema 契约）
service/provisioning_delivery.go   开通交付链
service/escalation_matrix_store.go / dashboard_config_store.go   持久化存储
service/approval_chain_shadow.go   审批链影子执行
```

另有 `connector/builtin/` 新增 7 个内置连接器、`pkg/seeder/` 新增 28 个种子文件。

### 1.3 新增运维/验证命令（`itsm-backend/cmd/`）

`ai-eval`、`aliyun-oss-api-verify`、`aliyun-sdk-api-verify`、`aliyun-security-api-verify`、`service-request-maintenance`

## 2. 新增 CLI / Agent / MCP

- `itsm-cli/src/agent` **41** 个新源文件 + `itsm-cli/src/mcp` 6 个（零依赖 MCP Server，`ff97aa0ad`）；
- ⚠️ 上游把 **`itsm-cli/dist` 的 144 个构建产物提交进了 git**（`git ls-tree` 实测）——
  移植其 CLI 能力时必须排除 `dist/**`，否则等于把别人的构建结果当源码吸收。
- 注：`itsm-cli/` 在 merge-base 就已存在，属**双方各自演进**的共享组件，不是上游新建。

## 3. 新增数据库迁移（**共 46 个 `.sql`**，其中 45 个在 `itsm-backend/migrations/`；下表为主题代表，非穷举）

| 主题 | 文件 |
|---|---|
| SSO | `20261014_create_sso_tables.sql` |
| 问题调查 | `20261018_create_problem_investigation_tables.sql`(+down) |
| 知识评审 | `20261015_knowledge_review_status.sql` |
| **租户内唯一键基线** | `20260923_tenant_scope_baseline_unique_keys.sql`、`20261006_tenant_scope_user_username_unique.sql`、`20261013_tenant_scope_ticket_number_unique.sql`(+down)、`20261011_add_menus_tenant_path_unique.sql` |
| 遗留退役 | `20261008_retire_legacy_roles.sql`（`feat(role)!`）、`20260925_retire_legacy_sla_definitions.sql`、`20260920_drop_legacy_workflow_tables.sql`、`20260921_drop_cab_members_table.sql`、`20261009_drop_orphan_ai_analysis_result.sql` |
| 其它 | `20261011_add_servicerequestapproval_version.sql`、`20261018_fix_bpmn_approval_result_variable.sql`、`20260928_change_affected_cis_backfill.sql` |

## 4. 新增前端页面域（Next.js 路由组）

`admin/{capabilities, connectors, escalation-matrices, escalation-rules, marketplace, menus, operations, service-catalogs, skills, sla-definitions, sla-templates, tags, teams, tickets, workflows, components}`、`cmdb/cloud-signals`
→ **但形态不可移植**：上游仍是 Next.js App Router（`next build`，`src/app/**` 新增 56 文件），
本地已迁 Vite（`tsc -b && vite build`，`src/pages` + `routes/` + `layouts/`）。
只能作为**行为规格**参照，不能取文件。

## 5. 新增依赖

- Go：`aliyun-oss-go-sdk v3.0.2`、`xuri/excelize/v2`（Excel 导出）、`prometheus/client_model`、`golang.org/x/crypto`；间接引入 `urfave/cli/v2`（CLI 框架）
- npm：`dompurify`（富文本消毒）、`axios`、`nanoid`、`sharp`；版本升至 `1.6.19`

## 6. 新增 CI 与测试资产

- `.github/workflows/fresh-install-gate.yml`、`rls-golden-path.yml`
- **382 个新增后端 `*_test.go`**（占其新增文件的 40%）——这是上游最成体系的资产：
  BPMN 生命周期/补偿/模板升级、provisioning 恢复与永久错误、RLS/租户作用域、rag/ai 配额、软删与自助资料等回归护栏。

## 7. 新增文档

`docs/product` 6、`docs/architecture` 6、`docs/operations` 4、`docs/delivery` 4、`docs/testing` 2、`docs/review` 2、`docs/tools` 1

---

## 8. 三处**必须先裁决**的不兼容（不是"要不要移植"，是"两套事实不能并存"）

### 8.1 `users.username` 唯一性 —— 这一条我们偏离了仓库基线

| | 本地 | 上游 |
|---|---|---|
| schema | `ent/schema/user.go:21-23` `field.String("username").Unique()` → **全局唯一** | 放宽为 **(tenant_id, username) 组合唯一** |
| 绕行方案 | `pkg/bootstrap/token.go:79`、`admin.go:100`：账号策略 `admin-<tenantCode>`（登记为 `07:G2` 关闭方式） | 无需绕行 |
| 依据 | 无（历史沿用） | **`AGENTS.md:245`：「业务唯一键默认按 tenant 组合唯一」** |

上游迁移头注释还点明了症状：「第二个租户创建 admin 必然撞全局唯一键，租户开通后该租户永远没有管理员」，
并刻意保留 `email` 全局唯一（找回密码按 email 单独定位，见 `handlers/auth/service.go`）。

**裁决影响面**：一旦采用上游口径，本地 `07:G2` 的关闭方式（命名绕行）需要重述，
`cmd/initialize`、`cmd/provision_tenant`、`cmd/backfill_provider_admin`、bootstrap 首管策略与相关回归全部要复核。
**这是全部移植工作的前置**，因为唯一键语义决定后续任何迁移能否与本地库共存。

### 8.2 `cab` vs `change_review` —— 同一能力的两套命名与两张表

实测对照（双方路由文件逐行比对）：

| 端点 | 本地 `router/cab_routes.go` | 上游 `router/change_review_routes.go` |
|---|---|---|
| `GET /members` | `RequirePermission("change","read")` → `ListCABMembers` | 同权限 → `ListMembers` |
| `POST /members` | `change:write` → `AddCABMember` | 同权限 → `AddMember` |
| `PUT /members/:id` | `change:write` → `UpdateCABMember` | 同权限 → `UpdateMember` |
| `DELETE /members/:id` | `change:write` → `RemoveCABMember` | 同权限 → `RemoveMember` |

- 表：本地 `ent/schema/cab_member.go`；上游 `ent/schema/change_review_member.go`，
  并有 `20260921_drop_cab_members_table.sql`（注释：「CAB 已重命名为 change_review，评审审批流转由审批链引擎驱动」）。
- 两边 handler 注释语义几乎一致（都是「仅名册管理；审批流转由审批链引擎驱动」）。

→ **移植上游 `change_review` 等于制造第二套名册表 + 第二组同名端点**。必须先决定统一命名（含前端入口、权限码、数据迁移方向）。

### 8.3 角色模型 —— 上游 `feat(role)!` 与本地已收口的三处边界冲突

`20261008_retire_legacy_roles.sql` + 59 文件重构（角色→权限包、退役 17 个遗留角色、迁移存量数据），
与本地 ① 治理域收口 D7（`tenant:*` 平台专属，`internal/authz/roles.go:247-249,521-523`）
② G11 组合角色 `provider_tenant_admin` 四处同源 ③ `middleware.RoleRank` / 菜单提权名单 / `pkg/seeder` 词表
**同时冲突**。列为硬排除，除非上游能提供其角色表与本词表的映射。

## 9. 反过来：本地独有、上游没有的能力（防止单向"上游更多"）

`handlers/` 目录级实测差集：**`attachment`、`cab`、`cloud`、`dashboard`、`department`、`invitation`、`mcp`、`pat`、`root_cause`** 共 9 个域上游不存在。
即本地在附件、个人访问令牌、根因、部门、邀请、MCP 接入等方向是**上游的上游**。

## 10. 建议处理顺序

| 级别 | 动作 |
|---|---|
| **P0 裁决** | §8.1 username 唯一性口径（决定所有迁移能否并存）；§8.2 CAB/change_review 统一命名 |
| **P1 移植候选**（新增、低耦合） | `on_call` 域（本地空白）；`cmd/` 五个验证与维护命令；`handlers/cloudsignal` + `service/cloud` 差集；`ai_quota` / `sla_breach_predictor` / `service_quality_report` 先做差集再定 |
| **P2 资产吸收** | 382 个新增测试里挑**不依赖其私有实现**的契约/回归用例（RLS 作用域、软删、租户唯一键），作为我们侧的守护测试移植 |
| **观察** | `marketplace`、`workbench`、`account`（与本地既有实现重叠面未测绘） |
| **拒绝** | `feat(role)!`、`itsm-cli/dist/**`、Next.js 侧 `src/app/**`、`CHANGELOG/UPGRADE/api-reference` 整文件替换 |

## 11. 复现命令

```bash
git fetch --no-tags heidsoft main
MB=e7fd79e951aa1d5f1e10a3810c318433fe3fb845

# 新增文件与域级差集
git diff --diff-filter=A --name-only $MB heidsoft/main | wc -l            # 955
comm -13 <(git ls-tree --name-only HEAD:itsm-backend/handlers | sort) \
         <(git ls-tree --name-only heidsoft/main:itsm-backend/handlers | sort)   # 上游独有 handler 域

# 不变量冲突取证
grep -n -A2 'field.String("username")' itsm-backend/ent/schema/user.go
git show heidsoft/main:itsm-backend/migrations/20261006_tenant_scope_user_username_unique.sql | head
grep -n '业务唯一键' AGENTS.md                                            # :245
git show heidsoft/main:itsm-backend/handlers/change_review/handler.go | head
head -8 itsm-backend/handlers/cab/handler.go
```

## 12. 文件信息

| 项 | 值 |
|---|---|
| 创建日期 | 2026-11-06 |
| 上游快照 | `heidsoft/main = 63b9feb5`（其 `main` 最新提交日期 2026-11-05） |
| 结论性质 | 只读清单；未合并、未改动任何代码；试合并已在独立 worktree `--abort` |
| 关联 | 姊妹篇 [`2026-11-06-upstream-heidsoft-integration-scan.md`](./2026-11-06-upstream-heidsoft-integration-scan.md)（可移植性分级与 S1–S7 协议） |
| 待办 | §8.1 / §8.2 两项裁决需产品与架构共同确认，建议登记进 `docs/multi-tenant/07-known-gaps.md` 的 G 空间 |
