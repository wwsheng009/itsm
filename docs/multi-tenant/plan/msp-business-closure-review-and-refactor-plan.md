# MSP 业务闭环审查与现有功能改造计划

> 状态：**Draft v0.3（2026-09-30 决策冻结同步）**｜日期：2026-09-30｜基准：仓库 HEAD `337558e3`
> 定位：**端到端业务闭环审查**（13 条链路 × 五段判定）+ **现有功能改造计划**（模块级改造项 → 批次 → 验收）。
> 上位：[canon](./msp-concept-model-and-architecture-canon.md)（边界/不变量）｜[实施方案](./msp-implementation-plan.md)（`IP-P*-*` 工作流）｜[一致性审计](./msp-docs-consistency-audit.md)（编号/权威层级）
> 配套：[建号与注册流程](./msp-account-provisioning-and-registration-flow.md)｜[三角色演练剧本](./msp-three-persona-operation-simulation.md)｜[现状 01–07](../README.md)
> 编号：链路编号 `CL-01–CL-13`（本文局部，已登记 canon 附录 C）；改造项引用既有 `IP-P*-*`，不新增编号。

---

## 0. 审查方法与判定标准

**闭环定义（五段齐全）**：一条业务链路必须同时具备

1. **入口**：产品内可达的操作路径（API/UI/脚本，非 SQL 直写）；
2. **授权**：显式校验（身份 → 权限 → 范围/归属 → 租户状态），fail-closed；
3. **执行**：业务动作正确落库/落审计；
4. **审计**：关键事件可追溯（actor/target_tenant/source/result）；
5. **回收**：撤销/停用/退租/转岗有闭环（不残留权限与数据）。

**判定**：✅ 闭环（五段齐全且有测试证据）｜🟡 半闭环（功能可用但缺 1–2 段，或依赖脚本/手工）｜⛔ 断链（入口缺失或授权缺失，不可产品化运营）。

**审查范围**：`saas_msp` 模式下 1 平台 + N 服务商 + M 客户的多租户运营主干（租户 → 账号 → 会话 → 授权 → 作业 → 审计 → 退租）。

---

## 1. 端到端链路清单（现状判定）

| 链路 | 目标流程 | 现状（as-is） | 断点（编号） | 判定 |
|---|---|---|---|---|
| **CL-01** 平台建 provider 租户 | 创建 → 供给 → 就绪 → 可用 | `POST /tenants` + `provision_tenant` + 脚本 8 阶段可跑通（02/03） | R12（未知模式默认开启）、R3（双归属字段只写不读）、K1/K2（角色靠 SQL 直写） | 🟡 |
| **CL-02** provider 首个管理员 | 建号 → 首登 → 改密 | **仅 SQL 直写**；bootstrap 写死 `admin`，第二租户撞全局唯一 | `07:G2`（F2）、K4 | ⛔ |
| **CL-03** provider 员工生命周期 | 建号/邀请 → 角色 → 停用/转岗 | 无 API 建号（K4）；`msp_role` 3 值 vs 词表 5 角色（K3）；注册无白名单（F3） | K3/K4、F1/F3/F4 | ⛔ |
| **CL-04** 分配（Allocation） | 创建 → 生效 → 解除 → 校验 | 接口可用（`/msp/allocations`），但**不校验客户归属 provider**；路径/请求体通道绕过校验 | R2、R9、R10 | 🟡 |
| **CL-05** 登录与会话 | 登录 → 落地 → refresh → 登出 | 登录可用；租户状态不校验、`tenantCode` 语义混乱、refresh 回退 home、响应不一致 | F5/F6/F9/F10/F11 | 🟡 |
| **CL-06** 切换与头通道 | 切换 → 审计 → 撤销；头 → 只读 → 403 | 后端切换可用但无审计/无旧 refresh 撤销；前端无入口；头通道仅头部校验 | F12/F13、R9 | 🟡 |
| **CL-07** 跨客户作业 | 工作台（看+做）→ 条目级授权 → 审计 | **工作台不存在**（仅 `/msp` 仪表盘）；无过滤器/条目级写；`allowedActions` 无 | REV-1/WB1–WB6（目标未建） | ⛔ |
| **CL-08** 客户租户生命周期 | 创建 → 供给 → 暂停 → 退租 | 03 文档流程可用（软删除+审计）；归属未校验；暂停/退租后登录未拦截 | R3、F5 | 🟡 |
| **CL-09** 客户账号与权限 | 首管 → 自助建号 → 角色管理 | 首管仅 SQL；客户建号有全局唯一与角色坑；无邀请 | F1/F2/F3/F4、K4/K5 | ⛔ |
| **CL-10** 工单流转（MSP） | 建单 → 快照 → 派单 → 通知 → 关闭 | 基础工单可用；MSP 四字段零写入；指派=自我指派可绕过；通知无 provider 分支 | R11、R9、R8 | 🟡 |
| **CL-11** 组织/团队与权限 | 成员 → 组织 → 数据范围 | 成员关系无成员行；全局唯一键；零 RLS；`data_scope` 空承诺 | 集成分析 ❌6/🟡14 | ⛔ |
| **CL-12** 审计与合规 | 跨租户事件 → 查询 → 告警 | 审计表可用；缺 provider/来源维度、缺 `tenant_source`、无越权告警 | R8、F15、I11 | 🟡 |
| **CL-13** 退租与数据处置 | 停用 → 导出 → 删除/保留 | 软删除 + 审计已有；无数据导出/保留策略、无 membership 回收 | 03 §退租、P1 未覆盖 | 🟡 |

