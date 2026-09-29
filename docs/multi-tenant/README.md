# 多租户 · 多客户单一服务商（MSP）运营手册

> **状态**：当前
> **更新日期**：2026-09-29
> **选型依据**：[ADR-004：多客户管理场景租户模型选型](../architecture/adr-004-multi-customer-tenant-model-selection.md)
> **概念/目标口径权威**：[概念模型与架构总纲（Canon）](./plan/msp-concept-model-and-architecture-canon.md)（附录 C：权威层级 + 编号注册表）｜**一致性审计**：[msp-docs-consistency-audit.md](./plan/msp-docs-consistency-audit.md)

## 目标读者与适用场景

本目录面向"**一家服务商（MSP）同时服务多个客户，且客户需要登录系统自行处理业务**"的部署与运营场景，读者包括：

- 平台架构师：评估租户模型、隔离边界与扩展点；
- 部署/运维工程师：完成 `saas_msp` 模式部署、配置与验证；
- 服务商管理员：开通客户租户、分配工程师、日常跨客户运营；
- 客户方管理员：在自有租户内管理用户、角色与业务数据。

不适用于：单企业内部使用（`private` 模式，见 `docs/install.md`）、纯 SaaS 自助注册（`saas` 模式）。

## 核心模型（一句话）

**服务商 = 一个 `msp_provider` 租户；每个客户 = 一个 `msp_customer` 租户（归属**唯一** provider，1:1）；服务商员工通过 `MSPAllocation` 被授权到多个客户租户；日常跨客户操作用"工作台 + 客户过滤器 + 条目级操作"，`X-Customer-Tenant-ID` 为单请求只读通道，会话切换仅用于深度操作。**

## 文档导航

| 文档 | 内容 | 主要读者 |
|---|---|---|
| [01-architecture.md](./01-architecture.md) | 场景与总体架构：租户模型、隔离四层、MSP 授权模型、跨租户数据流、设计约束 | 架构师 / 研发 |
| [02-deployment-and-configuration.md](./02-deployment-and-configuration.md) | 部署与配置：`saas_msp` 模式、环境变量、RLS 灰度、tenant guard、验证清单 | 运维 / 部署 |
| [03-customer-dimension.md](./03-customer-dimension.md) | 客户维度：客户租户生命周期、字段配置、客户用户与 RBAC、登录解析、退租 | 服务商管理员 / 客户管理员 |
| [04-provider-dimension.md](./04-provider-dimension.md) | 服务商维度：provider 租户、`msp_role`、分配与回收、跨客户操作与权限矩阵 | 服务商管理员 |
| [05-usage-guide.md](./05-usage-guide.md) | 使用指南：客户端与 MSP 端操作路径（控制台/接口/CLI）与典型流程 | 全体使用者 |
| [06-verification-and-troubleshooting.md](./06-verification-and-troubleshooting.md) | 验证与排障：验收清单、常见问题、错误码与定位方法 | 运维 / 测试 |
| [07-known-gaps.md](./07-known-gaps.md) | 已确认产品缺口：实测现象、运维规避、建议修复与优先级 | 产品 / 研发 / 运维 |

> **说明**：01–07 为**现状/运营（as-is）**文档（更新于 2026-09-28，描述实测行为）；**目标口径一律以 [canon](./plan/msp-concept-model-and-architecture-canon.md) 为准**。两者已知差异与整改记录见[一致性审计](./plan/msp-docs-consistency-audit.md) §3。

## 角色与术语

| 术语 | 含义 |
|---|---|
| 租户（Tenant） | 最高级数据归属与隔离边界；`ent/schema/tenant.go` |
| 服务商租户 | `type = msp_provider`，承载服务商员工与其 MSP 管理面 |
| 客户租户 | `type = msp_customer`，承载一个客户的用户与全部业务数据 |
| MSPAllocation | 服务商员工 ↔ 客户租户的分配记录（`primary/backup/specialist`，含 `deassigned_at`） |
| `X-Customer-Tenant-ID` | 服务商员工跨客户操作时携带的目标客户租户标识，必须命中其分配列表 |
| `msp_role` | 服务商员工在 provider 租户内的角色（如 `provider_admin` / `provider_agent`） |
| 部署模式 | `private` / `saas` / `saas_msp`；仅后两者开放 `/api/v1/msp/*` |

## 与既有文档的关系

- 选型结论与行动项：[ADR-004](../architecture/adr-004-multi-customer-tenant-model-selection.md)；
- 技术全景与运营案例：`docs/articles/05-multi-tenant-msp-operations.md`；
- 生产初始化与发布：[production-initialization.md](../delivery/production-initialization.md)；
- CMDB 可见性口径：[ADR-003](../architecture/adr-003-cmdb-tenant-wide-default.md)（租户内共享、租户间隔离）。

## 运维脚本（scripts/msp/）

| 脚本 | 作用 |
|---|---|
| `scripts/msp/build-provision-tenant.sh` | 构建 `provision_tenant` 二进制（内网走 goproxy.cn），产物 `$HOME/itsm-artifacts/provision_tenant_linux_amd64`（snap docker 可见路径） |
| `scripts/msp/setup-msp-tenants.sh` | 一键初始化：租户创建 → 模板供给 → MSP 授权 → 首个用户 → 分配 → 隔离性验证（幂等） |

使用方式与阶段说明见 [02 文档 §10](./02-deployment-and-configuration.md)；实测输出见 [06 文档 §7](./06-verification-and-troubleshooting.md)。

## 方案（plan/）

