# MSP 离职编排与分配回收方案（Offboarding Orchestration）

> **状态**：v0.6（2026-10-09）——**P0 + P1 + P2 全部落地并实弹验证**（前端入口 + 平台 TUM 面 + F-08 剧本 + 验收脚本 + 到期自动回收，见 §6 实施记录）；决策点 D1–D4 按 §3.7 推荐值落定（D1=A / D2=A / D3=B / D4=A）；P2 独立立项收口于 [membership 到期回收方案](./msp-membership-expiry-recycle-plan.md)（IP-P2-9）
>
> **批次建议**：`IP-P2-8`（落库前对照《[实施方案](./msp-implementation-plan.md)》§3.0 总表登记，未登记前本方案不视为冻结）
>
> **上位（权威顺序）**：[canon](./msp-concept-model-and-architecture-canon.md)（I3/I5/B3/B5/D5/D10/D11）→ [目标架构](./msp-target-architecture.md)（P1 剩余项「分配回收编排 + 历史接口」，历史接口已于 2026-10-08 补齐）→ [用户交互流程 F-07/F-08](./msp-user-interaction-flows.md) → [MSW 收口方案](./msp-membership-switch-consolidation-plan.md)（MSW-1/MSW-3、Q3=A 立即生效口径）→ [TUM 方案](./msp-tenant-user-management-enhancement-plan.md)（平台面 `user.admin_*` 审计与错误码复用）

---

## 1. 背景与目标

F-08「回收 / 离职 / 退租」的目标动作是「**员工离职 → 停用账号 + 撤销全部分配 + 会话失效 + 审计**」。现状核对（§2）显示各**单点能力齐备**（停用+token 吊销、单对分配撤销、membership 审计、立即生效），缺的是**编排**：没有单一动作把「停用 + 全量回收 + 审计」串起来，provider 管理员需手工逐个回收分配，漏回收即残留。

**目标**：
1. 一条「离职」动作完成：停用账号 + 全量会话失效 + 撤销该员工全部分配（含 membership 联动与立即生效）+ 审计留痕；
2. provider 自服务可用，平台面可复用同一服务（分期，见 D3）；
3. **幂等可重跑**、失败可续、巡检兜底（与 MSW-1 既有口径一致）。

---

## 2. 现状核对（S1–S7）

| # | 能力 | 现状 | 证据 |
|---|------|------|------|
| S1 | 停用账号（单条） | ✅ `active=false` + **吊销全部 access token**（P1-2 延伸）；防自停用 | `service/user_service.go:596-629`（`ChangeUserStatus`） |
| S2 | 批量停用 | ✅ 置 `active=false` + **全量 token 吊销**（IP-P2-8 补齐，2026-10-08；原缺口：仅置位未吊销）；防批量含本人 | `service/user_service.go`（`BatchUpdateUsers`） |
| S3 | 删除用户 | ✅ 软删=置 `active=false` + **全量 token 吊销**（IP-P2-8 补齐，2026-10-08；原缺口：未吊销） | `service/user_service.go`（`DeleteUser`） |
| S4 | 单对分配撤销 | ✅ 置 `deassigned_at` + membership 软删（`membership.revoke` 审计）+ **立即生效**（权限缓存单点失效 + (user×tenant) 在途 refresh 吊销） | `service/msp_allocation_service.go:379-414`（`Deactivate`）、`service/membership_sync.go:251-349` |
| S5 | 全量 token 基建 | ✅ `InvalidateUserAccessTokens` / `InvalidateUserRefreshTokens`（全局）/ `...ForTenant` | `middleware/token_revocation.go:83/95/129` |
| S6 | 审计基建 | ✅ `audit_logs` 含 `target_user_id`/`source`/`target_tenant_id`；membership 通道 `writeSyncAudit` 模式可复用 | `ent/schema/auditlog.go`、`service/membership_sync.go:413-462` |
| S7 | 文档口径 | ⚠️ F-08 标注「无审计」为**过期表述**——membership 层审计（`membership.sync`/`membership.revoke`）已随 MSW-1/MSW-3 落地 | `docs/.../msp-user-interaction-flows.md:314` vs 上述 S4 |