**汇总**：✅ 0 条 ｜🟡 **8 条**（CL-01/04/05/06/08/10/12/13）｜⛔ **5 条**（CL-02/03/07/09/11）。

---

## 2. 闭环结论（关键判断）

1. **主干未闭环**：多客户运营的"账号 → 授权 → 跨客户作业"三段（CL-02/03/07/09）全部断链——这是当前**只能靠 SQL 脚本 + 单租户前端**运营的根本原因（K4、F1–F4）。
2. **安全段先闭环**：CL-04/06/10 的半闭环都卡在同一处——**授权校验不完整**（R9/R10/R2）与**审计缺失**（R8/F15）。不修则"能看多个客户"= 能看**所有**客户（越权）。
3. **半闭环的共性**：入口存在但**缺"回收/审计"段**（登录无租户状态校验、切换无撤销、退租无数据处置）。
4. **可达性结论**：P0 完成后可闭环 **10/13**（CL-01/02/03/04/05/06/07/09/10/12）；P1 完成后 **12/13**（+CL-11、CL-08 完善）；CL-13 需 P2 数据处置策略（导出/保留/删除）后闭环。
5. **依赖顺序不可颠倒**：CL-04/06 的安全闭环（R9/R10）**必须先于** CL-07 工作台上线——否则工作台会把越权面放大（当前路径通道即可读未分配客户）。

---

## 3. 现有功能改造计划（模块级）

> 改造项均映射到[实施方案](./msp-implementation-plan.md)的 `IP-P*-*`；"现状文件/端点"为改造起点（行号随重构漂移，以最新代码为准）。

### 3.1 后端

