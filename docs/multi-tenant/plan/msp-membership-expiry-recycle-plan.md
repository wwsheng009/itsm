# Membership 到期回收方案（Expiry Recycle：惰性判定 + 后台任务）

> **状态**：v1.0（2026-10-09）——**已落地并实弹验证**（IP-P2-9；见 §5 实施记录）
>
> **批次**：`IP-P2-9`（实施方案 §3.0 总表登记；offboarding 方案 §4 P2「另行立项」收口）
>
> **上位（权威顺序）**：[offboarding 方案](./msp-offboarding-orchestration-plan.md) §4 P2 → [scope-model](./msp-scope-model-analysis-and-ai-gateway-reference.md) B.3.5/B.8 → [MSW 收口方案](./msp-membership-switch-consolidation-plan.md)（MSW-3 回收语义复用）→ [user-lifecycle 方案](./msp-user-lifecycle-and-tenant-switching-plan.md) §7（RLS enforce 前置验证项）

---

## 1. 背景与目标

`user_tenant_memberships.expires_at` 自 IP-P1-1 建表即有（schema 注释「作用域到期（IP-P1-3 起生效）」），但落地核对发现**没有任何执行点**：既无读路径判定、也无后台回收——已到期的成员身份仍可登录/切换/持权，与 scope-model B.3.5「到期自动失效（惰性判定 + 后台回收任务）」不符；user-lifecycle §7 亦把「`expires_at` 到期回收有效」列为 RLS enforce 前的验证项之一。

**目标**：

1. **惰性判定**（读路径 fail-closed）：到期即时失效，不依赖后台任务先跑；
2. **后台回收**（状态收敛）：软删 + 分配级联解除 + 审计 + 立即生效副作用；
3. **home 豁免**：账号级语义不自动回收（避免把账号锁死在租户之外）；
4. **幂等 + 多实例安全**。

---

## 2. 语义（冻结）

| 项 | 口径 |
|---|---|
| 判定 | `expires_at IS NULL` = 永不过期；`> now` = 有效；`<= now` = 视为不存在（读路径与后台回收**同一判定**） |
| 读路径 | 5 处 active membership 解析点全部追加 `middleware.MembershipNotExpired()`：权限解析（`ResolvePermissions`，Login/Refresh/Switch/GetMe/菜单共用）、切换邀请依据、工作台成员计数、组织挂靠校验、客户侧「服务方成员」展示 |
| 后台回收 | 扫描「存活 + `expires_at <= now`」→ 逐条复用 MSW-3 回收核心：**allocation 级联解除 + 条件软删 + `membership.revoke` 审计 + 立即生效**（权限缓存单点失效 + (user×tenant) 在途 refresh 吊销） |
| home 豁免 | `source=home` 的到期行**跳过并告警**（配置异常可见化；账号级到期由账号停用/租户到期承载） |
| 幂等/并发 | 条件更新（`deleted_at IS NULL` 守卫）n=0 视为已被并发执行者回收（不重复审计/副作用）；启动即跑一轮 + 定时（默认 10 分钟） |
| 审计 | `membership.revoke`，`trigger=membership.expired`、`outcome=expired`（source=system；系统触发 actor=0） |

---

## 3. 实现落点

| 层 | 落点 | 说明 |
|---|---|---|
| 判定谓词 | `middleware/membership_expiry.go` | `MembershipNotExpired()`（NULL/未来有效；过去排除） |
| 读路径收口 | `middleware/membership_permission.go`、`handlers/auth/service.go`、`service/msp_workbench.go`、`service/membership_org_service.go`、`service/user_service.go` | 5 处追加谓词（fail-closed，即时生效） |
| 回收核心 | `service/membership_sync.go`（抽出 `revokeMembershipCore`；`DeactivateMembership` 复用） | 条件软删守卫（多实例/重跑安全）；allocation 级联解除沿用 MSW-3 口径 |
| 后台任务 | `service/membership_expiry.go` + `internal/bootstrap/app.go`（`membership-expiry-recycler`） | 启动一轮（收敛停机存量）+ ticker；跨租户扫描走 `tenantctx.SystemContext` |

---

## 4. 开关与回滚

- `MEMBERSHIP_EXPIRY_RECYCLE_ENABLED`：默认**开**；显式 `false/0/no/off`（大小写/空白容忍）关闭回滚（关闭后**惰性判定仍生效**，仅停止后台收敛）；
- `MEMBERSHIP_EXPIRY_RECYCLE_INTERVAL_MINUTES`：默认 10；非法/<=0 回退默认；
- 回滚：无 DDL、无 schema 变更；关闭开关 / 回退二进制即停；已回收行（`deleted_at`）不回滚（登记变更单）。

---

## 5. 验证（实施记录，2026-10-09）

| 层 | 用例/实弹 | 结果 |
|---|---|---|
| service | `TestMembershipExpiry_RecyclesExpiredAllocationMembership`（软删 + 级联 + 审计 + 幂等重跑不重复审计）、`TestMembershipExpiry_SkipsFutureAndHome`、`TestMembershipExpiryRecycleEnvParsing` | 全绿 |
| middleware | `TestResolvePermissions_ExpiredMembershipFailsClosed`（到期 fail-closed + 续期恢复）、`TestMembershipNotExpired_PredicateSemantics` | 全绿 |
| 回归 | `./service`（全量）、`./middleware`（含 `rbac_precheck_gen.go` 重新生成）、`./handlers/auth`、`./handlers/user`、`./handlers/rbac`、`./router` | 全绿 |
| 实弹① 后台回收 | 联调实例：构造「分配型 + 已到期」membership（user 7 / customer tenant 6 / allocation 7）→ 重启触发启动轮 | 日志 `scanned=1 recycled=1 failed=0`；membership `live=false` + `deassigned`；allocation 级联解除；审计 `trigger=membership.expired`（`allocation_id=7`） |
| 实弹② 惰性判定 | 再构造 membership 94（存活、已到期）→ 进程外复算 `middleware.ResolvePermissions`（Login/Switch/GetMe 共用函数） | `permissions=[] source=none`（fail-closed，未等后台任务）；随后第二轮回收收敛（`scanned=1 recycled=1`；allocation 40 级联解除） |

> 实弹为一次性探针（`tmp-expiry-probe`，用后即删）；探针数据（分配型 membership/allocation）由回收任务自身清理，未留残数。

---

## 6. 残余（登记）

1. **设置 `expires_at` 的管理面**：当前无 API/UI 写入 `expires_at`（个人级临时提权场景）；本批只做「到期执行」。触发条件：出现「限时提权」需求时随 Q7 个人级调整面一并立项（scope-model B.8）；
2. **季度权限复核**：scope-model B.8 ②「高权角色（如 `msp_admin`）变更告警 + 季度复核」为运营机制，不在本批；
3. 多实例「同轮重复扫描」为幂等无害（条件更新去重）。

---

## 7. 修订记录

| 版本 | 日期 | 说明 |
|---|---|---|
| v1.0 | 2026-10-09 | 首版：语义冻结 + 5 处读路径收口 + 回收核心复用（条件软删守卫）+ 后台任务（开关/间隔/启动一轮）+ 单测/回归/两轮实弹 |
