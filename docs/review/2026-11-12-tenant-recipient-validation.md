# C 方案 Phase 4 第一批：接收人租户校验（2026-11-12）

> **Status: current (deployed & verified on 238)**
> **范围**：工单 CC、BPMN CC service task、工单审批委派、BPMN 任务 actor/target 校验
> **设计基线**：[membership-tenant-identity-c-plan.md](../multi-tenant/plan/membership-tenant-identity-c-plan.md)
> **当前运行态**：`http://172.18.3.238:8089` / `1.8.0-dev.5639c21c9`
> **部署证据**：[2026-11-12-demo-full-deploy-5639c21c9.md](./evidence/2026-11-12-demo-full-deploy-5639c21c9.md)

## 1. 问题

Phase 3 已把工单 CC、审批委派的 membership 引用写入数据库，但接收人资格仍有两类风险：

1. **合法 provider 被误拒**：`users.tenant_id` 是 provider 家租户，而业务行属于客户租户；
   以 `users.tenant_id == tenant_id` 过滤会拒绝持有客户租户 active membership 的 provider。
2. **非法跨租户目标可能写入**：审批委派在无 BPMN 实例时直接写 `ticket_approvals`，
   原实现只解析目标 membership，解析失败仍继续写入，形成 `user_id` 与目标租户错配。

## 2. 本轮实现

### 2.1 统一接收人判定

本轮采用**迁移窗口内 fail-closed 的跨租户边界**：

- 账号 active；
- `users.tenant_id == tenant_id` 的 home tenant 用户沿用兼容窗口；
- 跨租户账号必须存在 `(user_id, tenant_id)` 的 active、未删除、未过期 membership；
- membership 查询错误直接失败，不降级放行。

### 2.2 工单 CC

- `CCTicket` 不再用 `users.tenant_id` 过滤接收人；
- 批量解析接收人 active membership，任一无 membership 的跨租户用户导致整体失败；
- `ticket_ccs.user_membership_id / added_by_membership_id` 使用已解析结果，避免二次查询；
- 抄送添加人同样校验跨租户 membership；
- CC 查询/管理权限改为 membership 优先，provider 使用 `membership.msp_role`
  （`msp_admin/msp_manager/msp_tech/msp_specialist`）判定，不把 provider 家租户角色用于客户租户；
- `TicketCCRecordResponse` 增加 `userMembershipId / addedByMembershipId`，前端类型同步。
- 路由权限修正：CC 写/删从 `workflow:write` 改为 `ticket:update`，CC 查询从
  `workflow:read` 改为 `ticket:read`。`workflow:*` 保留给流程设计与流程实例管理，
  不应成为工单协作的前置门槛；工单级授权仍由 `ensureCanCCTicket` 执行。
- 前端抄送入口改为 `ticket:update` 或 `workflow:write` 任一即可，避免 provider/技术员
  已在服务层获准但按钮被隐藏。

### 2.3 BPMN CC

- 所有显式用户接收人执行 fail-closed 校验，任一无资格用户使任务失败，不再静默跳过；
- `addedBy` 缺失时回退 BPMN actor context；跨租户 `addedBy` 必须有当前租户 membership；
- 接收人 membership 批量解析后直接写入 `ticket_ccs`。

### 2.4 审批委派

- 委派前解析目标账号与目标租户 membership；目标不存在、停用或跨租户无 membership
  直接拒绝，业务审批记录不变；
- 拒绝委派给自己；
- 已写入 `approver_membership_id` 的审批记录，要求当前审批人 membership 与记录一致；
- 原审批记录与新建委派审批记录复用同一解析结果，不再出现“解析失败仍写入”的分叉。

### 2.5 BPMN 任务 actor/target

- `AssignTask`、`DelegateTask`、`AddApproverTask`、`ReassignTask`、`TerminateTask`
  的操作人改为“home tenant 或当前租户 active membership”；
- 目标处理人按 ID/用户名解析 active 账号，跨租户必须持有当前租户 membership；
- provider 作为客户租户审批人可以发起委托，也可以接受委托。

### 2.6 RBAC 预检与路由守卫同源