---

## 3. 设计

### 3.1 编排语义（顺序与失败策略）

**顺序**（安全优先）：

1. **停用账号 + 全量会话失效**：`active=false` → `InvalidateUserAccessTokens` + `InvalidateUserRefreshTokens`（全局）；此步失败 = 整体失败（安全面不可妥协）。
2. **撤销全部分配**（逐条幂等）：对每条有效分配执行与 `Deactivate` 等价的核心（`deassigned_at` + membership 软删 + `membership.revoke` 审计 + 缓存失效 + 租户 refresh 吊销）；**逐条失败不阻断**，计入 `pending` 并告警（巡检兜底，与 MSW-1 口径一致）。
3. **汇总审计**：`user.offboard`（含 outcome/计数/trigger）。

**幂等**：重复调用返回相同结果（第二次 `alreadyInactive=true`、`allocationsRevoked=0`）；`pending` 项可重跑收敛。

### 3.2 服务落点

- 新增 `service/offboarding_service.go`：`OffboardUser(ctx, userID, homeTenantID int, meta OffboardMeta) (*OffboardResult, error)`，依赖 `*ent.Client` + `*MSPAllocationService`（组合）。
- 新增 `MSPAllocationService.RevokeAllForUser(ctx, userID int) (revoked []int, pending []int, err error)`：
  - **批量加载**有效分配（1 查询）+ 相关 membership（1 查询），逐条复用撤销核心（抽出 `revokeOne` 供 `Deactivate` 复用）；
  - 遵守仓库性能约定（禁 N+1 逐条查询；审计行 N 条为预期），加查询预算测试（参照 `TestProvisionTenantQueryBudgetStaysBatched`）。

### 3.3 API 契约

| 面 | 端点 | 权限 | 语义 |
|----|------|------|------|
| Provider 自服务（P0） | `POST /api/v1/users/:id/offboard` | `user:write`（租户作用域） | 停用 + 全量回收 + 审计；409 `SELF_OPERATION_FORBIDDEN`（防自离职，复用 TUM 错误码）；404 用户不存在 |
| 平台治理面（P1） | `POST /api/v1/tenants/:id/users/:userId/offboard` | `tenant:write` + L1 平台身份（补丁 4/6 口径） | 复用同一服务；审计 `target_tenant_id` 标注 |

响应（200）：
```json
{ "userId": 123, "deactivated": true, "alreadyInactive": false,
  "allocationsRevoked": 3, "allocationsPending": 0 }
```

### 3.4 审计

- **新事件 `user.offboard`**：`resource=user`、`target_user_id`、`target_tenant_id`（=被停用用户 home 租户）、payload 含 `outcome/allocations_revoked/allocations_pending/trigger/source`；
- 登记：实施方案 §3.0-E 事件目录（canonical）追加 + TUM 方案 §3.4 对齐说明（provider 自服务面事件；平台面如落则同事件名、`source` 区分）；
- 分配侧 N×`membership.revoke` 由既有通道产生（trigger=`msp.allocation.deassign`），无需重复审计。

### 3.5 前端（P1）

- 租户用户管理页（provider 租户）行操作新增「离职（停用并撤销全部分配）」：二次确认（提示将撤销的有效分配数）、成功后刷新；
- customer 租户不展示该动作（避免与普通停用重复；customer 用户无分配语义）；
- 落点：`itsm-frontend/src/pages/(main)/system/users/index.tsx`（租户面；实现时按现状确认入口；`admin/users` 为平台面）。

### 3.6 测试与验收

