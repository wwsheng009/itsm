# 多租户文档索引（INDEX）

> 状态：**当前**｜更新日期：2026-09-30（决策全量冻结 + P0 契约冻结）｜定位：`docs/multi-tenant/` 的**唯一入口索引**——文档清单、权威层级、编号速查、阅读路径与维护规则。
> 维护：新增/重命名/停用文档必须同步本索引，并保持 docs-gate **C.6**（多租户一致性门禁）通过。

---

## 0. 30 秒速查（我该看哪份？）

| 问题 | 看这里 |
|---|---|
| 概念/口径/边界有疑问 | [canon](./plan/msp-concept-model-and-architecture-canon.md)（**唯一概念权威**；编号注册表见其附录 C） |
| 跨客户怎么设计（看+做、过滤器、切换） | [工作台方案](./plan/msp-cross-customer-workbench-and-filter-plan.md) + [目标架构 §9](./plan/msp-target-architecture.md) |
| 登录落哪、切换/刷新契约、隐私红线 | [登录与切换细化](./plan/msp-login-and-switching-refinement-plan.md) + [主方案](./plan/msp-user-lifecycle-and-tenant-switching-plan.md) |
| 怎么落地（步骤/验收/回滚） | [实施方案（P0→P1→P2）](./plan/msp-implementation-plan.md) |
| 编号（R/K/G/F/WB/REV/D/E/I/A/IP…）是什么意思 | [canon 附录 C](./plan/msp-concept-model-and-architecture-canon.md#附录-c跨文档一致性登记权威层级--编号注册表)（权威）＋本索引 §4（速查） |
| 文档为什么改过、冲突怎么裁的 | [一致性审计](./plan/msp-docs-consistency-audit.md) |
| 实测现状/缺口/排障 | [01–07](./README.md)（as-is）｜[三角色演练剧本](./plan/msp-three-persona-operation-simulation.md) |
| 想在浏览器一步步重现全链路业务 | [scenarios/ 场景目录](./scenarios/README.md)（建租户→开通→建号→分配→建单→协作→审计与反例，含验证命令） |

---

## 1. 按角色的阅读路径

| 角色 | 推荐顺序 |
|---|---|
| 产品/决策 | [README](./README.md) → canon §1/§2/§7 → 工作台方案 → 登录细化 → 实施方案 §0/§2 |
| 架构评审 | canon（全篇）→ [目标架构](./plan/msp-target-architecture.md) → [集成分析](./plan/msp-integration-with-rbac-org-workflow-analysis.md) → 一致性审计 §1/§2 |
| 后端研发 | 实施方案 §3 → 目标架构 §3–§8 → 集成分析 → canon §6（不变量） |
| 前端研发 | [前端分析](./plan/msp-frontend-pages-and-permissions-analysis.md) → 工作台方案 §2 → [用户交互流程](./plan/msp-user-interaction-flows.md) → 实施方案 `IP-P0-8` |
| 测试/QA | [scenarios/ 场景目录（浏览器实操）](./scenarios/README.md) → [06 验证与排障](./06-verification-and-troubleshooting.md) → 三角色演练剧本 → 实施方案 §6（DoD/反例/映射） |
| 运维/部署 | [02 部署与配置](./02-deployment-and-configuration.md) → [07 已知缺口](./07-known-gaps.md) → `scripts/msp/*` → 06 §7 |

---

## 2. 文档清单（层级 / 状态 / 定位 / 关键编号）

> 层级 L0–L6 见 canon 附录 C.1：L0 选型 → L1 概念 → L2 决策修订源 → L3 目标详细设计 → L4 方案 → L5 现状/运营 → L6 演练。

| 文档 | 层级 | 状态 | 定位 | 关键编号 |
|---|---|---|---|---|
| [INDEX.md](./INDEX.md)（本文件） | 入口 | 当前 | 索引/阅读路径/速查 | — |
| [README.md](./README.md) | 入口 | 当前 | 运营手册入口、角色术语、维护约定 | — |
| [01-architecture.md](./01-architecture.md) | L5 | 现状（2026-09-28） | 场景与总体架构（as-is） | — |
| [02-deployment-and-configuration.md](./02-deployment-and-configuration.md) | L5 | 现状（2026-09-28） | 部署与配置、运维脚本（as-is） | — |
| [03-customer-dimension.md](./03-customer-dimension.md) | L5 | 现状（2026-09-28） | 客户租户生命周期（as-is） | — |
| [04-provider-dimension.md](./04-provider-dimension.md) | L5 | 现状（2026-09-28） | 服务商维度与分配（as-is） | — |
| [05-usage-guide.md](./05-usage-guide.md) | L5 | 现状（2026-09-28） | 使用指南（as-is） | — |
| [06-verification-and-troubleshooting.md](./06-verification-and-troubleshooting.md) | L5 | 现状（2026-09-28） | 验证与排障、验收清单 | — |
| [07-known-gaps.md](./07-known-gaps.md) | L5 | 现状（2026-09-28） | 生产实测缺口（唯一 G 空间） | `G1–G10` |
| [msp-concept-model-and-architecture-canon.md](./plan/msp-concept-model-and-architecture-canon.md) | **L1** | **v1.0（定稿）** | **概念与架构单一权威**：定义/边界/不变量/风险/验收/决策 | `R1–R12` `K1–K5` `D1–D11` `E1–E6` `I1–I13` `A1–A12` `B1–B6` |
| [msp-target-architecture.md](./plan/msp-target-architecture.md) | L3 | Draft v0.3（P0/P1 契约冻结） | 目标态详细设计（membership/上下文/权限/建号/迁移） | — |
| [msp-cross-customer-workbench-and-filter-plan.md](./plan/msp-cross-customer-workbench-and-filter-plan.md) | **L2** | Draft v0.2（P0 契约冻结） | 跨客户工作台 + 过滤器（**决策修订源**） | `WB1–WB6` `WB-R1–6` `WB-A1–6` `REV-1–5` |
| [msp-login-and-switching-refinement-plan.md](./plan/msp-login-and-switching-refinement-plan.md) | **L2** | Draft v0.3（决策确认） | 登录落地/隐私/切换（**决策修订源**） | `LOGIN-*` |
| [msp-user-lifecycle-and-tenant-switching-plan.md](./plan/msp-user-lifecycle-and-tenant-switching-plan.md) | L4 | Draft v0.5（P0/P1 契约冻结） | 生命周期与切换实现计划（主方案） | `F1–F15` |
| [msp-scope-model-analysis-and-ai-gateway-reference.md](./plan/msp-scope-model-analysis-and-ai-gateway-reference.md) | L4 | Draft v0.5（P0/P1 契约冻结） | 作用域模型分析（Q1 回答） | — |
| [msp-user-interaction-flows.md](./plan/msp-user-interaction-flows.md) | L4 | Draft v0.5（口径回填） | 用户交互流程图 | `UF-01–UF-10` |
| [msp-frontend-pages-and-permissions-analysis.md](./plan/msp-frontend-pages-and-permissions-analysis.md) | L4 | Draft v0.2（P0 契约冻结） | 前端页面/权限/菜单现状与改造 | `FE-A1–A8` |
| [msp-integration-with-rbac-org-workflow-analysis.md](./plan/msp-integration-with-rbac-org-workflow-analysis.md) | L4 | Draft v0.4（回填 + P1 同步） | ×RBAC/组织/工作流集成分析与冲突处置 | `INT-D#` |
| [msp-three-persona-operation-simulation.md](./plan/msp-three-persona-operation-simulation.md) | L6 | Draft v0.3（复核） | 三角色操作演练剧本（验收载体） | — |
| [msp-multi-tenant-business-acceptance-design.md](./plan/msp-multi-tenant-business-acceptance-design.md) | L6 | v1.1（2026-10-03） | **多租户业务验收设计**（G0–G7 场景目录 + 判据 + 证据）：平台建租户/服务商接单/客户闭环/隔离反例/审计；脚本 `scripts/msp/acceptance/run-msp-business-acceptance.ps1` | `S1–S4` `P1–P10` `C1–C6` `I1–I6` `A1–A3` |
| [msp-docs-consistency-audit.md](./plan/msp-docs-consistency-audit.md) | 治理 | v0.3（C1–C19 全闭环） | 一致性审计（权威层级/冲突 C1–C19/整改） | `C1–C19` |
| [msp-implementation-plan.md](./plan/msp-implementation-plan.md) | 落地 | **v1.1（P0/P1 契约冻结）** | **实施方案 P0→P1→P2**（步骤/DoD/回滚） | `IP-P0-#` `IP-P1-#` `IP-P2-#` |
| [msp-account-provisioning-and-registration-flow.md](./plan/msp-account-provisioning-and-registration-flow.md) | L4 | Draft v0.3（决策同步） | **建号与注册流程**（四通道/邀请/首登/过渡期） | — |
| [msp-business-closure-review-and-refactor-plan.md](./plan/msp-business-closure-review-and-refactor-plan.md) | 治理/落地 | Draft v0.4（决策同步） | **业务闭环审查 + 现有功能改造计划**（13 链路/波次/深度分级） | `CL-01–CL-13` |
| [msp-tenant-user-management-enhancement-plan.md](./plan/msp-tenant-user-management-enhancement-plan.md) | L4 | v0.3（后端落地） | **平台侧租户用户管理增强**（跨租户用户列表/重置密码/启停/强制下线/审计；含操作便利性与业务闭环设计） | `TUM-#` |

### 2.1 场景目录（scenarios/，L6 浏览器实操剧本）

> 从**操作者视角**的浏览器逐步剧本：每一步含界面反馈预期与独立验证命令；2026-10-05 全链路实测通过（详见 [README](./scenarios/README.md) §5 证据基线）。

| 文档 | 状态 | 定位 |
|---|---|---|
| [scenarios/README.md](./scenarios/README.md) | 当前（2026-10-05） | 场景总览/角色与账号矩阵/证据基线/已知限制/维护约定 |
| [00-environment-and-accounts.md](./scenarios/00-environment-and-accounts.md) | 当前 | 环境准备、账号矩阵、通用验证手法、重置与排错 |
| [01-platform-create-provider-tenant.md](./scenarios/01-platform-create-provider-tenant.md) | 当前 | 场景 1：平台新建服务商租户 |
| [02-platform-create-customer-tenant.md](./scenarios/02-platform-create-customer-tenant.md) | 当前 | 场景 2：平台新建客户租户并绑定服务商 |
| [03-tenant-provisioning-first-admin.md](./scenarios/03-tenant-provisioning-first-admin.md) | 当前 | 场景 3：开通向导（模板供给/首管/首登改密） |
| [04-user-provisioning.md](./scenarios/04-user-provisioning.md) | 当前 | 场景 4：建号三通道 + 邀请扩展 |
| [05-msp-allocation-management.md](./scenarios/05-msp-allocation-management.md) | 当前 | 场景 5：分配管理与跨客户授权 |
| [06-customer-ticket-type-and-ticket.md](./scenarios/06-customer-ticket-type-and-ticket.md) | 当前 | 场景 6：客户建类型与工单 |
| [07-msp-workbench-collaboration.md](./scenarios/07-msp-workbench-collaboration.md) | 当前 | 场景 7：工作台协作（回复/状态/指派/批量） |
| [08-audit-and-isolation-verification.md](./scenarios/08-audit-and-isolation-verification.md) | 当前 | 场景 8：审计看板 + 隔离负向验证 |

**上位与关联（本目录之外）**：

| 文档 | 关系 |
|---|---|
| [ADR-004 多客户管理场景租户模型选型](../architecture/adr-004-multi-customer-tenant-model-selection.md) | **L0 选型（Accepted 2026-09-30）**；行动项引用写作 `ADR-004:A#`，执行跟踪见实施方案 |
| [ADR-003 CMDB 模型](../architecture/)（按需查阅） | 03 文档引用的 CMDB 租户内模型决策 |
| [通知模块设计方案](../plan/notification-module-design-plan-2026-09-29.md) | 主方案/目标架构引用的 Q4 落地设计（通知/邮件通道） |
| `scripts/msp/setup-msp-tenants.sh` / `build-provision-tenant.sh` | 一键初始化与构建（8 阶段幂等 + 隔离探针） |
| `scripts/docs-gate/check-multi-tenant-consistency.sh`（C.6） | 本目录一致性自动门禁 |

---

## 3. 权威层级与冲突裁决（摘要）

| 层级 | 文档 | 权威范围 |
|---|---|---|
| L0 | ADR-004 | 选型结论与行动项 |
| L1 | **canon** | 概念、边界 B1–B6、不变量 I1–I13、术语、决策 D/E |
| L2 | 工作台方案、登录细化 | 对"跨客户操作/登录落地"的最新修订（**对被修订文档有约束力**） |
| L3 | 目标架构 | membership/上下文/权限矩阵/迁移的详细设计 |
| L4 | 主方案、scope-model、user-flows、frontend、集成分析 | 领域实现计划（局部编号，引用加前缀） |
| L5 | 01–07 | 实测现状与运维手册（**不得作为目标口径**） |
| L6 | 三角色演练剧本、[scenarios/ 场景目录](./scenarios/README.md) | 操作剧本（验收载体）：前者为设计期演练，后者为**浏览器逐步实操**（可重现/可验证） |

**裁决规则**：概念冲突 → canon 胜；实现/交互冲突 → **最新修订源（L2）胜**，但必须回填被修订文档；现状冲突 → 01–07 + 代码为准，canon 必须"现状/目标"分列；引用编号必须带前缀；文档状态行与修订记录一致。

---

## 4. 编号注册表速查（权威版 = canon 附录 C）

| 编号 | 归属 | 含义 |
|---|---|---|
| `R1–R12` | canon | 架构/安全风险 |
| `K1–K5` | canon §7.3 | provider 能力供给缺口（原 G1–G5） |
| `G1–G10` | 07-known-gaps | 生产实测缺口（**唯一 G 空间**，引用写作 `07:G4`） |
| `F1–F15` | 主方案 | 生命周期/切换发现项 |
| `UF-01–UF-10` | user-flows | 交互流程号（原 F-01…F-10） |
| `WB1–WB6` / `WB-R1–6` / `WB-A1–6` | 工作台方案 | 规则 / 风险 / 验收（原 G1–G6、R1–R6、A1–A6） |
| `REV-1–REV-5` | 工作台方案 | 对既有文档的修订项（原 R7–R11） |
| `D1–D11` | canon §10 | 开放决策（D10/D11 已定稿） |
| `E1–E6` | canon §7.2 | 工单流转决策 |
| `I1–I13` | canon §6 | 不变量 |
| `A1–A12` | canon §9 | 验收标准（canon 内裸 `A#` 仅此含义） |
| `ADR-004:A1–A11` | ADR-004 | 行动项（**必须带前缀**） |
| `LOGIN-A#/R#/D#/B#/F#` | 登录细化 | 文档局部编号（必须带前缀） |
| `FE-A#` | 前端分析 | 文档局部验收（必须带前缀） |
| `IP-P0-#/P1-#/P2-#` | 实施方案 | 阶段工作流编号（必须带前缀） |
| `CL-01–CL-13` | 闭环审查与改造计划 | 端到端业务链路编号（必须带前缀） |
| `C1–C19` | 一致性审计 | 冲突清单编号 |

**字母消歧（禁止裸用）**：**实现路线 A/B**（P0 最小闭环 / P1 membership 转正）｜**部署选项 A/B**（单 provider 预设 / 多 provider 模型）｜**拆分方案 A/B**（平台-服务商同体 / 拆分平台租户）。

---

## 5. 状态与版本速查

| 文档 | 版本/状态 | 修订记录位置 |
|---|---|---|
| canon | **v1.0（定稿，2026-09-30）** | 文末 |
| 工作台方案 | v0.2（P0 契约冻结） | 文末 |
| 登录细化 | v0.3（决策确认） | 文末 |
| 目标架构 | v0.3（P0/P1 契约冻结） | 文末 |
| 主方案 | v0.5（P0/P1 契约冻结） | 文末 |
| user-flows | v0.5 | 文末 |
| scope-model | v0.5（P1 契约冻结） | 文末 |
| 前端分析 | v0.2（P0 契约冻结） | 文末 |
| 集成分析 | v0.4（回填 + P1 同步） | 文末 |
| 建号流程 | v0.3（决策同步） | 文末 |
| 三角色剧本 | v0.3（复核） | 文末 |
| scenarios/（场景目录） | 当前（2026-10-05 实测） | 各场景头部日期 |
| 一致性审计 | v0.3（C1–C19 全闭环） | 文末 |
| 实施方案 / 闭环审查 | v1.1 / v0.4 | 文末 |
| ADR-004 | Accepted（2026-09-30） | 状态段 |
| README / INDEX | 当前（2026-09-30） | 头部日期 |
| 01–07 | 现状（2026-09-28） | 头部日期 |

> 版本规则：**状态行与修订记录必须一致**；批次交付后更新日期并追加修订记录（docs-gate C.6 强制头部四件套）。

---

## 6. 维护规则（本索引与目录）

1. **新增/重命名/停用文档**：同步更新本索引 §2 与 [README](./README.md) 导航；头部必须含四件套（状态/日期/定位/关联）；
2. **新增编号/术语**：先登记 canon 附录 C，再在本索引 §4 加一行；
3. **修订回填**：任何文档修订既有结论 → 同 PR 回填被修订文档并留指针（权威层级 §3）；
4. **门禁**：提交前 `make docs-gate`（C.1–C.6）必须 **6/6** 通过；
5. **索引优先**：本索引是目录唯一入口；README 面向"运营手册"读者，本索引面向"找文档"读者。

---

## 7. 目录地图

```text
docs/multi-tenant/
├── INDEX.md                    ← 本文件（唯一入口索引）
├── README.md                   （运营手册入口 + 维护约定）
├── 01-architecture.md          01–07：现状/运营（as-is，2026-09-28）
├── 02-deployment-and-configuration.md
├── 03-customer-dimension.md
├── 04-provider-dimension.md
├── 05-usage-guide.md
├── 06-verification-and-troubleshooting.md
├── 07-known-gaps.md
├── scenarios/                  L6 浏览器实操剧本（场景 0–8）
│   ├── README.md
│   ├── 00-environment-and-accounts.md
│   ├── 01-platform-create-provider-tenant.md
│   ├── 02-platform-create-customer-tenant.md
│   ├── 03-tenant-provisioning-first-admin.md
│   ├── 04-user-provisioning.md
│   ├── 05-msp-allocation-management.md
│   ├── 06-customer-ticket-type-and-ticket.md
│   ├── 07-msp-workbench-collaboration.md
│   └── 08-audit-and-isolation-verification.md
└── plan/
    ├── msp-concept-model-and-architecture-canon.md      L1 概念权威（附录 C 注册表）
    ├── msp-cross-customer-workbench-and-filter-plan.md  L2 修订源
    ├── msp-login-and-switching-refinement-plan.md       L2 修订源
    ├── msp-target-architecture.md                       L3 目标详细设计
    ├── msp-user-lifecycle-and-tenant-switching-plan.md  L4 主方案
    ├── msp-scope-model-analysis-and-ai-gateway-reference.md
    ├── msp-user-interaction-flows.md
    ├── msp-frontend-pages-and-permissions-analysis.md
    ├── msp-integration-with-rbac-org-workflow-analysis.md
    ├── msp-three-persona-operation-simulation.md        L6 演练剧本
    ├── msp-docs-consistency-audit.md                    治理：一致性审计
    ├── msp-implementation-plan.md                       落地：实施方案
    ├── msp-account-provisioning-and-registration-flow.md 流程：建号与注册
    └── msp-business-closure-review-and-refactor-plan.md  治理：闭环审查与改造计划
```