| 方案 | 内容 |
|---|---|
| [msp-concept-model-and-architecture-canon.md](./plan/msp-concept-model-and-architecture-canon.md) | **⭐ 概念模型与架构总纲（Canon v0.8）**：15 个概念的唯一定义与权威载体；四层分层 + 边界规则 B1–B6 + Canonical ER + 不变量 I1–I13；概念→现状→目标映射；术语收敛；子系统挂接规范；部署模式与单/多 provider 决策（选项 A/B）、工单流转（§7.2）与 provider 功能管理（§7.3）；风险 R1–R12、能力缺口 K1–K5；迁移 P0/P1/P2；验收 A1–A12；决策 D1–D10/E1–E6；**附录 C：权威层级 + 编号注册表** |
| [msp-user-lifecycle-and-tenant-switching-plan.md](./plan/msp-user-lifecycle-and-tenant-switching-plan.md) | 多租户用户生命周期与租户切换：功能缺口 F1–F15（建号/登录选租户/切换上下文）、路线 A/B 选型、P0 详细设计、分期与验收 |
| [msp-scope-model-analysis-and-ai-gateway-reference.md](./plan/msp-scope-model-analysis-and-ai-gateway-reference.md) | 服务方/客户方作用域模型分析（Q1 回答）：一个账号、多作用域（membership）目标模型、ai-gateway 多租户设计对照与可借鉴清单 |
| [msp-target-architecture.md](./plan/msp-target-architecture.md) | **目标架构方案**：账号唯一/作用域多元、membership 模型、租户上下文与 fail-closed 解析、隔离与权限（Q7 角色模板）、建号/邀请/首登、数据模型与迁移、前端架构、审计与 A→B 演进 |
| [msp-user-interaction-flows.md](./plan/msp-user-interaction-flows.md) | **用户交互流程图**：10 个流程（Mermaid：开通/邀请/建号/登录/切换/头通道/权限/回收/重置/续期）+ 邀请与会话状态机 + 流程×缺口×接口对照 |
| [msp-login-and-switching-refinement-plan.md](./plan/msp-login-and-switching-refinement-plan.md) | **登录与作用域切换细化方案（隐私优先）**：登录页无租户选择器（客户关系保护）、按 `account_kind` 分派、域名/企业代码定位、服务商登录后顶栏切换器、防枚举、与既有方案 6 项修订 |
| [msp-frontend-pages-and-permissions-analysis.md](./plan/msp-frontend-pages-and-permissions-analysis.md) | **前端页面与权限分析及目标细化**：路由/页面/权限/菜单/租户上下文现状（含权限双源、菜单缓存未分键、`tenants[0]` 等风险）+ 切换器/刷新链路/页面改造清单/分期验收 |
| [msp-cross-customer-workbench-and-filter-plan.md](./plan/msp-cross-customer-workbench-and-filter-plan.md) | **跨客户工作台与全局过滤方案**（替代"全局切换"）：会话作用域/视图过滤器/条目级操作三概念分离；顶栏 `CustomerFilter`（全部/子集+徽标）；工作台列表带客户列、行内处理、批量护栏；资源级授权与 bounded bypass；API/性能/分期 |
| [msp-integration-with-rbac-org-workflow-analysis.md](./plan/msp-integration-with-rbac-org-workflow-analysis.md) | **多租户 × 权限/部门/团队/工作流 集成分析与冲突处置**：五问判定框架；RBAC（`user_roles` 平台级豁免、权限双源、`data_scope` 空承诺）、组织（成员关系无成员行、全局唯一键、零 RLS）、工作流（指派未验租户、授权可覆写、列表 fail-open）、执行器（租户 ctx 不统一）、通知；❌6/🟡14 清单 + 7 条不变量 + 分期 |
| [msp-three-persona-operation-simulation.md](./plan/msp-three-persona-operation-simulation.md) | **三角色业务操作模拟剧本**（平台/服务商/客户）：逐步操作（请求+现状预期+目标预期+缺口标注）；跨视角时序与可见性矩阵；缺口索引（R1–R11/K1–K5；生产实测缺口见 `07:G1–G10`）；验收检查表与执行说明 |
| [msp-docs-consistency-audit.md](./plan/msp-docs-consistency-audit.md) | **文档一致性审计（架构割裂排查）**：权威层级与生效规则、术语/编号注册表、冲突清单 C1–C19（含假阳性撤销）、已执行修复、待办与防复发约定 |
| [msp-implementation-plan.md](./plan/msp-implementation-plan.md) | **实施方案（P0 → P1 → P2）**：结合现有项目的可执行步骤（工作流 `IP-P*-*`）、分阶段出口 DoD、A1–A12 验收映射、发布/灰度/回滚、风险依赖与里程碑 |

## 维护约定

- 本目录为长期文档，文件名不带日期；变更须同步更新受影响文档与 ADR-004 的行动项状态；
- 引用代码请标注 `文件:行号`，行号随重构漂移时应以最新代码为准并更新引用。
- **编号与术语**：新增/引用编号必须查 canon **附录 C** 注册表；跨文档引用带前缀（如 `ADR-004:A8`、`07:G4`、`LOGIN-R3`、`FE-A1`）；字母 A/B 必须带限定词（实现路线/部署选项/拆分方案）。
- **状态行与修订记录一致**；**现状与目标必须分列**（禁止把目标当现状、或反之）。
- **修订回填**：任何文档修订既有结论时，必须回填被修订文档并留指针（权威层级见 canon 附录 C.1）。
- **自动检查**：本目录的一致性由 docs-gate **C.6**（`scripts/docs-gate/check-multi-tenant-consistency.sh`）强制执行——头部四件套、编号注册表（`K#`/`WB#`/`REV#`/`UF-`/`LOGIN-`/`ADR-004:` 前缀）。