| 层 | 用例 |
|----|------|
| service | 编排全量（N 分配撤销 + membership 软删 + access/refresh 失效断言 + `user.offboard`×1 + `membership.revoke`×N）；幂等重跑；self 拒绝；无分配用户（纯停用）；撤销失败注入（停用仍完成 + pending 计数 + 告警）；查询预算 |
| handler/router | 路由注册 + `user:write` 门禁 + 404/409 |
| 前端 | 动作渲染/确认弹窗/调用/刷新（jest） |

**验收**：A1 离职后 0 有效分配、A2 会话立即失效（access+refresh）、A3 审计完整（1+N）、A4 幂等重跑 0 变更、A5 巡检 0 漂移。

### 3.7 决策点（待评审）

| 编号 | 决策 | 选项 | 建议 |
|------|------|------|------|
| D1 | 入口面 | A. provider 自服务；B. 仅平台 TUM；C. 双面同批 | **A 先行、平台面 P1**（D3） |
| D2 | 失败语义 | A. 停用先行 + 分配回收尽力而为（可重跑）；B. 全有或全无（单事务） | **A**：与 MSW-1「失败不阻断、巡检兜底」口径一致；单事务跨多资源锁风险高 |
| D3 | 平台面批次 | A. 同批；B. P1 | **B**（平台面 TUM 停用已可用，编排为增量） |
| D4 | 前端可见性 | A. 仅 provider 租户；B. 全租户 | **A** |

---

## 4. 分期

- **P0**：`OffboardingService` + `RevokeAllForUser` + provider 路由 + `user.offboard` 审计 + service/handler 测试。
- **P1**：前端入口（§3.5）+ 平台 TUM 面 + 场景剧本补充（F-08 实操）+ F-08 文档回填（S7）。
- **P2（已完成，IP-P2-9）**：`expires_at` 到期自动回收——惰性判定（5 处读路径 fail-closed）+ 后台回收任务（启动一轮 + 定时；软删 + allocation 级联解除 + `membership.revoke` 审计 + 立即生效）；home 豁免。独立方案：[membership 到期回收方案](./msp-membership-expiry-recycle-plan.md)（scope-model §5 登记项收口）。

## 5. 回滚

- 无 DDL、无 schema 变更；路由下线 / 前端隐藏即回退；已置位数据（`active=false`/`deassigned_at`）不回滚（登记变更单）。

## 6. 实施记录（P0，2026-10-08）

