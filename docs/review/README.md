# Review 目录说明

> Status: historical collection。

本目录中的评审报告是特定日期的代码审查快照，不是当前架构或发布事实源。报告里的问题可能已经修复，标记“已完成”的能力也可能在后续改动中回归。

进行新开发前，请先阅读 [`docs/documentation-governance.md`](../documentation-governance.md)，再以当前源码、测试和运行时证据验证报告结论。仍有效的架构规则应迁移到 `docs/architecture/` 或 `docs/product/`，仍有效的工作项应进入根目录 roadmap 或 issue。

## 迁移状态（v1.5）

已迁移的评审结论：

- BPMN 表达式拒绝策略、变量原子化、ServiceTask Handler、CMDB 跨租户隔离 / 拓扑深度 → [`docs/architecture/workflow-cmdb-invariants.md`](../architecture/workflow-cmdb-invariants.md)
- 主流程闭环 / 契约治理 / AI 默认门禁 / 测试夹具唯一性 → [`docs/architecture/domain-ownership.md`](../architecture/domain-ownership.md) §跨领域不变量
- 测试优先级 / 夹具不变量 / Jest 退出异常 / API smoke 17 项 → [`docs/testing/test-invariants.md`](../testing/test-invariants.md)
- 前端 UX 不变量（error.tsx / loading.tsx / 默认凭据 / ErrorBoundary 跳转） → [`docs/testing/static-analysis-gates.md`](../testing/static-analysis-gates.md) §5.6–5.9

## 已迁移文件

- `servicenow-benchmark-2026-06-18.md` → [`docs/archive/reviews/servicenow-benchmark-2026-06-18.md`](../archive/reviews/servicenow-benchmark-2026-06-18.md)
  - 原因：报告中的 v1.1 / v1.2 / v1.4 / v2.0 / v3.0 路线已完全被 [`ROADMAP.md`](../../ROADMAP.md) 取代；ServiceNow GAP 列表已迁移到 [`docs/product/`](../product/) 阶段改进计划。

## 仍保留的报告

