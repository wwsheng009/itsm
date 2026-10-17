# 05 · MSP 客户交付项目（后置路线）

> 状态：**Draft v0.1（设计讨论稿；P2+ 后置，前置为单租户 PM 落地）**
> 更新日期：2026-10-17
> 目标读者：架构、后端研发、MSP 方向产品
> 前置阅读：[04 身份与成员](./04-membership-and-external-users.md)；[multi-tenant canon](../multi-tenant/plan/msp-concept-model-and-architecture-canon.md)、[单据流转架构](../multi-tenant/plan/msp-cross-tenant-record-flow-architecture.md)

---

## 1. 原则（与 canon 对齐，不可动摇）

1. **不做跨租户数据引用**：PM 表不出现指向他租户的字段/外键；交付项目的数据**只属于一个租户**；
2. **跨租户 = 受控访问目标租户数据**：provider 员工访问客户项目 = provider membership + allocation + 客户租户 active + 目标租户 RBAC（canon I5）；
3. **权限在目标租户内计算**（I2），写门禁使用**客户侧业务码**（IP-P2-14/15 已定稿口径），`msp_*` 码不进客户租户词表；
4. **动作二分**（D8）：处理类动作（任务状态推进等）可由 provider 执行；审批类动作（立项/结项/预算审批）保留客户治理面；
5. 记录**不复制、不搬迁**：交付项目主记录在客户租户，provider 只做受控读写与聚合视图。

## 2. 推荐形态：交付项目在客户租户

**数据归属**：`projects.tenant_id = 客户租户`（含任务/里程碑/成员）。provider 通过既有两条通道参与：

| 场景 | 通道 | 机制 |
|---|---|---|
| 跨客户总览（不切换） | 工作台新增端点 | `GET /api/v1/msp/workbench/projects`（聚合）+ 单条详情；复制 `handlers/msp/records.go` + `service/msp_record_projection.go` 的"多类共用实现"模式（recordType=project）；端点级 `RequireMSPPermission` + allocation 允许集合 + 声明租户一致性（400/403 语义同现有） |
| 单客户深度操作（排期/任务/里程碑） | 深度切换（`SwitchTenant`） | 切到客户租户后直接使用客户侧 PM 页面与权限模型（canon §2.3：避免第二套 UI/权限），**前端零新增** |
| provider 写回（条目级） | 7 步授权链 | 同 `service/msp_record_write.go`：allocation ∩ 客户 active ∩ **目标租户业务码**（建议 `project:write`；状态机/版本锁最终裁决），写操作在客户租户内执行 + `tenantctx` 重绑定（I6）+ 逐条审计 |

## 3. 前置改造清单（实施时逐项落地）

| # | 改造 | 位置 |
|---|---|---|
| 1 | 权限码：在 C2 基线 `MSPCustomerScopeRolePermissionCodes()` 增补 `project:read`（viewer 起）/`project:write`（tech 起） | `itsm-backend/internal/authz/roles.go` |
| 2 | 存量客户租户对账：`cmd/reconcile_customer_scope_roles`（幂等）；供给期自动生效 | `pkg/seeder` + `cmd/` |
| 3 | 工作台端点注册 + 预检重生成（codegen） | `router/msp_routes.go`、`middleware/rbac_precheck_gen.go` |
| 4 | 聚合读/写服务按既有模式扩展（recordType=project；写面仅"处理类"） | `handlers/msp/`、`service/msp_record_*.go` |
| 5 | 前端工作台增加"交付项目"面板（对标三类单据面板）；provider 过滤器 `customerTenantIds` 复用 | `itsm-frontend/src/pages/(main)/msp/` |
| 6 | 部署模式门控沿用：`private` 下该面 404（`middleware/msp_gate.go`），不影响单租户 PM | — |
| 7 | 实弹验收：对标既有工作台实弹用例（含越权/撤权反例） | `tests/`、evidence 目录 |

## 4. 交付方法论受控下发（模板分发）

provider 沉淀标准交付模板（阶段/里程碑/任务清单/检查表），按**受控下发（copy-in）**模式发给客户：

- 对标既有实现：`handlers/msp/ticket_type_distribution.go`（工单类型）与 `handlers/msp/workflow_distribution.go`（流程模板）——copy-in + 来源追踪 + 受控锁 + 发布同步 + 解绑转 native；
- 新增 `project_template_distributions`（同构）：provider 模板 → 客户租户草稿副本，客户可本地修改，升级提示按既有分发状态查询模式；
- 原则不变：**分发是复制模板定义，不是共享记录**。

## 5. 合同 / SLA / 结算联动

- 交付项目与 `msp_contract`（服务合同）关联在 provider 侧建立（provider 自己的关联记录，不写进客户项目表）；
- 里程碑/验收节点绑定 `msp_contract_sla_binding` / `sla_definition_grant`；
- 验收结果与 SLA 违约处置（认账/豁免）进既有结算报表口径（D-SLA-5/D-SLA-8/9 已落地能力）。

## 6. 项目任务纳入 provider 待办（可选）

- 先例：`service/msp_task_projection.go`（客户流程任务 → provider 工作台待办，claim/complete 写回在客户租户内执行，`source=workbench` 审计）；
- `ProjectTask` 同构扩展：投影列表 + 认领/完成回写；属"处理类动作"；
- 若不做，则 provider 用深度切换处理任务（P2 最小路径）。

## 7. 备选形态：provider 自有项目组合（P3 评估）

- 场景：provider 内部实施团队管理、跨客户排期、资源负荷、内部成本；
- 形态：项目建在 **provider 租户**（`tenant_id=provider`），客户可见性**默认无**；
- 客户侧可见 provider 项目需要"反向投影"（customer 读 provider 数据），本仓目前投影为 customer→provider 单向，**属新机制**，须单独立项（含权限/审计/RLS 设计）；
- 更稳替代：主记录仍在客户租户，provider 侧只做聚合视图（推荐）。

## 8. 分期与验收

| 阶段 | 交付 | 验收 |
|---|---|---|
| P2-pre | 单租户 PM 已上线并稳定 | 见 [06 实施路线](./06-implementation-roadmap.md) |
| P2-A | C2 基线 + 对账 + 工作台只读项目面板 | 跨客户可见范围 = allocation 集合；越界 403 + 审计；撤权即不可见 |
| P2-B | 深度切换下 provider 完成项目排期/任务推进 | 写门禁按目标租户业务码；状态机/并发冲突 409；审批类动作不可达 |
| P2-C | 模板受控下发 + 合同/SLA 联动 | 分发来源追踪、解绑转 native；验收结果进结算口径 |
| P2-D（可选） | 项目任务待办投影 | claim/complete 写回客户租户 + 审计；反例（撤权后 403） |

## 9. 风险与注意

1. **词表所有权**：不得把 `msp_*` 码写入客户租户；写门禁一律客户侧业务码（IP-P2-14 教训）；
2. **深切换成本**：会话切换是重操作且需审计，日常"看+做"优先工作台，深度配置才切换（canon MSW-6 边界）；
3. **前端口径**：工作台面板数据全部由服务端计算 `allowedActions`，前端不自行推导权限；
4. **测试基线**：新增端点必须补预检 codegen 与路由/权限守卫用例，否则 CI 守卫失败。