| 项 | 落地 | 测试/验证 |
|----|------|-----------|
| 服务 | `service/offboarding_service.go`：`OffboardUser`（停用先行 + access/refresh 全量吊销 + 全量回收 + `user.offboard` 审计；幂等；哨兵错误 `ErrOffboardUserNotFound`/`ErrSelfOffboard`） | `offboarding_service_test.go`：编排全量（2 分配回收 + membership 软删 + 审计 1+N + refresh 吊销）、幂等重跑、防自离职、跨租户 404 |
| 回收 | `MSPAllocationService.RevokeAllForUser`（批量加载 + 逐条复用 `revokeAllocationCore`；`Deactivate` 同源重构） | `membership_sync_test.go` 既有 Deactivate 用例回归全绿 |
| 路由 | `POST /api/v1/users/:id/offboard`（`user:write`） | `offboard_handler_test.go`（200/404/409/非法 ID 1001）；router 守卫测试全绿 |
| 装配 | `internal/bootstrap/app.go` 注入 `SetOffboardingService` | `go build ./internal/bootstrap` 通过 |
| 回归 | —— | `go test -p 1 ./service/ ./handlers/user/ ./router/` 全绿（跳过已登记偶发 bpmn 用例） |
| 前端（P1 部分） | `admin/users` 用户行操作「离职」：仅服务商租户（D4=A）展示、二次确认 + 已撤销分配计数提示 + 刷新；`UserApi.offboardUser` + i18n 双语 | jest `user-api`（含 offboard 用例）、i18n 套件；`tsc -b` / eslint 绿 |
| 会话缺口（S2/S3） | `DeleteUser`（软删）与 `BatchUpdateUsers(deactivate)` 补全 access+refresh 吊销（与单条停用口径一致） | `user_session_revocation_test.go`（删除/批量停用吊销 + 未涉及隔离 + department 不吊销）；后端回归全绿 |
| 平台 TUM 面（P1） | `POST /api/v1/tenants/:id/users/:userId/offboard`（`tenant:write` + super_admin 硬校验 + 最后管理员护栏）：复用 `OffboardingService` + `user.admin_offboard` 治理审计（`target_tenant_id` 标注）+ 租户通知；TUM 抽屉行操作「离职」+ `TenantUserAdminApi.offboardUser` | e2e（幂等重跑/非平台面 403/审计 2 行）、service 用例（护栏 + 域/治理双审计 + 幂等）、handler 契约、路由契约；抽屉 jest（6 通过）、`tsc`/eslint 绿 |
| F-08 剧本 + 实弹（P1） | [scenarios/11-offboarding.md](../scenarios/11-offboarding.md)（双通道步骤表 + 反例 + 开关前置 + 实测记录）；`scripts/msp/acceptance/run-offboarding-acceptance.ps1`（OFF-0–OFF-7，隔离探针用户，可重复执行） | 实弹 10/10 PASS（建号→分配→403 反例→offboard 200 revoked=1→停用/清零→幂等→审计 2 行）；浏览器实操（平台 TUM 抽屉离职 → 提示「撤销 1 个分配」→ 行刷新停用）；开关未开 fail-closed 实测留证 |
| UX 修复（实弹发现） | TUM 抽屉 `Modal.confirm` 静态调用不消费主题上下文 → 改 `App.useApp().modal` | 控制台告警消除；抽屉 jest 6/6、`tsc -b`/eslint 绿 |

| P2 到期自动回收（IP-P2-9） | 惰性判定 + 后台任务（`membership-expiry-recycler`；开关/间隔登记见部署文档）；读路径 5 处收口 + 回收核心条件软删守卫 | service 3 用例 + middleware 2 用例全绿；回归全绿；实弹两轮（`scanned=1 recycled=1`；审计 `trigger=membership.expired`；惰性判定复算 fail-closed）——详见[到期回收方案 §5](./msp-membership-expiry-recycle-plan.md) |

剩余：无（P0–P2 全部完成；P2 实弹记录见[到期回收方案 §5](./msp-membership-expiry-recycle-plan.md)）。

## 7. 修订记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1 | 2026-10-08 | 首版：现状核对 S1–S7、差距（编排缺失/批量与删除路径 token 缺口/文档过期）、设计（编排语义/服务落点/契约/审计/前端/测试）、决策点 D1–D4、分期与回滚 |
| v0.2 | 2026-10-08 | **P0 落地**（§6 实施记录）；决策点 D1–D4 按 §3.7 推荐值落定；P1 待实施 |
| v0.3 | 2026-10-08 | **前端入口（P1 部分）落地**（§6）+ 停用/删除路径会话缺口补齐（S2/S3）；剩余：平台 TUM 面 + 剧本 |
| v0.4 | 2026-10-08 | **平台 TUM 面落地**（§3.3 契约：端点 + 服务复用 + 双审计 + 抽屉行操作）；P1 剩余：F-08 剧本 |
| v0.5 | 2026-10-09 | **F-08 剧本 + 实弹验证收口**：scenarios/11 + 验收脚本（OFF-0–OFF-7 全 PASS）+ 浏览器实操 + 开关 fail-closed 留证 + 实弹发现的 modal 上下文修复；P1 全部完成，仅剩 P2 另案 |
| v0.6 | 2026-10-09 | **P2 收口（IP-P2-9）**：`expires_at` 到期自动回收落地（惰性判定 + 后台任务 + home 豁免 + 条件更新去重）；独立方案 `msp-membership-expiry-recycle-plan.md`；单测/回归/两轮实弹全绿；P0–P2 全部完成 |