| 报告 | 状态 | 保留理由 |
|---|---|---|
| `architecture-review-2026-06-14.md` | historical | BPMN / CMDB 不变量主要迁移来源；保留作历史 |
| `frontend-ux-review-2026-06-19.md` | historical | §5.6–5.9 门禁主要迁移来源 |
| `module-function-retrospective-2026-07-10.md` | historical | 主流程闭环 / 契约治理迁移来源 |
| `system-function-review-result-2026-07-01.md` | historical | F-1..F-9 测试夹具修复、GA readiness 12 modules 基线 |
| `system-function-review-checklist-2026-07-01.md` | historical | checklist 原件 |
| `commercial-readiness-acceptance-report-2026-06-18.md` | historical | 商用验收快照 |
| `browser-e2e-test-report-2026-06-18.md` | historical | 浏览器 E2E 烟测基线 |
| `browser-functional-test-report-2026-06-20.md` | historical | 浏览器功能测试 |
| `deep-business-test-report-2026-06-18.md` | historical | 深度业务流测试（默认账号过时，但仍含 API 验证记录） |
| `product-architecture-review-2026-09-03.md` | historical | 产品架构评审快照；2026-09-12 由 `docs/reviews/`（复数目录）合并入本目录 |
| `workflow-cmdb-review-2026-09-03.md` | historical | 工作流/CMDB 评审；被 [`docs/architecture/workflow-cmdb-invariants.md`](../architecture/workflow-cmdb-invariants.md) §回归 引用，移动时已同步更新该引用 |
| `2026-10-28-multi-tenant-functional-review.md` | historical | 多租户功能审查（13 项问题）；`plan.md` 修复计划的输入，移动时已同步更新该引用 |
| `2026-10-28-page-review-round2.md` | historical | 系统管理后台页面实测审查第二轮（46 项问题） |
| `2026-10-28-page-review-round3.md` | historical | 系统管理后台页面实测审查第三轮（31 项问题） |
| `2026-10-28-page-review-round4.md` | historical | 工作流 / SLA / 升级域实测审查第四轮（10 页，58 项问题） |
| `2026-10-28-page-review-round5.md` | historical | 资源中心 / Bot / 工具目录 / CMDB 域实测审查第五轮（7 页，39 项问题） |
| `2026-10-28-page-review-round6.md` | historical | 权限域 / 配置域实测审查第六轮（12 页 + 4 页签，101 项问题） |
| `2026-10-28-page-review-round7.md` | historical | 表单域 / AI 域 / 业务域 / CMDB 域实测审查第七轮（20 页，87 项问题） |
| `2026-10-28-page-review-round8.md` | historical | 多租户前端实测审查第八轮——`plan.md` 修复验收 14 项（确认生效 5 / 未生效 2 / 页面矛盾 1 / 未验证 6）+ 新缺陷 14 项（P1×4、P2×10） |
| `2026-11-06-tenant-user-role-adjustment-review.md` | **v0.2 复核修订（2026-11-06）** | 租户用户「角色调整」能力评审；v0.1 判为业务缺口，v0.2 按源码改判为**边界确认（TUM-7 维持不做）+ 三项真实缺陷**；衍生 G12–G15 已登记 [`docs/multi-tenant/07-known-gaps.md`](../multi-tenant/07-known-gaps.md)（G12 错误码漂移、G13 护栏按当页估算已修复；G14 membership 撤销面待接线决策、G15 职责边界口径） |
| `2026-11-06-upstream-heidsoft-integration-scan.md` | 扫描结论（未合并） | 上游 fork `heidsoft/itsm`（238 部署机 origin）能力扫描：merge-base `e7fd79e9`、双向独有 636/537 提交、patch-id 等价 0、试合并 **439 冲突 / 2883 自动合并**；根因是**前端架构级分叉（上游 Next.js vs 本地 Vite）**；产出「硬排除项（含 `feat(role)!` BREAKING）+ 17 簇可移植性分级 + 移植顺序 + S1–S7 审查→调整→移植协议」 |
| `2026-11-06-upstream-heidsoft-feature-inventory.md` | 只读清单（未合并） | 上游**到底加了什么**：955 新增文件逐项归类（新 handler 域 on_call/cloudsignal/workbench/account、`service/cloud` 29 文件、5 个 `cmd/` 验证命令、46 个新增 SQL 迁移、382 个新回归测试、`itsm-cli/dist` 144 产物入 VCS）；并给出**三处必须先裁决的不兼容**——`users.username` 全局唯一 vs 上游租户内组合唯一（上游依据正是 `AGENTS.md:245`，我们才是偏离基线的一方）、`cab` vs `change_review` 同端点同权限两套表、`feat(role)!` 与 D7/G11 冲突；另列本地独有 9 个 handler 域，防单向结论 |
| `2026-11-10-msp-demo-e2e-validation.md` | historical | 2026-11-10/11 238 demo 多轮多用户 E2E 快照：权限矩阵、跨单据关联、MSP 工作台、多租户深度处理、附件访问、UI/UX；已被 `2026-11-12-tenant-recipient-validation.md` 的当前验证取代 |
| `2026-11-11-role-whitelist-audit.md` | **current** | 硬编码角色白名单专项审计：修复 Header AI `isAdmin()` 旁路、AI 通知/审批 fallback 无效角色码、工单数据范围/MSP 写权限重复白名单、provider allowedActions 行级误判；内部备注限制复核保留；CC 管理、旧前端角色守卫、平台租户页 `super_admin` 条件列为待产品决策 |
| `2026-11-11-membership-actor-reference-phase3.md` | **current** | C 方案 Phase 3 发布验证：评论/附件/CC/审批/通知/审计/BPMN 决策的 membership 引用、回填迁移、复合外键 post-schema 重建；238 `bf131540` 六容器 healthy、迁移台账/15 外键/租户错配 0、E2E 45/45（含评论/CC/附件 membership 断言） |
| `2026-11-12-tenant-recipient-validation.md` | **current** | C 方案 Phase 4 第一批：CC / BPMN CC / 审批委派 / BPMN 任务 actor-target 的跨租户 membership 校验；provider 客户租户身份放行、无 membership 外部账号 fail-closed；CC 路由权限与 RBAC 预检同源修复；238 `5639c21c9` 46/46 E2E |
| `2026-11-12-membership-business-actors.md` | **current** | C 方案 Phase 4 第二批：问题/变更/服务请求创建人与处理人 membership 双写与回填；21 条 membership 复合外键清理脏引用后 VALIDATE；post-schema 约束自愈；跨租户读写/伪造引用矩阵与真实 PostgreSQL 约束巡检；238 `47bd56dc5` 发布，E2E 46/46，发现并修复事件转问题缺 creator membership |
| `evidence/2026-11-11-demo-full-deploy-44faf311.md` | historical | 2026-11-11 第一轮全量部署 8089 的证据（`44faf311`，40/40 E2E）；已被后续 `af3cba9c` 取代 |
| `evidence/2026-11-11-demo-full-deploy-5699b5a8.md` | historical | 2026-11-11 第二轮全量部署 8089 的证据（`5699b5a8`，43/43 E2E）；已被 `af3cba9c` 取代 |
| `evidence/2026-11-11-demo-full-deploy-af3cba9c.md` | historical | 2026-11-11 第三轮全量部署 8089 的证据（`af3cba9c`）：44/44 E2E、D18 附件访问修复；已被 `a0d4240a` / `91eb6e82` 取代 |
| `evidence/2026-11-11-demo-full-deploy-a0d4240a.md` | historical | 2026-11-11 第四轮全量部署 8089 的证据（`a0d4240a`）：角色白名单修复、44/44 分组复跑；已被 `91eb6e82` 取代 |
| `evidence/2026-11-11-demo-full-deploy-91eb6e82.md` | historical | 2026-11-11 角色枚举与 provider allowedActions 修复部署（`91eb6e82`，44/44）；已被后续 C 方案发布取代 |
| `evidence/2026-11-11-demo-full-deploy-49a2ad11.md` | historical | 2026-11-11 C 方案主体与 `jwt_tenant_source` 修复发布（`49a2ad11`）：membership 上下文/工单 membership 回填/provider 代客建单；已被 `ad878805` 取代 |
| `evidence/2026-11-11-demo-full-deploy-ad878805.md` | historical | 2026-11-11 C 方案发布（`ad878805`）：membership 身份、45/45 E2E、变更重复删除 404；已被 `e1599ffd` 取代 |
| `evidence/2026-11-11-demo-full-deploy-e1599ffd.md` | historical | 2026-11-11 C 方案 Phase 2 发布（`e1599ffd`）：工单分配 `assignee_membership_id` 同步、45/45 E2E；已被 `bf131540` 取代 |
| `evidence/2026-11-11-demo-full-deploy-bf131540.md` | historical | 2026-11-11 Phase 3 全量部署证据（`bf131540`）：45/45 E2E；已被 `5639c21c9` 取代 |
| `evidence/2026-11-12-demo-full-deploy-5639c21c9.md` | **evidence** | 2026-11-12 当前全量部署证据（`5639c21c9`）：接收人租户校验、RBAC 预检同源、B4b E2E、46/46 分组 E2E、容器健康与备份回滚路径 |
| `evidence/2026-11-12-demo-full-deploy-47bd56dc5.md` | **evidence** | 2026-11-12 Phase 4 第二批最终部署证据（`47bd56dc5`）：21/21 约束 VALIDATED、6 类业务引用 missing=0、46/46 E2E；含事件转问题 membership 补漏与 reconcile 迁移 |

## 目录合并（2026-09-12）

仓库曾并存 `docs/review/`（单数，本目录，有治理登记）与 `docs/reviews/`（复数，无 README、治理体系外）两个评审目录。为消除双入口，后者 2 份报告已通过 `git mv` 合并入本目录，`docs/reviews/` 空目录已移除，唯一外部引用（`workflow-cmdb-invariants.md`）已同步更新路径。

**新评审报告一律放入本目录并登记上表**，不要再新建 `docs/reviews/`。