| 模块 | 现状文件/端点 | 改造项 | 批次 | 验收/证据 | 风险 |
|---|---|---|---|---|---|
| 部署门控 | `middleware/msp_gate.go`、`config/config.go` | 仅 `saas_msp` 开启；未知值 fatal；启动自检 | IP-P0-1 | 模式矩阵测试 + 自检日志 | 过渡期行为变化（需告警期） |
| 租户解析 | `middleware/tenant.go`、`msp_tenant_resolver.go` | 冲突 fail-closed（修 `07:G9`）；来源优先级显式化 | IP-P0-6 | 冲突请求 401 + 日志 | 现有客户端可能依赖静默忽略 |
| MSP 授权 | `service/msp_access_validator.go`、`handlers/msp/*`、`middleware/msp_middleware.go` | 统一 `CanAccessCustomer`；接线/删除死代码；头/路径/请求体同口径 | IP-P0-2 | 3 通道 × 未分配客户全 403 | 历史行为变更（预期） |
| 认证会话 | `handlers/auth/{handler,service}.go`、`handlers/common/service.go` | 登录落 provider 家；refresh 按 claim；切换撤销+审计；响应一致 | IP-P0-6 | F5/F6/F9/F10/F11 用例 | refresh 失效面（需回滚开关） |
| 建号 | `service/user_service.go`、`handlers/user/handler.go`、`router/router.go` | 新建 `service/provisioning.go`；三通道收口；白名单；错误码 | IP-P0-5 | 四通道正/反例；F3 修复 | bypass 收口不彻底会留后门 |
| 角色/权限 | `internal/authz/{roles,catalog}.go`、`middleware/rbac.go`、`pkg/seeder` | 5 个 `msp_*` 入词表 + seed；去硬编码兜底；`msp_role` 收敛 | IP-P0-9 | 新租户 seed 权限齐备 | 与脚本 SQL 双写冲突（需切换期） |
| MSP 路由 | `router/msp_routes.go` | 新增用户管理 + 工作台端点（`/msp/customers/:id/users`、`/msp/workbench/*`） | IP-P0-5/7 | 端点清单 + 403 矩阵 | 路由膨胀（需分组守卫） |
| 工单 | `service/ticket_service.go`、`ent/schema/ticket.go`、`repository/*` | 快照写入 + provider 收窄 + 指派校验 | IP-P0-3 | 快照断言 + 过滤正确 | 存量回填（幂等脚本） |
| 审计 | `service/audit*`、中间件 | `channel/target_tenant/source/membership_id` 统一；事件目录 | IP-P0-10 | 审计查询按 target_tenant | 字段膨胀 |
| 数据模型 | `ent/schema/{user,tenant,msp_allocation,ticket}.go`、`ent/migrate` | `must_change_password`、`last_active_tenant_id`、allocation 唯一索引、归属字段收敛 | IP-P0-4/P1-1 | 迁移 + 巡检 0 差异 | 在线 DDL（低风险，只增） |
| 组织/团队 | `service/approver/*`、`internal/schema/tenant_guard.go` | 成员关系迁 membership；唯一约束；复合 FK；guard 扩展 | IP-P1-3/P2-5 | A5 + 巡检 | 与既有审批链路耦合 |
| 工作流/执行器 | `handlers/{bpmn,approval,approval_chain,timer,automation_rule,escalation_matrix}/`、`ent/schema/{process_*,workflow*}.go` | 执行器/定时器租户 ctx 统一；指派校验；列表 fail-closed | **IP-P0-11** | 跨租户 ctx 用例 + 指派反例 | 与审批链耦合（错 ctx 会跨租户执行） |
| RLS | `database/rls/migrations/*` | membership/组织/邀请入 policy；shadow → enforce | IP-P1-7/P2-2 | shadow 0 新增错误 | enforce 误伤（需灰度） |

### 3.2 前端（itsm-frontend）

| 模块 | 现状文件 | 改造项 | 批次 | 验收/证据 | 风险 |
|---|---|---|---|---|---|
| 会话启动 | `lib/auth/session-bootstrap.ts`、`AuthGuard.tsx` | 移除 `tenants[0]`；尊重服务端上下文 | IP-P0-8 | FE-A1/A2 | 旧缓存迁移（清理策略） |
| 上下文/Store | `lib/store/auth-store.ts` | 切换后重拉 `/auth/me`；登出 clear + reset | IP-P0-8 | FE-A3 | — |
| API 层 | `lib/api/{http-client,tenant-api}.ts` | 端点修正（`/auth/switch-tenant`）；头注入与清理 | IP-P0-8 | FE-A1 | 旧端点残留调用 |
| 顶栏 | `components/layout/header/*` | `CustomerFilter`（主控件）+ "进入客户"（降级）；上下文指示 | IP-P0-7/8 | WB-A2/WB-A6 | 双控件语义混淆（培训） |
| MSP 页面 | `pages/(main)/msp/**` | 工作台（列表带客户列 + 行内操作 + `allowedActions`） | IP-P0-7 | WB-A1–A6 | 性能（逐租户查询） |
| 权限链路 | `useUserMenusQuery.ts`、`useCapabilities.ts` | queryKey 按租户分键 + invalidate | IP-P0-8 | 切换后菜单/权限全换 | 缓存穿透 |
| 路由/守卫 | `routes/*`、`lib/router/*` | 独立 403；分组守卫；单一权限模型 | IP-P0-8 | FE-A7/A8 | 遗留路由元数据清理 |
| 登录/注册页 | `pages/(auth)/login/*`、`register/*` | 登录页无租户列表；注册角色白名单透传 | IP-P0-5/8 | FE-A4；F3 修复 | 注册策略需产品确认（D2） |

### 3.3 脚本/运维与验证资产