- 修复 deep-switch 下“`/auth/me` 有 `ticket:update`、但请求在 RBAC 预检阶段 403”：
  `RBACMiddleware` 的路径预检在 `SmartCheckPermission` 未命中时，按同一
  `ResourceActionMap` 条目调用 `AuthorizeResource`，使用 JWT 主角色 + membership
  合并角色的 DB `role_permissions` 集合；
- 该回退不绕过路由级 `RequirePermission`，两道闸继续使用同一权限口径；
- 重新生成 `middleware/rbac_precheck_gen.go`，CC 预检条目同步为
  `ticket:read / ticket:update`；新增 middleware 回归测试锁定该场景。

### 2.7 错误映射

- 工单流转 handler 不再把所有业务校验错误统一映射为 500；
- 权限错误 → 403，资源不存在 → 404，跨租户/状态/委派/校验错误 → 400；
- 未知基础设施错误仍走 `FailWithErr`，保留 500 且不泄露原始错误。

## 3. 行为契约

| 场景 | 结果 |
|---|---|
| 客户 home 用户 CC/委派 | 允许，membership 可用时落库 |
| provider 持有客户租户 active membership | CC、审批、BPMN 任务允许 |
| 外部租户用户无客户租户 membership | CC/委派 fail-closed，无业务行写入 |
| 审批记录已有 `approver_membership_id`，请求人 membership 不匹配 | 403/业务错误，拒绝审批 |
| 委派给自己 | 拒绝 |
| membership 查询基础设施错误 | 拒绝，不降级 |

## 4. 验证

本地门禁：

```text
GOGC=10 GOMAXPROCS=1 go test -p=1 -gcflags=all=-l ./service -count=1   PASS
go test ./service/bpmn ./middleware ./dto                              PASS
npx tsc --noEmit                                                       PASS
改动文件 ESLint                                                         PASS
```

新增回归：

- `TestCCTicketAllowsProviderMembershipAndRejectsForeignTarget`
- `TestCCTaskHandlerValidatesRecipientMembership`
- `TestApproveTicketDelegateValidatesMembership`
- `TestDelegateTask_AllowsProviderMembershipTarget`
- `TestDelegateTask_AllowsProviderMembershipActor`
- `TestRBACPrecheckUsesMembershipRoleDBPermissions`
- E2E `B4b MSP 深切后抄送目标必须属于客户租户`

238 运行态：

```text
version=1.8.0-dev.5639c21c9
commit=5639c21c92e831ca35a5251cef87ddfe234e49f9
branch=deploy/full-5639c21c9
readyz=ready
demo containers=6/6 healthy
```

E2E 分组独立进程：

```text
00-smoke                    6 passed
01-permissions              8 passed
02-associations             8 passed
03-workflows                8 passed（含 B4b）
04-ui-ux                   11 passed
05-multi-tenant-business    4 passed
06-attachment-access        1 passed
合计                       46/46
```

运行态接口抽查：

```text
provider 深切客户租户后合法 CC -> HTTP 200
无客户租户 membership 的外部用户 CC -> HTTP 400
```

数据巡检（2026-11-12 00:13:08Z 后新增行）：

```text
ticket_ccs            5/5 双方 membership
attachments           3/3
ticket_comments      18/18
notifications         3/3
ticket_notifications 173/173
租户错配              0
```

E2E 断言器补充：只有同一接口在失败后出现后续成功响应时，才忽略该次瞬时
4xx 控制台噪音；持久 4xx 仍会导致测试失败。完整部署与镜像证据见
[2026-11-12-demo-full-deploy-5639c21c9.md](./evidence/2026-11-12-demo-full-deploy-5639c21c9.md)。

## 5. 未完成与边界

- home tenant 用户仍保留无 membership 兼容窗口；最终收口应要求所有写路径均有 active membership；
- BPMN `ccType=group/role` 仍按 `users.tenant_id` 选人，provider 的客户租户组织/角色选人待后续；
- 前端目前只接收 `userMembershipId / addedByMembershipId`，尚未在 CC 列表显式展示；
- 问题/变更/服务请求的创建人/处理人 membership 迁移仍未开始。
