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

## 目录合并（2026-09-12）

仓库曾并存 `docs/review/`（单数，本目录，有治理登记）与 `docs/reviews/`（复数，无 README、治理体系外）两个评审目录。为消除双入口，后者 2 份报告已通过 `git mv` 合并入本目录，`docs/reviews/` 空目录已移除，唯一外部引用（`workflow-cmdb-invariants.md`）已同步更新路径。

**新评审报告一律放入本目录并登记上表**，不要再新建 `docs/reviews/`。