| 资产 | 改造项 | 批次 | 验收 |
|---|---|---|---|
| `scripts/msp/setup-msp-tenants.sh` | 阶段 5/6 由"SQL 直写"改为"校验 + 兜底"（默认不写） | IP-P0-9/P1-5 | 重复执行幂等；新租户不跑脚本权限齐备 |
| `scripts/msp/build-provision-tenant.sh` | 保持（路径 `$HOME/itsm-artifacts` + sha256 已文档化） | — | 02 §10 一致 |
| `scripts/smoke-test.sh`、`test_permissions_and_menus.sh` | 增加多租户反例用例（未分配 403/客户三无） | 每批 | 用例通过 |
| 三角色演练剧本 §8 checklist | 按批次更新勾选项（P0/P1/P2 映射） | 每批 | P0 项全过 |
| `scripts/docs-gate/*`（C.6） | 保持；新增文档纳入检查 | 每批 | 6/6 |

### 3.4 数据迁移清单（只增不删）

| 批次 | 变更 | 回滚 |
|---|---|---|
| P0 | `users.must_change_password`、`users.last_active_tenant_id`、`msp_allocations` 部分唯一索引 | 停写/删索引 |
| P1 | `user_tenant_memberships`、`invitations`、组织复合 FK | 表可下线（读路径回退） |
| P2 | `msp_allocations.provider_tenant_id`、RLS policy | 字段可空/模式回退 |

### 3.5 按功能域的改造深度分级（结论：深水区是横切能力，不是业务域）

**判据**：**D3 深度** = 动数据模型/权限模型/执行上下文/DB 安全策略；**D2 中等** = 模型不动，加"授权矩阵 + 查询/投递收窄 + 审计"；**D1 轻量** = 配置/登记/命名/一次性数据；**D0 无需**。

**D3 深度（6 项横切能力）**：

| 能力域（代码位置） | 深改原因 | 批次 |
|---|---|---|
| RBAC/权限（`rbac/`、`internal/authz`、`role*`、`permission*`、`menu`） | 权限双源 + `user_roles` 平台豁免 + `msp_*` 不在词表 | IP-P0-9 → IP-P1-2 |
| 组织/部门/团队（`department/`、`group/`、`team`、`source_organization`、`cab_member`） | 成员关系无成员行 / 全局唯一键 / 零 RLS | IP-P1-3 |
| 流程/审批/自动化/定时器（`handlers/{bpmn,approval,approval_chain,timer,automation_rule,escalation_matrix}/`、`ent/schema/{process_*,workflow*}.go`） | 执行器 ctx 不统一、指派未验租户、列表 fail-open | **IP-P0-11** |
| RLS/数据访问（`database/security.go`、`tenant_guard`、`rls/migrations`） | 豁免/低权/ctx 覆盖不全 | IP-P1-7 → IP-P2-2 |
| 用户/认证（`user/`、`auth/`、`bootstrap_token`、`password_reset_token`） | 全局唯一、bootstrap 写死、建号无通道 | IP-P0-5/6 → IP-P1-4/5 |
| 审计（`auditlog`、`process_audit_log`） | 缺 `target_tenant/source/membership` | IP-P0-10 → IP-P1-8 |

**D2 中等（业务域，模型不动，统一三件套）**：

| 模块 | MSP 口径 | 动作 |
|---|---|---|
| CMDB/资产/云/发现 | provider 默认只读；写/变更/发布禁止；租户内闭环 | 授权矩阵 + UI 隐藏/禁用 + 导入导出按租户 |
| 知识库/已知错误 | provider 只读、客户内写 | 同上 |
| 服务目录/服务请求 | 客户内闭环；provider 只读 | 同上 |
| 变更/发布/标准变更 | provider 默认禁止（审批/执行在客户内） | 高危禁令 + 审批链校验 |
| SLA/报表/分析/仪表盘 | 按分配收窄 + provider 维度 | 查询改造（P0 部分 / P2 完整） |
| 通知/消息/邮件/连接器/IM | provider+客户双投递（A12）；模板租户化（D3） | 路由分支 + 邀请 SMTP（P1） |
| 智能分派/派单/升级矩阵 | 被指派者 ∈ provider ∧ allocation | 指派校验（与 IP-P0-2 同批） |
| 附件/对象存储 | 条目级访问校验 + key 租户维度 | 接入统一授权入口 |
| 缓存 | key 必带租户维度 | 逐 key 审查（`ADR-004:A8`） |
| AI/搜索/向量/MCP | AI 服务无租户状态 → 参数收敛 | 调用鉴权 + `tenantId` 校验 |
| 系统配置/共享表（`systemconfig`、`tag`、`prompt_template`、`marketplace_item`） | 区分平台级 vs 租户级；共享需登记复核 | 登记 + 季度复核（D3 决策） |

