# 04 · 身份与成员：membership 单源与外部人员简化

> 状态：**Draft v0.1（设计讨论稿）**
> 更新日期：2026-10-17
> 目标读者：架构、后端研发
> 前置阅读：[02 数据模型](./02-data-model.md) §3.1；[multi-tenant canon](../multi-tenant/plan/msp-concept-model-and-architecture-canon.md)

---

## 1. 结论（先看这里）

- **不建 `external_person` 实体、不建外部关系模型**（D-02）；
- 项目成员表 `project_members` **只认 `user_id`**；
- "外部人员"是**派生标签**：读取该用户在目标租户的 `user_tenant_membership` 的 `source / account_kind / msp_role` 计算得出；
- **不在 `users` 主数据上新增全局 flag**：同一人可以在 provider 租户是"内部员工"、在 customer 租户是"外部投影"，"外部性"是租户内关系属性，放 users 会形成第二事实源，与 membership 单源收敛（IP-P1-1/IP-P1-2）冲突。

## 2. 本仓已有的投影与标识机制（复用，零 schema 变更）

`user_tenant_membership`（`itsm-backend/ent/schema/user_tenant_membership.go`）关键字段：

| 字段 | 取值 | 项目管理里的含义 |
|---|---|---|
| `source` | `home \| allocation \| platform \| invite \| migration` | 成员来源：本单位/服务方投影/平台/外部邀请 |
| `account_kind` | `customer \| provider \| platform` | 账号类型快照 |
| `msp_role` | `msp_viewer/tech/specialist/manager/admin` | 服务方角色（可展示） |
| `allocation_id` | `msp_allocations.id` | 投影来源可追溯 |
| `status` / `expires_at` / `deleted_at` | active/suspended 等 | 成员有效性判定依据 |

同步服务（已实现，勿重复造）：

| 场景 | 实现 | 效果 |
|---|---|---|
| provider 分配 → 客户租户投影 | `service/membership_sync.go` `SyncAllocationMembership` | 在客户租户建 membership：`source=allocation`、`account_kind=provider`、`allocation_id` 落库 |
| 解除分配 | `RevokeAllocationMembership` | membership 软删（`deassigned_at` + `deleted_at`），回收即失效 |
| 外部邀请加入 | `service/invitation_service.go` | 接受邀请后建 membership：`source=invite` |
| 本单位成员 | `pkg/bootstrap`、`service/membership_backfill.go` | `source=home`、`is_default=true` |

约束：`uq_membership_live`（user_id + tenant_id where deleted_at is null）部分唯一索引——同一用户同一租户只有一条存活成员行；异源冲突按 `ErrMembershipSourceConflict` 交巡检/人工处置。

## 3. 项目成员读取模型

```
project_members(user_id) ──join──> user_tenant_membership(tenant_id = 项目租户, deleted_at IS NULL)
                                         │
                                         └─派生徽标：memberOrigin
                                              home        → 本单位成员
                                              invite      → 外部邀请人员
                                              allocation  → 服务方投影人员
                                              platform    → 平台人员（默认不加入项目）
```

- 徽标为 API 输出计算字段（后端 join membership），前端仅展示，**不落库**；
- 若要按来源筛选（Q-03），按 `source` 过滤即可，仍无需新字段。

### 3.1 成员候选与指派候选（关键实现点）

候选查询必须是"**目标租户的活跃 membership**"：

```
membership.tenant_id = 项目租户
AND membership.deleted_at IS NULL
AND membership.status = 'active'
AND (membership.expires_at IS NULL OR membership.expires_at > now())
```

**不能用 `users.tenant_id` 判断归属**：它是账号归属租户（provider 用户的 `users.tenant_id` 是 provider 租户），投影关系完全在 membership 里。查错会漏人（投影成员选不到）或误放（已回收成员仍可选）。

### 3.2 成员失效与撤销（不物理删除）

| 事件 | 项目侧处理 |
|---|---|
| 分配被撤销 / 邀请到期 / 成员被移除 | `project_members.status='removed'`（或展示为"已离场"），保留历史与审计；任务指派列表自动过滤 |
| 成员重新加入 | 复用同一行恢复 `active`，不重复插入（唯一约束：project_id + user_id 存活行唯一） |
| 成员离职（账号停用） | 展示"已停用"；任务改派提示 |

## 4. 外部人员加入路径（复用邀请流程）

需要登录的外部协作人（客户侧对接人、外部供应商工程师）：

1. 在项目租户内发起 `invitation`（`ent/schema/invitation.go`）：一次性 token、默认 72h TTL、平台角色黑名单、`target_user_id` 支持绑定已有账号（**`users.username/email` 全局唯一，禁止重复建号**）；
2. 接受后自动建号或绑定，生成 `source=invite` 的 membership；
3. 分配受限角色（建议新增 `external_collaborator` 或复用 end_user 受限面），只授项目相关权限；
4. 到期回收用 `membership.expires_at`（无需额外机制）；需要时人工续期。

无账号的外部联系人（只需要被记录）：不入库为用户，直接在项目备注/风险/任务描述中登记文本；或后续评估"项目级联系人"轻量字段（非 P0）。

## 5. 与 MSP 的关系

- provider 员工被投影到客户租户（`source=allocation`）后，在客户租户内即为**本地成员**，可直接成为客户租户项目的成员——**不是跨租户引用**；
- 写权限按目标租户 RBAC 计算（含 MSP 写门禁口径，见 [05](./05-msp-delivery.md)）；
- 分配撤销 → membership 软删 → 项目成员自动降级显示，任务指派过滤，历史保留。

## 6. 实施注意

1. 不要在 `users` 上加 `is_external`/`user_kind` 全局字段（D-02 理由）；
2. 成员相关 API 输出 `memberOrigin` 计算字段时，批量查询 membership，避免 N+1；
3. 邀请与成员操作的审计（`membership.sync`、`membership.revoke`、邀请审计动作）保持与既有通道一致；
4. 前端成员选择器与后端候选查询共用同一过滤条件（活跃 membership），避免前后端口径漂移。

## 7. 验收要点

- [ ] 合法投影成员（`source=allocation`）能被选入项目并可被指派任务；
- [ ] 分配撤销后：成员显示失效、不可再被指派、历史任务不受影响；
- [ ] 邀请外部人员走完"邀请→接受→成为项目成员"全流程（含重复邮箱绑定已有账号用例）；
- [ ] 候选/指派接口不依赖 `users.tenant_id`（反例用例：provider 员工归属 provider 租户仍能被客户项目选中）。