**D1 轻量**：部署门控 R12（IP-P0-1）；内置角色/审批组/序列（K1/K2、`07:G4/G5`，一次性脚本）；共享表登记；脚本路径（已改）。
**D0 无需**：业务实体模型与领域逻辑本身（`tenant_id` 边界已在）；纯展示/统计口径除外。

**三个关键判断**：
1. 深度改造清单 = **6 项横切能力**；业务域统一三件套，不逐域发明 MSP 概念（第三重约束统一用客户租户内 RBAC 表达）；
2. **顺序不可颠倒**：权限/执行器 ctx（深水区）→ RLS → 业务域授权矩阵；
3. 原实施方案缺"执行器/定时器 ctx 统一"工作流 → 已补 **IP-P0-11**（= 原 canon §8 P0 ⑤ / 集成分析 §5.2）。
---

## 4. 改造顺序与发布单元（现有功能视角）

### 4.1 依赖关系（不可颠倒）

```text
IP-P0-1 门控 ──┐
IP-P0-2 授权修复（R9/R10）──┬──→ IP-P0-7 工作台（先安全后放大可见面）
                            │
IP-P0-6 登录/切换契约 ──┬───┴──→ IP-P0-8 前端链路（依赖端点与响应契约冻结）
IP-P0-5 建号通道 ──┬──→ IP-P0-9 角色供给（seed 与通道同批验证）
                   └──→ IP-P1-4 邀请（复用白名单与审计）
IP-P0-3 工单快照 ──→ IP-P2-1 provider 维度（快照字段先行）
IP-P1-1 membership ──→ IP-P1-2 权限单源 ──→ IP-P1-3 组织 ──→ IP-P1-7 RLS
```

### 4.2 发布波次（每个波次 = 可独立发布的单元）

| 波次 | 内容 | 前置 | 出口证据 |
|---|---|---|---|
| **W1 安全** | IP-P0-1 + IP-P0-2（含 R9/R10/R12） | 无 | 未分配客户 3 通道 403；模式矩阵测试；剧本 M10 |
| **W2 会话** | IP-P0-6 + IP-P0-8（后端契约 + 前端链路，可并行） | W1 | F5/F6/F9–F12 用例；FE-A1–A5 |
| **W3 建号** | IP-P0-5 + IP-P0-9 | W2（登录落 home 后首登流程可验） | 四通道正/反例；F3 修复；K1/K2 关闭 |
| **W4 业务面** | IP-P0-7 + IP-P0-3 | W1（必须） | WB-A1–A6；快照断言；A8 |
| **W5 收尾** | IP-P0-4 + IP-P0-10 | W3 | A1/A2；审计查询 |
| **W6 Membership** | IP-P1-1 → IP-P1-2 → IP-P1-3 | W5 | A4/A5/A6；回填巡检 0 差异 |
| **W7 生命周期** | IP-P1-4 + IP-P1-5 + IP-P1-6 | W6 | 邀请/首登链路；`07:G2` 关闭；WB-A4 |
| **W8 数据面** | IP-P1-7 + IP-P2-1 + IP-P2-2 | W7 | A7；A11/A12；enforce 无 500 |

**并行建议**：W2 内前后端并行；W3 与 W4 可部分并行（不同模块）；W6 数据迁移与 W7 邀请开发可并行（接口先冻结）。

---

## 5. 闭环验收（链路级 DoD）

### 5.1 链路验收表

| 链路 | 闭环验收（P0/P1 后） | 证据 |
|---|---|---|
| CL-01 | 建 provider 租户 → 供给 → 首个管理员可用，全程无 SQL | 脚本/API 记录 + 就绪探针 |
| CL-02 | 连续 2 租户 bootstrap 成功；首登强制改密 | `07:G2` 回归用例 |
| CL-03 | 服务商经 API 建员工/邀请；停用后无法登录且 allocation 失效 | K4/F4 用例 |
| CL-04 | 跨 provider 分配被拒；解除后立即失去访问 | R2 反例 + 巡检 |
| CL-05 | 登录校验租户状态；refresh 不回退；响应一致 | F5/F9/F11 用例 |
| CL-06 | 切换撤销旧 refresh + 审计；头通道未分配 403 | F12 用例 + 审计查询 |
| CL-07 | 工作台看+做全链路；未分配客户不可见；条目级审计 | WB-A1–A6 |
| CL-08 | 暂停/退租后登录与 API 全拒；归属校验生效 | 租户状态用例 |
| CL-09 | 客户首管（平台/msp 通道）→ 客户自助建号/邀请 | 建号流程文档 §9 |
| CL-10 | 建单落快照；按 provider 过滤正确；指派校验 | R11 回归 + A12 前置 |
| CL-11 | 成员=membership；跨租户组织关联被拒；数据范围语义明确 | A5 + 集成用例 |
| CL-12 | 审计含 target_tenant/source；越权尝试可查 | 审计查询 + 告警 |
| CL-13 | 退租：停用 → 导出 → 按策略删除/保留；membership 回收 | 退租演练记录（P2） |

### 5.2 回归清单（改造不得破坏的既有功能）

- [ ] 单租户（`private`/`saas`）登录、RBAC、菜单、工单基础流转不受影响；
- [ ] 平台管理员的租户/用户治理路径（现状可用部分）保持；
- [ ] `provision_tenant` 供给流程与 8 阶段脚本幂等性保持；
- [ ] 通知（租户级 email connector）与工单邮件路径不回归（F4 补充核查）；
- [ ] 前端旧路由/页面在 P0 改造后仍可访问（灰度期内）。

### 5.3 安全反例（必须全部拒绝）

1. 未分配客户：头/路径/请求体 3 通道 → 403；
2. 客户账号跨租户 / 被加入第二个租户 → 拒绝（P1 起 DB 兜底）；
3. 服务商访问未分配客户的工单详情/回复/指派 → 403；
4. 注册/邀请提权（`super_admin` 等）→ 拒绝；
5. 非平台身份指定目标租户（query/body/header）→ 拒绝；
6. 切换后旧 refresh 复用 → 401。

---

## 6. 风险、决策与估算

| 项 | 说明 | 处置 |
|---|---|---|
| 决策已清零（2026-09-30） | D2（直客）、D3（`messages` 租户化）、D5（平台 membership）、D8（批量边界）、E1–E6（工单流转）全部确认（canon v1.0 §10） | 按 canon / [实施方案 §3.0](./msp-implementation-plan.md) 直接实施 |
| 邀请契约 | ✅ 已冻结（2026-09-30）：token TTL 72h（可配）、sha256 哈希、一次性、撤销 API；`invitations` DDL 随 `IP-P1-4` | 见[目标架构 §6.2](./msp-target-architecture.md) |
| 服务商开租户 | ✅ 已确认：当前平台通道（P2 前不开放自助建租户） | 记入 P2 评估 |
| P1 契约 | 组织关联子表 / allocation provider 列 / invitations DDL | ✅ 2026-09-30 冻结于[实施方案 §4.0](./msp-implementation-plan.md) |
| 数据回填 | 存量工单/成员/分配回填 | 幂等脚本 + dry-run 差异清单 |
| 工作量（粗估） | W1–W5（P0）25–40 人日；W6–W8（P1）25–35；P2 15–25 | 以"波次 = 发布单元"校准 |

---

## 7. 与其它文档的关系

| 文档 | 关系 |
|---|---|
| [实施方案](./msp-implementation-plan.md) | 提供 `IP-P*-*` 步骤/DoD；本文提供"链路闭环判定 + 模块改造映射" |
| [建号与注册流程](./msp-account-provisioning-and-registration-flow.md) | CL-02/03/09 的流程规范 |
| [三角色演练剧本](./msp-three-persona-operation-simulation.md) | 链路验收的执行载体（P/M/C 系列步骤） |
| [一致性审计](./msp-docs-consistency-audit.md) | 编号与权威层级规则 |

---

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：13 条链路闭环审查（五段判定）、闭环结论（P0 后 10/13、P1 后 12/13）、模块级改造计划（后端/前端/脚本/迁移）、发布波次 W1–W8、链路级 DoD、回归与反例、风险与估算 |
| v0.2 | 2026-09-29 | 新增 **§3.5 按功能域的改造深度分级**（D3 六项横切 / D2 业务域三件套 / D1 / D0）；后端改造表补"工作流/执行器"行并指向 **IP-P0-11** |
| v0.3 | 2026-09-30 | **决策冻结同步**：§6 未决项清零（D2/D3/D5/D8/E1–E6 确认）；邀请契约与自助开租户结论固化；基准 HEAD 重钉 `337558e3` |
| v0.4 | 2026-09-30 | §6 标题术语对齐（"未决"→"决策"，内容已于 v0.3 清零）；补 P1 契约冻结指针（实施方案 §4.0） |
